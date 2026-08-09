package tracing

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type jobTrace struct {
	ctx        context.Context
	root       trace.Span
	webhookAt  time.Time
	dispatchAt time.Time
	jobCreated time.Time
	runningAt  time.Time
}

// JobTraceRegistry links all spans for one GH job under a single trace.
type JobTraceRegistry struct {
	tracer trace.Tracer
	mu     sync.Mutex
	jobs   map[string]*jobTrace
}

// NewJobTraceRegistry creates a registry backed by tracer.
func NewJobTraceRegistry(tracer trace.Tracer) *JobTraceRegistry {
	return &JobTraceRegistry{
		tracer: tracer,
		jobs:   make(map[string]*jobTrace),
	}
}

// StartJob opens the root span job.webhook_received for jobID.
func (r *JobTraceRegistry) StartJob(ctx context.Context, jobID string, attrs map[string]string) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.jobs[jobID]; ok {
		return existing.ctx
	}

	childCtx, root := r.tracer.Start(ctx, "job.webhook_received", withStringAttrs(attrs))
	r.jobs[jobID] = &jobTrace{
		ctx:       childCtx,
		root:      root,
		webhookAt: time.Now(),
	}
	return childCtx
}

// JobCtx returns the active trace context for jobID.
func (r *JobTraceRegistry) JobCtx(jobID string) (context.Context, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jt, ok := r.jobs[jobID]
	if !ok {
		return nil, false
	}
	return jt.ctx, true
}

// MarkDispatch records dispatch start time for latency metrics.
func (r *JobTraceRegistry) MarkDispatch(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if jt, ok := r.jobs[jobID]; ok {
		jt.dispatchAt = time.Now()
	}
}

// MarkJobCreated records k8s Job creation time.
func (r *JobTraceRegistry) MarkJobCreated(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if jt, ok := r.jobs[jobID]; ok {
		jt.jobCreated = time.Now()
	}
}

// MarkRunning records pod running time.
func (r *JobTraceRegistry) MarkRunning(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if jt, ok := r.jobs[jobID]; ok {
		jt.runningAt = time.Now()
	}
}

// Timestamps returns timing markers for metrics.
func (r *JobTraceRegistry) Timestamps(jobID string) (webhook, dispatch, jobCreated, running time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jt, ok := r.jobs[jobID]
	if !ok {
		return time.Time{}, time.Time{}, time.Time{}, time.Time{}
	}
	return jt.webhookAt, jt.dispatchAt, jt.jobCreated, jt.runningAt
}

// EndJob ends the root span and removes jobID.
func (r *JobTraceRegistry) EndJob(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jt, ok := r.jobs[jobID]
	if !ok {
		return
	}
	jt.root.End()
	delete(r.jobs, jobID)
}

func withStringAttrs(attrs map[string]string) trace.SpanStartOption {
	if len(attrs) == 0 {
		return trace.WithAttributes()
	}
	kv := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		kv = append(kv, attribute.String(k, v))
	}
	return trace.WithAttributes(kv...)
}
