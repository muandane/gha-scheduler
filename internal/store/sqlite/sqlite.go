package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/muandane/gha-scheduler/internal/store"
)

const timeLayout = time.RFC3339Nano

// Store implements store.JobStore backed by SQLite.
type Store struct {
	db *sql.DB
}

// Open opens path and runs pending migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// OpenDB opens an existing *sql.DB (tests).
func OpenDB(db *sql.DB) (*Store, error) {
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) UpsertQueued(ctx context.Context, owner, repo, runID, jobID string, at time.Time) error {
	fullRepo := owner + "/" + repo
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (job_id, run_id, owner, repo, status, webhook_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET
			status = excluded.status,
			webhook_at = COALESCE(jobs.webhook_at, excluded.webhook_at),
			updated_at = excluded.updated_at
	`, jobID, runID, owner, fullRepo, store.StatusQueued, formatTime(at), formatTime(at), formatTime(at))
	return err
}

func (s *Store) MarkDispatching(ctx context.Context, jobID string, spec store.RunnerSpecSnapshot, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET
			status = ?,
			dispatch_at = ?,
			cpu = ?,
			arch = ?,
			pool = ?,
			cache_enabled = ?,
			labels_json = ?,
			updated_at = ?
		WHERE job_id = ?
	`, store.StatusDispatching, formatTime(at), spec.CPU, spec.Arch, spec.Pool, boolInt(spec.CacheEnabled), spec.LabelsJSON, formatTime(at), jobID)
	return err
}

func (s *Store) MarkJobCreated(ctx context.Context, jobID string, at time.Time) error {
	var webhookAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT webhook_at FROM jobs WHERE job_id = ?`, jobID).Scan(&webhookAt)
	if err != nil {
		return err
	}
	var dispatchLatency sql.NullFloat64
	if webhookAt.Valid {
		wt, _ := parseTime(webhookAt.String)
		dispatchLatency = sql.NullFloat64{Float64: at.Sub(wt).Seconds(), Valid: true}
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE jobs SET
			status = ?,
			job_created_at = ?,
			dispatch_latency_sec = COALESCE(?, dispatch_latency_sec),
			updated_at = ?
		WHERE job_id = ?
	`, store.StatusDispatched, formatTime(at), dispatchLatency, formatTime(at), jobID)
	return err
}

func (s *Store) MarkScheduled(ctx context.Context, jobID, podName string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET status = ?, scheduled_at = ?, pod_name = ?, updated_at = ?
		WHERE job_id = ?
	`, store.StatusScheduled, formatTime(at), podName, formatTime(at), jobID)
	return err
}

func (s *Store) MarkRunning(ctx context.Context, jobID string, at time.Time) error {
	var jobCreatedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT job_created_at FROM jobs WHERE job_id = ?`, jobID).Scan(&jobCreatedAt)
	if err != nil {
		return err
	}
	var scheduleLatency sql.NullFloat64
	if jobCreatedAt.Valid {
		jc, _ := parseTime(jobCreatedAt.String)
		scheduleLatency = sql.NullFloat64{Float64: at.Sub(jc).Seconds(), Valid: true}
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE jobs SET
			status = ?,
			running_at = ?,
			schedule_latency_sec = COALESCE(?, schedule_latency_sec),
			updated_at = ?
		WHERE job_id = ?
	`, store.StatusRunning, formatTime(at), scheduleLatency, formatTime(at), jobID)
	return err
}

func (s *Store) MarkCompleted(ctx context.Context, jobID string, exitCode int, at time.Time) error {
	var runningAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT running_at FROM jobs WHERE job_id = ?`, jobID).Scan(&runningAt)
	if err != nil {
		return err
	}
	status := store.StatusSucceeded
	if exitCode != 0 {
		status = store.StatusFailed
	}
	var jobDuration sql.NullFloat64
	if runningAt.Valid {
		rt, _ := parseTime(runningAt.String)
		jobDuration = sql.NullFloat64{Float64: at.Sub(rt).Seconds(), Valid: true}
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE jobs SET
			status = ?,
			completed_at = ?,
			exit_code = ?,
			job_duration_sec = COALESCE(?, job_duration_sec),
			updated_at = ?
		WHERE job_id = ?
	`, status, formatTime(at), exitCode, jobDuration, formatTime(at), jobID)
	return err
}

func (s *Store) MarkDispatchError(ctx context.Context, jobID, reason string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET status = ?, dispatch_error = ?, updated_at = ?
		WHERE job_id = ?
	`, store.StatusDispatchError, reason, formatTime(at), jobID)
	return err
}

func (s *Store) GetJob(ctx context.Context, jobID string) (*store.Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT job_id, run_id, owner, repo, status,
			webhook_at, dispatch_at, job_created_at, scheduled_at, running_at, completed_at,
			dispatch_latency_sec, schedule_latency_sec, job_duration_sec,
			cpu, arch, pool, cache_enabled, labels_json,
			exit_code, pod_name, dispatch_error, created_at, updated_at
		FROM jobs WHERE job_id = ?
	`, jobID)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return job, err
}

func (s *Store) ListJobs(ctx context.Context, q store.ListQuery) (store.ListResult, error) {
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var args []any
	where := []string{"1=1"}
	if q.Repo != "" {
		where = append(where, "repo = ?")
		args = append(args, q.Repo)
	}
	if q.Status != "" {
		where = append(where, "status = ?")
		args = append(args, q.Status)
	}
	if q.Cursor != "" {
		where = append(where, "updated_at < ?")
		args = append(args, q.Cursor)
	}
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		SELECT job_id, run_id, owner, repo, status,
			webhook_at, dispatch_at, job_created_at, scheduled_at, running_at, completed_at,
			dispatch_latency_sec, schedule_latency_sec, job_duration_sec,
			cpu, arch, pool, cache_enabled, labels_json,
			exit_code, pod_name, dispatch_error, created_at, updated_at
		FROM jobs WHERE %s
		ORDER BY updated_at DESC
		LIMIT ?
	`, strings.Join(where, " AND "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return store.ListResult{}, err
	}
	defer rows.Close()

	var jobs []store.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return store.ListResult{}, err
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return store.ListResult{}, err
	}

	var next string
	if len(jobs) > limit {
		next = formatTime(jobs[limit-1].UpdatedAt)
		jobs = jobs[:limit]
	}
	return store.ListResult{Jobs: jobs, NextCursor: next}, nil
}

