package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

const runnerNamePrefix = "ghs-"

// RunnerLister lists GitHub self-hosted runners.
type RunnerLister interface {
	ListRunners(ctx context.Context, owner, repo string) ([]ghclient.Runner, error)
	DeleteRunner(ctx context.Context, owner, repo string, runnerID int64) error
}

// LeaseChecker reports whether a dispatch lease is held for a job ID.
type LeaseChecker interface {
	IsDispatchLocked(ctx context.Context, jobID string) (bool, error)
}

// OrphanMetrics records orphan sweep outcomes.
type OrphanMetrics interface {
	RecordOrphanRunnerDeleted(ctx context.Context)
	RecordOrphanRunnerSkipped(ctx context.Context, reason string)
}

// OrphanRunnerSweep removes stale GitHub runner registrations with no k8s Job.
type OrphanRunnerSweep struct {
	namespace string
	repos     []string
	grace     time.Duration
	gh        RunnerLister
	k8s       kubernetes.Interface
	leases    LeaseChecker
	metrics   OrphanMetrics
	log       func(msg string, args ...any)
}

// OrphanSweepConfig configures the orphan runner sweep.
type OrphanSweepConfig struct {
	Namespace string
	Repos     []string
	Grace     time.Duration
	Metrics   OrphanMetrics
}

// NewOrphanRunnerSweep creates an OrphanRunnerSweep.
func NewOrphanRunnerSweep(cfg OrphanSweepConfig, gh RunnerLister, k8s kubernetes.Interface, leases LeaseChecker) *OrphanRunnerSweep {
	grace := cfg.Grace
	if grace == 0 {
		grace = 2 * time.Minute
	}
	return &OrphanRunnerSweep{
		namespace: cfg.Namespace,
		repos:     cfg.Repos,
		grace:     grace,
		gh:        gh,
		k8s:       k8s,
		leases:    leases,
		metrics:   cfg.Metrics,
		log:       func(string, ...any) {},
	}
}

// SweepOnce runs one orphan cleanup pass across configured repos.
func (s *OrphanRunnerSweep) SweepOnce(ctx context.Context) error {
	now := time.Now()
	for _, full := range s.repos {
		owner, repo, err := splitRepo(full)
		if err != nil {
			continue
		}
		runners, err := s.gh.ListRunners(ctx, owner, repo)
		if err != nil {
			s.log("orphan sweep list runners failed", "owner", owner, "repo", repo, "err", err)
			continue
		}
		for _, runner := range runners {
			s.evaluateRunner(ctx, owner, repo, runner, now)
		}
	}
	return nil
}

func (s *OrphanRunnerSweep) evaluateRunner(ctx context.Context, owner, repo string, runner ghclient.Runner, now time.Time) {
	if !strings.HasPrefix(runner.Name, runnerNamePrefix) {
		s.skip(ctx, "wrong_prefix")
		return
	}
	jobID, ok := parseJobIDFromRunnerName(runner.Name)
	if !ok {
		s.skip(ctx, "bad_name")
		return
	}
	if runner.CreatedAt.IsZero() || now.Sub(runner.CreatedAt) < s.grace {
		s.skip(ctx, "young")
		return
	}
	if s.leases != nil {
		locked, err := s.leases.IsDispatchLocked(ctx, jobID)
		if err == nil && locked {
			s.skip(ctx, "locked")
			return
		}
	}
	hasJob, err := s.hasJob(ctx, jobID)
	if err != nil || hasJob {
		if hasJob {
			s.skip(ctx, "has_job")
		}
		return
	}
	if err := s.gh.DeleteRunner(ctx, owner, repo, runner.ID); err != nil {
		return
	}
	if s.metrics != nil {
		s.metrics.RecordOrphanRunnerDeleted(ctx)
	}
}

func (s *OrphanRunnerSweep) skip(ctx context.Context, reason string) {
	if s.metrics != nil {
		s.metrics.RecordOrphanRunnerSkipped(ctx, reason)
	}
}

func (s *OrphanRunnerSweep) hasJob(ctx context.Context, jobID string) (bool, error) {
	jobs, err := s.k8s.BatchV1().Jobs(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", k8sjob.LabelGHJob, jobID),
	})
	if err != nil {
		return false, err
	}
	return len(jobs.Items) > 0, nil
}

// parseJobIDFromRunnerName extracts job_id from ghs-<run_id>-<job_id>.
func parseJobIDFromRunnerName(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, runnerNamePrefix)
	if !ok || rest == "" {
		return "", false
	}
	idx := strings.LastIndex(rest, "-")
	if idx <= 0 || idx >= len(rest)-1 {
		return "", false
	}
	jobID := rest[idx+1:]
	if jobID == "" {
		return "", false
	}
	return jobID, true
}
