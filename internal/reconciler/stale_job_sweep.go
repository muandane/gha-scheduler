package reconciler

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"

	"github.com/muandane/gha-scheduler/internal/cleanup"
	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

// WorkflowJobGetter fetches a GitHub workflow job by ID.
type WorkflowJobGetter interface {
	GetWorkflowJob(ctx context.Context, owner, repo string, jobID int64) (ghclient.WorkflowJob, error)
}

// StaleJobMetrics records stale job sweep outcomes.
type StaleJobMetrics interface {
	RecordStaleJobFound(ctx context.Context)
	RecordJobDeleted(ctx context.Context, reason string)
	RecordJobCleanupSkipped(ctx context.Context, reason string)
	RecordJobCleanupError(ctx context.Context)
}

// StaleJobSweepConfig configures the stale k8s Job sweep.
type StaleJobSweepConfig struct {
	Namespace         string
	CleanupGrace      time.Duration
	StuckThreshold    time.Duration
	MaxRuntime        time.Duration
	GHStatusCacheTTL  time.Duration
	Metrics           StaleJobMetrics
}

// StaleJobSweep removes scheduler k8s Jobs that are done on GitHub or stuck locally.
type StaleJobSweep struct {
	namespace    string
	cleanupGrace time.Duration
	stuck        time.Duration
	maxRuntime   time.Duration
	cacheTTL     time.Duration
	gh           WorkflowJobGetter
	k8s          kubernetes.Interface
	cleaner      *cleanup.JobCleaner
	leases       cleanup.LeaseChecker
	metrics      StaleJobMetrics
	log          func(msg string, args ...any)

	mu         sync.Mutex
	ghCache    map[string]ghJobCacheEntry
}

type ghJobCacheEntry struct {
	job       ghclient.WorkflowJob
	fetchedAt time.Time
}

// NewStaleJobSweep creates a StaleJobSweep.
func NewStaleJobSweep(cfg StaleJobSweepConfig, gh WorkflowJobGetter, k8s kubernetes.Interface, cleaner *cleanup.JobCleaner, leases cleanup.LeaseChecker) *StaleJobSweep {
	if cfg.CleanupGrace == 0 {
		cfg.CleanupGrace = 30 * time.Second
	}
	if cfg.StuckThreshold == 0 {
		cfg.StuckThreshold = 15 * time.Minute
	}
	if cfg.MaxRuntime == 0 {
		cfg.MaxRuntime = 6 * time.Hour
	}
	if cfg.GHStatusCacheTTL == 0 {
		cfg.GHStatusCacheTTL = 2 * time.Minute
	}
	return &StaleJobSweep{
		namespace:    cfg.Namespace,
		cleanupGrace: cfg.CleanupGrace,
		stuck:        cfg.StuckThreshold,
		maxRuntime:   cfg.MaxRuntime,
		cacheTTL:     cfg.GHStatusCacheTTL,
		gh:           gh,
		k8s:          k8s,
		cleaner:      cleaner,
		leases:       leases,
		metrics:      cfg.Metrics,
		ghCache:      make(map[string]ghJobCacheEntry),
		log:          func(string, ...any) {},
	}
}

// SweepOnce runs one stale job cleanup pass.
func (s *StaleJobSweep) SweepOnce(ctx context.Context) error {
	req, err := labels.NewRequirement(k8sjob.LabelGHJob, selection.Exists, nil)
	if err != nil {
		return fmt.Errorf("stale job sweep: label selector: %w", err)
	}
	selector := labels.NewSelector().Add(*req).String()

	jobs, err := s.k8s.BatchV1().Jobs(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return fmt.Errorf("stale job sweep: list jobs: %w", err)
	}

	now := time.Now()
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if reason := s.evaluateJob(ctx, job, now); reason != "" {
			if s.metrics != nil {
				s.metrics.RecordStaleJobFound(ctx)
			}
			if _, err := s.cleaner.CleanupByJobID(ctx, job.Labels[k8sjob.LabelGHJob], reason); err != nil {
				s.log("stale job cleanup failed", "job_id", job.Labels[k8sjob.LabelGHJob], "reason", reason, "err", err)
			}
		}
	}
	return nil
}

