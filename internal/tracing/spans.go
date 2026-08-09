package tracing

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/labelquery"
)

// Emitter records job lifecycle spans from webhook and pod watch events.
type Emitter interface {
	WebhookReceived(ctx context.Context, attrs map[string]string)
	DispatchStarted(ctx context.Context, jobID string, spec labelquery.RunnerSpec, repo string)
	PodScheduled(ctx context.Context, attrs map[string]string)
	PodRunning(ctx context.Context, attrs map[string]string)
	PodCompleted(ctx context.Context, attrs map[string]string)
	JobCreated(jobID string)
	RecordCacheSidecarFailure(ctx context.Context)
}

// SpanEmitter implements Emitter using OpenTelemetry with per-job trace linking.
type SpanEmitter struct {
	tracer   trace.Tracer
	registry *JobTraceRegistry
	metrics  *Metrics
}

// NewSpanEmitter creates an Emitter backed by tracer and optional registry/metrics.
func NewSpanEmitter(tracer trace.Tracer, registry *JobTraceRegistry, metrics *Metrics) *SpanEmitter {
	if registry == nil {
		registry = NewJobTraceRegistry(tracer)
	}
	return &SpanEmitter{tracer: tracer, registry: registry, metrics: metrics}
}

// Registry returns the job trace registry for external hooks.
func (s *SpanEmitter) Registry() *JobTraceRegistry {
	return s.registry
}

// WebhookReceived starts the root span for a job trace.
func (s *SpanEmitter) WebhookReceived(ctx context.Context, attrs map[string]string) {
	jobID := attrs["job_id"]
	if jobID == "" {
		s.emit(ctx, "job.webhook_received", attrs, false)
		return
	}
	s.registry.StartJob(ctx, jobID, attrs)
}

// DispatchStarted emits job.dispatch as a child of the job trace.
func (s *SpanEmitter) DispatchStarted(ctx context.Context, jobID string, spec labelquery.RunnerSpec, repo string) {
	s.registry.MarkDispatch(jobID)
	attrs := specAttrs(spec, repo)
	if src, ok := s.registry.Source(jobID); ok {
		attrs["dispatch_source"] = src
	}
	jobCtx, ok := s.registry.JobCtx(jobID)
	if !ok {
		s.emit(ctx, "job.dispatch", attrs, false)
		return
	}
	s.emitChild(jobCtx, "job.dispatch", attrs, false)
}

// ReconcileDispatch starts a trace for reconciler-driven dispatch.
func (s *SpanEmitter) ReconcileDispatch(ctx context.Context, req dispatch.Request) {
	if req.JobID == "" {
		return
	}
	s.registry.StartJobFromReconcile(ctx, req.JobID, map[string]string{
		"repo":   req.Owner + "/" + req.Repo,
		"run_id": req.RunID,
		"job_id": req.JobID,
	})
}

// JobCreated marks Job creation for dispatch latency metrics.
func (s *SpanEmitter) JobCreated(jobID string) {
	s.registry.MarkJobCreated(jobID)
	if s.metrics != nil {
		s.metrics.RecordTimingsFromRegistry(context.Background(), s.registry, jobID, lifecycleDispatchComplete)
	}
}

// PodScheduled emits job.pod_scheduled linked to job trace.
func (s *SpanEmitter) PodScheduled(ctx context.Context, attrs map[string]string) {
	s.emitLifecycle(ctx, "job.pod_scheduled", attrs, false)
}

// PodRunning emits job.pod_running and records schedule latency.
func (s *SpanEmitter) PodRunning(ctx context.Context, attrs map[string]string) {
	jobID := attrs["job_id"]
	if jobID != "" {
		s.registry.MarkRunning(jobID)
		if s.metrics != nil {
			s.metrics.RecordTimingsFromRegistry(ctx, s.registry, jobID, lifecyclePodRunning)
		}
	}
	s.emitLifecycle(ctx, "job.pod_running", attrs, false)
}

// PodCompleted emits job.pod_completed and ends the job trace.
func (s *SpanEmitter) PodCompleted(ctx context.Context, attrs map[string]string) {
	failed := attrs["exit_code"] != "" && attrs["exit_code"] != "0"
	jobID := attrs["job_id"]
	if jobID != "" && s.metrics != nil {
		s.metrics.RecordTimingsFromRegistry(ctx, s.registry, jobID, lifecyclePodCompleted)
	}
	s.emitLifecycle(ctx, "job.pod_completed", attrs, failed)
	if jobID != "" {
		s.registry.EndJob(jobID)
	}
}

// RecordCacheSidecarFailure increments cache sidecar failure metric.
func (s *SpanEmitter) RecordCacheSidecarFailure(ctx context.Context) {
	if s.metrics != nil {
		s.metrics.RecordCacheSidecarFailure(ctx)
	}
}

// CacheSidecarFailed implements informer.LifecycleEmitter.
func (s *SpanEmitter) CacheSidecarFailed(ctx context.Context) {
	s.RecordCacheSidecarFailure(ctx)
}

func (s *SpanEmitter) emitLifecycle(ctx context.Context, name string, attrs map[string]string, failed bool) {
	jobID := attrs["job_id"]
	if jobID == "" {
		s.emit(ctx, name, attrs, failed)
		return
	}
	jobCtx, ok := s.registry.JobCtx(jobID)
	if !ok {
		s.emit(ctx, name, attrs, failed)
		return
	}
	s.emitChild(jobCtx, name, attrs, failed)
}

func (s *SpanEmitter) emit(ctx context.Context, name string, attrs map[string]string, failed bool) {
	_, span := s.tracer.Start(ctx, name, withStringAttrs(attrs))
	if failed {
		span.SetStatus(codes.Error, "pod failed")
	}
	span.End()
}

func (s *SpanEmitter) emitChild(ctx context.Context, name string, attrs map[string]string, failed bool) {
	_, span := s.tracer.Start(ctx, name, withStringAttrs(attrs))
	if failed {
		span.SetStatus(codes.Error, "pod failed")
	}
	span.End()
}

func specAttrs(spec labelquery.RunnerSpec, repo string) map[string]string {
	rawJSON, _ := json.Marshal(spec.Raw)
	return map[string]string{
		"repo":       repo,
		"run_id":     spec.RunID,
		"cpu":        itoa(spec.CPU),
		"arch":       spec.Arch,
		"pool":       spec.Pool,
		"cache":      boolStr(spec.Cache),
		"mem":        spec.Mem,
		"image":      spec.Image,
		"raw_labels": string(rawJSON),
	}
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
