package cleanup

import (
	"context"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

// LeaseChecker reports whether a dispatch lease is held for a job ID.
type LeaseChecker interface {
	IsDispatchLocked(ctx context.Context, jobID string) (bool, error)
}

// Metrics records job cleanup outcomes.
type Metrics interface {
	RecordJobDeleted(ctx context.Context, reason string)
	RecordJobCleanupSkipped(ctx context.Context, reason string)
	RecordJobCleanupError(ctx context.Context)
}

// JobCleaner deletes scheduler-managed k8s Jobs by GitHub job ID.
type JobCleaner struct {
	namespace string
	k8s       kubernetes.Interface
	leases    LeaseChecker
	metrics   Metrics
	log       *slog.Logger
}

// Config configures JobCleaner.
type Config struct {
	Namespace string
	Metrics   Metrics
}

// NewJobCleaner creates a JobCleaner.
func NewJobCleaner(cfg Config, k8s kubernetes.Interface, leases LeaseChecker) *JobCleaner {
	return &JobCleaner{
		namespace: cfg.Namespace,
		k8s:       k8s,
		leases:    leases,
		metrics:   cfg.Metrics,
		log:       slog.Default(),
	}
}

// CleanupByJobID deletes all k8s Jobs labeled with the given GitHub workflow job ID.
func (c *JobCleaner) CleanupByJobID(ctx context.Context, jobID, reason string) (int, error) {
	if jobID == "" {
		return 0, nil
	}
	if c.leases != nil {
		locked, err := c.leases.IsDispatchLocked(ctx, jobID)
		if err != nil {
			return 0, fmt.Errorf("cleanup: dispatch lock check: %w", err)
		}
		if locked {
			if c.metrics != nil {
				c.metrics.RecordJobCleanupSkipped(ctx, "locked")
			}
			return 0, nil
		}
	}

	jobs, err := c.k8s.BatchV1().Jobs(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", k8sjob.LabelGHJob, jobID),
	})
	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordJobCleanupError(ctx)
		}
		return 0, fmt.Errorf("cleanup: list jobs: %w", err)
	}
	if len(jobs.Items) == 0 {
		return 0, nil
	}

	propagation := metav1.DeletePropagationBackground
	deleted := 0
	for _, job := range jobs.Items {
		err := c.k8s.BatchV1().Jobs(c.namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		})
		if err != nil {
			if c.metrics != nil {
				c.metrics.RecordJobCleanupError(ctx)
			}
			return deleted, fmt.Errorf("cleanup: delete job %q: %w", job.Name, err)
		}
		deleted++
		if c.metrics != nil {
			c.metrics.RecordJobDeleted(ctx, reason)
		}
		c.log.Info("deleted k8s job", "job_id", jobID, "k8s_job", job.Name, "reason", reason)
	}
	return deleted, nil
}
