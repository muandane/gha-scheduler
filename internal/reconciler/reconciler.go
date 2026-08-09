package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

// GHClient lists queued runs and jobs from GitHub.
type GHClient interface {
	ListRuns(ctx context.Context, owner, repo string, statuses []string) ([]ghclient.WorkflowRun, error)
	ListRunJobs(ctx context.Context, owner, repo string, runID int64) ([]ghclient.WorkflowJob, error)
}

// Dispatcher dispatches missed queued jobs.
type Dispatcher interface {
	Dispatch(ctx context.Context, req dispatch.Request) error
}

// Config configures the reconciler.
type Config struct {
	Namespace      string
	Repos          []string
	Interval       time.Duration
	StaleThreshold time.Duration
	LabelDefaults  dispatch.LabelDefaults
}

// Reconciler polls GitHub for stale queued jobs without matching k8s Jobs.
type Reconciler struct {
	cfg Config
	gh  GHClient
	d   Dispatcher
	k8s kubernetes.Interface
	log *slog.Logger
}

// New creates a Reconciler.
func New(cfg Config, gh GHClient, d Dispatcher, k8s kubernetes.Interface) *Reconciler {
	if cfg.Interval == 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.StaleThreshold == 0 {
		cfg.StaleThreshold = 30 * time.Second
	}
	return &Reconciler{
		cfg: cfg,
		gh:  gh,
		d:   d,
		k8s: k8s,
		log: slog.Default(),
	}
}

// Run executes reconciler cycles until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	for {
		if err := r.ReconcileOnce(ctx); err != nil {
			r.log.Error("reconcile failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ReconcileOnce runs a single reconciliation cycle.
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
	return r.reconcile(ctx)
}

func (r *Reconciler) reconcile(ctx context.Context) error {
	for _, full := range r.cfg.Repos {
		owner, repo, err := splitRepo(full)
		if err != nil {
			r.log.Error("invalid repo", "repo", full, "err", err)
			continue
		}
		if err := r.reconcileRepo(ctx, owner, repo); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileRepo(ctx context.Context, owner, repo string) error {
	runs, err := r.gh.ListRuns(ctx, owner, repo, []string{"queued", "in_progress"})
	if err != nil {
		return fmt.Errorf("reconciler: list runs %s/%s: %w", owner, repo, err)
	}

	now := time.Now()
	for _, run := range runs {
		jobs, err := r.gh.ListRunJobs(ctx, owner, repo, run.ID)
		if err != nil {
			return fmt.Errorf("reconciler: list jobs run %d: %w", run.ID, err)
		}
		for _, job := range jobs {
			if job.Status != "queued" {
				continue
			}
			if job.CreatedAt.IsZero() || now.Sub(job.CreatedAt) < r.cfg.StaleThreshold {
				continue
			}
			jobID := formatID(job.ID)
			dup, err := r.hasJob(ctx, jobID)
			if err != nil {
				return err
			}
			if dup {
				continue
			}
			req := dispatch.Request{
				Owner:         owner,
				Repo:          repo,
				RunID:         formatID(job.RunID),
				JobID:         jobID,
				Labels:        job.Labels,
				LabelDefaults: r.cfg.LabelDefaults,
			}
			if err := r.d.Dispatch(ctx, req); err != nil {
				r.log.Error("reconcile dispatch failed", "owner", owner, "repo", repo, "job_id", jobID, "err", err)
			}
		}
	}
	return nil
}

func (r *Reconciler) hasJob(ctx context.Context, jobID string) (bool, error) {
	jobs, err := r.k8s.BatchV1().Jobs(r.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", k8sjob.LabelGHJob, jobID),
	})
	if err != nil {
		return false, err
	}
	return len(jobs.Items) > 0, nil
}

func splitRepo(full string) (owner, repo string, err error) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected owner/repo, got %q", full)
	}
	return parts[0], parts[1], nil
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}