func (s *StaleJobSweep) evaluateJob(ctx context.Context, job *batchv1.Job, now time.Time) string {
	jobID := job.Labels[k8sjob.LabelGHJob]
	if jobID == "" {
		return ""
	}
	if s.leases != nil {
		locked, err := s.leases.IsDispatchLocked(ctx, jobID)
		if err == nil && locked {
			if s.metrics != nil {
				s.metrics.RecordJobCleanupSkipped(ctx, "locked")
			}
			return ""
		}
	}

	age := now.Sub(job.CreationTimestamp.Time)
	if age >= s.maxRuntime {
		return "max_runtime"
	}

	pods, err := s.listPodsForJob(ctx, jobID)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordJobCleanupError(ctx)
		}
		s.log("stale job sweep list pods failed", "job_id", jobID, "err", err)
		return ""
	}

	if reason := s.podReason(pods, age); reason != "" {
		return reason
	}

	if ghReason := s.githubReason(ctx, job, jobID, now); ghReason != "" {
		return ghReason
	}
	return ""
}

func (s *StaleJobSweep) listPodsForJob(ctx context.Context, jobID string) ([]corev1.Pod, error) {
	pods, err := s.k8s.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", k8sjob.LabelGHJob, jobID),
	})
	if err != nil {
		return nil, err
	}
	return pods.Items, nil
}

func (s *StaleJobSweep) podReason(pods []corev1.Pod, jobAge time.Duration) string {
	if len(pods) == 0 {
		if jobAge >= s.stuck {
			return "no_pod"
		}
		return ""
	}
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			return "pod_terminal"
		}
		if jobAge < s.stuck {
			continue
		}
		if pod.Status.Phase == corev1.PodPending {
			return "stuck_pending"
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if waiting := cs.State.Waiting; waiting != nil {
				switch waiting.Reason {
				case "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "InvalidImageName":
					return "stuck_container"
				}
			}
		}
	}
	return ""
}

func (s *StaleJobSweep) githubReason(ctx context.Context, job *batchv1.Job, jobID string, now time.Time) string {
	ownerRepo := job.Labels[k8sjob.LabelOwnerRepo]
	if ownerRepo == "" || s.gh == nil {
		return ""
	}
	owner, repo, err := splitRepo(ownerRepo)
	if err != nil {
		return ""
	}
	id, err := strconv.ParseInt(jobID, 10, 64)
	if err != nil {
		return ""
	}

	ghJob, ok := s.cachedGHJob(jobID)
	if !ok {
		ghJob, err = s.gh.GetWorkflowJob(ctx, owner, repo, id)
		if err != nil {
			s.log("stale job sweep get workflow job failed", "job_id", jobID, "err", err)
			return ""
		}
		s.setGHCache(jobID, ghJob)
	}

	if ghJob.Status != "completed" {
		return ""
	}
	completedAt := ghJob.CompletedAt
	if completedAt.IsZero() {
		completedAt = job.CreationTimestamp.Time
	}
	if now.Sub(completedAt) < s.cleanupGrace {
		return ""
	}
	return "gh_completed"
}

func (s *StaleJobSweep) cachedGHJob(jobID string) (ghclient.WorkflowJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.ghCache[jobID]
	if !ok || time.Since(entry.fetchedAt) > s.cacheTTL {
		return ghclient.WorkflowJob{}, false
	}
	return entry.job, true
}

func (s *StaleJobSweep) setGHCache(jobID string, job ghclient.WorkflowJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ghCache[jobID] = ghJobCacheEntry{job: job, fetchedAt: time.Now()}
}
