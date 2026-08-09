package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/muandane/gha-scheduler/internal/store"
	sqlitestore "github.com/muandane/gha-scheduler/internal/store/sqlite"
)

func openTestDB(t *testing.T) *sqlitestore.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.OpenDB(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestMigrateIdempotent(t *testing.T) {
	st := openTestDB(t)
	if _, err := sqlitestore.OpenDB(st.RawDB()); err != nil {
		t.Fatal(err)
	}
}

func TestJobLifecycle(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	if err := st.UpsertQueued(ctx, "org", "repo", "100", "200", now); err != nil {
		t.Fatal(err)
	}
	spec := store.RunnerSpecSnapshot{CPU: 2, Arch: "x64", Pool: "spot", CacheEnabled: true, LabelsJSON: `{}`}
	if err := st.MarkDispatching(ctx, "200", spec, now.Add(1*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkJobCreated(ctx, "200", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkScheduled(ctx, "200", "pod-1", now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkRunning(ctx, "200", now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCompleted(ctx, "200", 0, now.Add(100*time.Second)); err != nil {
		t.Fatal(err)
	}

	job, err := st.GetJob(ctx, "200")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != store.StatusSucceeded {
		t.Fatalf("status %q", job.Status)
	}
	if job.DispatchLatencySec < 1.9 || job.DispatchLatencySec > 2.1 {
		t.Fatalf("dispatch latency %v", job.DispatchLatencySec)
	}
	if job.ScheduleLatencySec < 7.9 || job.ScheduleLatencySec > 8.1 {
		t.Fatalf("schedule latency %v", job.ScheduleLatencySec)
	}
	if job.JobDurationSec < 89.9 || job.JobDurationSec > 90.1 {
		t.Fatalf("job duration %v", job.JobDurationSec)
	}
}

func TestListAndPrune(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_ = st.UpsertQueued(ctx, "org", "repo", "1", "j1", now.Add(-48*time.Hour))
	_ = st.MarkDispatching(ctx, "j1", store.RunnerSpecSnapshot{}, now.Add(-48*time.Hour))
	_ = st.MarkJobCreated(ctx, "j1", now.Add(-48*time.Hour))
	_ = st.MarkRunning(ctx, "j1", now.Add(-48*time.Hour))
	_ = st.MarkCompleted(ctx, "j1", 0, now.Add(-48*time.Hour))

	_ = st.UpsertQueued(ctx, "org", "repo", "2", "j2", now)

	res, err := st.ListJobs(ctx, store.ListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Jobs) != 2 {
		t.Fatalf("got %d jobs", len(res.Jobs))
	}

	n, err := st.Prune(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d", n)
	}
}

func TestStats(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i, lat := range []float64{1, 2, 3, 4, 100} {
		id := fmt.Sprintf("s%d", i)
		_ = st.UpsertQueued(ctx, "org", "repo", "1", id, now)
		_ = st.MarkDispatching(ctx, id, store.RunnerSpecSnapshot{}, now)
		_ = st.MarkJobCreated(ctx, id, now.Add(time.Duration(lat)*time.Second))
	}

	stats, err := st.Stats(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stats.DispatchP50 < 2 || stats.DispatchP50 > 4 {
		t.Fatalf("p50 %v", stats.DispatchP50)
	}
	if stats.DispatchP95 < 50 {
		t.Fatalf("p95 %v", stats.DispatchP95)
	}
}

func TestDispatchError(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_ = st.UpsertQueued(ctx, "org", "repo", "1", "e1", now)
	if err := st.MarkDispatchError(ctx, "e1", "jit_failed", now); err != nil {
		t.Fatal(err)
	}
	job, err := st.GetJob(ctx, "e1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != store.StatusDispatchError || job.DispatchError != "jit_failed" {
		t.Fatalf("%+v", job)
	}
}
