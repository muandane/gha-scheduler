package tracing

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/labelquery"
	"github.com/muandane/gha-scheduler/internal/store"
)

// StoreEmitter persists job lifecycle events and delegates to SpanEmitter.
type StoreEmitter struct {
	span  *SpanEmitter
	store store.JobStore
}

// NewStoreEmitter wraps span with persistent store writes.
func NewStoreEmitter(span *SpanEmitter, st store.JobStore) *StoreEmitter {
	return &StoreEmitter{span: span, store: st}
}

// Registry returns the underlying job trace registry.
func (s *StoreEmitter) Registry() *JobTraceRegistry {
	return s.span.Registry()
}

// WebhookReceived records webhook acceptance.
func (s *StoreEmitter) WebhookReceived(ctx context.Context, attrs map[string]string) {
	s.span.WebhookReceived(ctx, attrs)
	s.persist(ctx, "upsert_queued", attrs["job_id"], func() error {
		owner, repo := splitRepo(attrs["repo"])
		return s.store.UpsertQueued(ctx, owner, repo, attrs["run_id"], attrs["job_id"], time.Now())
	})
}

// ReconcileDispatch starts a reconciler-driven job trace and record.
func (s *StoreEmitter) ReconcileDispatch(ctx context.Context, req dispatch.Request) {
	s.span.ReconcileDispatch(ctx, req)
	s.persist(ctx, "reconcile_queued", req.JobID, func() error {
		return s.store.UpsertQueued(ctx, req.Owner, req.Repo, req.RunID, req.JobID, time.Now())
	})
}

// DispatchStarted records dispatch start.
func (s *StoreEmitter) DispatchStarted(ctx context.Context, jobID string, spec labelquery.RunnerSpec, repo string) {
	s.span.DispatchStarted(ctx, jobID, spec, repo)
	rawJSON, _ := json.Marshal(spec.Raw)
	s.persist(ctx, "dispatching", jobID, func() error {
		return s.store.MarkDispatching(ctx, jobID, store.RunnerSpecSnapshot{
			CPU:          spec.CPU,
			Arch:         spec.Arch,
			Pool:         spec.Pool,
			CacheEnabled: spec.Cache,
			LabelsJSON:   string(rawJSON),
		}, time.Now())
	})
}

// JobCreated records k8s Job creation.
func (s *StoreEmitter) JobCreated(jobID string) {
	s.span.JobCreated(jobID)
	s.persist(context.Background(), "job_created", jobID, func() error {
		return s.store.MarkJobCreated(context.Background(), jobID, time.Now())
	})
}

// PodScheduled records pod scheduled.
func (s *StoreEmitter) PodScheduled(ctx context.Context, attrs map[string]string) {
	s.span.PodScheduled(ctx, attrs)
	s.persist(ctx, "scheduled", attrs["job_id"], func() error {
		return s.store.MarkScheduled(ctx, attrs["job_id"], attrs["pod"], time.Now())
	})
}

// PodRunning records pod running.
func (s *StoreEmitter) PodRunning(ctx context.Context, attrs map[string]string) {
	s.span.PodRunning(ctx, attrs)
	s.persist(ctx, "running", attrs["job_id"], func() error {
		return s.store.MarkRunning(ctx, attrs["job_id"], time.Now())
	})
}

// PodCompleted records pod completion.
func (s *StoreEmitter) PodCompleted(ctx context.Context, attrs map[string]string) {
	s.span.PodCompleted(ctx, attrs)
	exitCode := 0
	if v := attrs["exit_code"]; v != "" {
		exitCode, _ = strconv.Atoi(v)
	}
	s.persist(ctx, "completed", attrs["job_id"], func() error {
		return s.store.MarkCompleted(ctx, attrs["job_id"], exitCode, time.Now())
	})
}

// RecordCacheSidecarFailure increments cache sidecar failure metric.
func (s *StoreEmitter) RecordCacheSidecarFailure(ctx context.Context) {
	s.span.RecordCacheSidecarFailure(ctx)
}

// CacheSidecarFailed implements informer.LifecycleEmitter.
func (s *StoreEmitter) CacheSidecarFailed(ctx context.Context) {
	s.span.CacheSidecarFailed(ctx)
}

// RecordDispatchError persists a dispatch failure.
func (s *StoreEmitter) RecordDispatchError(ctx context.Context, req dispatch.Request, reason string) {
	s.persist(ctx, "dispatch_error", req.JobID, func() error {
		return s.store.MarkDispatchError(ctx, req.JobID, reason, time.Now())
	})
}

func (s *StoreEmitter) persist(ctx context.Context, op, jobID string, fn func() error) {
	if s.store == nil || jobID == "" {
		return
	}
	if err := fn(); err != nil {
		slog.Warn("job store write failed", "op", op, "job_id", jobID, "err", err)
	}
}

func splitRepo(full string) (owner, repo string) {
	for i := 0; i < len(full); i++ {
		if full[i] == '/' {
			return full[:i], full[i+1:]
		}
	}
	return full, ""
}