func (s *Store) Stats(ctx context.Context, since time.Time) (store.Stats, error) {
	var out store.Stats

	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs
		WHERE status IN (?, ?, ?, ?, ?)
	`, store.StatusQueued, store.StatusDispatching, store.StatusDispatched, store.StatusScheduled, store.StatusRunning).Scan(&out.ActiveJobs)
	if err != nil {
		return out, err
	}

	sinceStr := formatTime(since)
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs
		WHERE status = ? AND updated_at >= ?
	`, store.StatusDispatchError, sinceStr).Scan(&out.DispatchErrors24h)
	if err != nil {
		return out, err
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs
		WHERE completed_at IS NOT NULL AND completed_at >= ?
	`, sinceStr).Scan(&out.CompletedJobs)
	if err != nil {
		return out, err
	}

	dispatch, err := s.latencyColumn(ctx, "dispatch_latency_sec", sinceStr)
	if err != nil {
		return out, err
	}
	schedule, err := s.latencyColumn(ctx, "schedule_latency_sec", sinceStr)
	if err != nil {
		return out, err
	}

	out.DispatchP50 = store.Percentile(dispatch, 0.5)
	out.DispatchP95 = store.Percentile(dispatch, 0.95)
	out.ScheduleP50 = store.Percentile(schedule, 0.5)
	out.ScheduleP95 = store.Percentile(schedule, 0.95)
	return out, nil
}

func (s *Store) latencyColumn(ctx context.Context, col, since string) ([]float64, error) {
	query := fmt.Sprintf(`
		SELECT %s FROM jobs
		WHERE %s IS NOT NULL AND updated_at >= ?
		ORDER BY updated_at DESC
		LIMIT 10000
	`, col, col)
	rows, err := s.db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vals []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return vals, rows.Err()
}

func (s *Store) Prune(ctx context.Context, before time.Time) (int64, error) {
	beforeStr := formatTime(before)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM jobs WHERE
			(completed_at IS NOT NULL AND completed_at < ?)
			OR (status IN (?, ?) AND updated_at < ?)
			OR (status = ? AND updated_at < ?)
	`, beforeStr, store.StatusSucceeded, store.StatusFailed, beforeStr, store.StatusDispatchError, beforeStr)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (*store.Job, error) {
	var j store.Job
	var status string
	var webhookAt, dispatchAt, jobCreatedAt, scheduledAt, runningAt, completedAt sql.NullString
	var dispatchLat, scheduleLat, jobDur sql.NullFloat64
	var cpu sql.NullInt64
	var arch, pool, labelsJSON, podName, dispatchError sql.NullString
	var cacheEnabled int
	var exitCode sql.NullInt64
	var createdAt, updatedAt string

	err := row.Scan(
		&j.JobID, &j.RunID, &j.Owner, &j.Repo, &status,
		&webhookAt, &dispatchAt, &jobCreatedAt, &scheduledAt, &runningAt, &completedAt,
		&dispatchLat, &scheduleLat, &jobDur,
		&cpu, &arch, &pool, &cacheEnabled, &labelsJSON,
		&exitCode, &podName, &dispatchError, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	j.Status = store.Status(status)
	j.WebhookAt, _ = parseNullTime(webhookAt)
	j.DispatchAt, _ = parseNullTime(dispatchAt)
	j.JobCreatedAt, _ = parseNullTime(jobCreatedAt)
	j.ScheduledAt, _ = parseNullTime(scheduledAt)
	j.RunningAt, _ = parseNullTime(runningAt)
	j.CompletedAt, _ = parseNullTime(completedAt)
	if dispatchLat.Valid {
		j.DispatchLatencySec = dispatchLat.Float64
	}
	if scheduleLat.Valid {
		j.ScheduleLatencySec = scheduleLat.Float64
	}
	if jobDur.Valid {
		j.JobDurationSec = jobDur.Float64
	}
	if cpu.Valid {
		j.CPU = int(cpu.Int64)
	}
	if arch.Valid {
		j.Arch = arch.String
	}
	if pool.Valid {
		j.Pool = pool.String
	}
	j.CacheEnabled = cacheEnabled != 0
	if labelsJSON.Valid {
		j.LabelsJSON = labelsJSON.String
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		j.ExitCode = &v
	}
	if podName.Valid {
		j.PodName = podName.String
	}
	if dispatchError.Valid {
		j.DispatchError = dispatchError.String
	}
	j.CreatedAt, _ = parseTime(createdAt)
	j.UpdatedAt, _ = parseTime(updatedAt)
	return &j, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(timeLayout, s)
}

func parseNullTime(ns sql.NullString) (time.Time, error) {
	if !ns.Valid {
		return time.Time{}, nil
	}
	return parseTime(ns.String)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// RawDB exposes the underlying database for tests.
func (s *Store) RawDB() *sql.DB {
	return s.db
}
