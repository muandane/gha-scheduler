package tracing

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MeterName is the OpenTelemetry meter scope name.
const MeterName = "github.com/muandane/gha-scheduler"

const attrReason = "reason"

// Metrics holds OTel instruments per SPEC §5.
type Metrics struct {
	dispatchLatency         metric.Float64Histogram
	scheduleLatency         metric.Float64Histogram
	webhookToRunningLatency metric.Float64Histogram
	jobDuration             metric.Float64Histogram
	dispatchErrors          metric.Int64Counter
	cacheSidecarFailures    metric.Int64Counter
	webhooksTotal           metric.Int64Counter
	orphanRunnersDeleted    metric.Int64Counter
	orphanRunnersSkipped    metric.Int64Counter
}

// NewMetrics registers instruments on the given meter.
func NewMetrics(m metric.Meter) (*Metrics, error) {
	dispatchLatency, err := m.Float64Histogram("gha_scheduler.dispatch_latency",
		metric.WithDescription("Seconds from webhook received to k8s Job created"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	scheduleLatency, err := m.Float64Histogram("gha_scheduler.schedule_latency",
		metric.WithDescription("Seconds from k8s Job created to pod running"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	webhookToRunningLatency, err := m.Float64Histogram("gha_scheduler.webhook_to_running_latency",
		metric.WithDescription("Seconds from webhook received to pod running"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	jobDuration, err := m.Float64Histogram("gha_scheduler.job_duration",
		metric.WithDescription("Seconds from pod running to pod completed"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	dispatchErrors, err := m.Int64Counter("gha_scheduler.dispatch_errors_total",
		metric.WithDescription("Dispatch failures by reason"),
	)
	if err != nil {
		return nil, err
	}
	cacheSidecarFailures, err := m.Int64Counter("gha_scheduler.cache_sidecar_failures_total",
		metric.WithDescription("Cache sidecar startup failures"),
	)
	if err != nil {
		return nil, err
	}
	webhooksTotal, err := m.Int64Counter("gha_scheduler.webhooks_total",
		metric.WithDescription("Webhook deliveries by validity"),
	)
	if err != nil {
		return nil, err
	}
	orphanRunnersDeleted, err := m.Int64Counter("gha_scheduler.orphan_runners_deleted_total",
		metric.WithDescription("Orphan GitHub runners deleted"),
	)
	if err != nil {
		return nil, err
	}
	orphanRunnersSkipped, err := m.Int64Counter("gha_scheduler.orphan_runners_skipped_total",
		metric.WithDescription("Orphan runner sweep skips by reason"),
	)
	if err != nil {
		return nil, err
	}
	return &Metrics{
		dispatchLatency:         dispatchLatency,
		scheduleLatency:         scheduleLatency,
		webhookToRunningLatency: webhookToRunningLatency,
		jobDuration:             jobDuration,
		dispatchErrors:          dispatchErrors,
		cacheSidecarFailures:    cacheSidecarFailures,
		webhooksTotal:           webhooksTotal,
		orphanRunnersDeleted:    orphanRunnersDeleted,
		orphanRunnersSkipped:    orphanRunnersSkipped,
	}, nil
}

// RecordDispatchLatency records webhook → Job created duration.
func (m *Metrics) RecordDispatchLatency(ctx context.Context, seconds float64) {
	if m == nil {
		return
	}
	m.dispatchLatency.Record(ctx, seconds)
}

// RecordScheduleLatency records Job created → pod running duration.
func (m *Metrics) RecordScheduleLatency(ctx context.Context, seconds float64) {
	if m == nil {
		return
	}
	m.scheduleLatency.Record(ctx, seconds)
}

// RecordWebhookToRunningLatency records webhook → pod running duration.
func (m *Metrics) RecordWebhookToRunningLatency(ctx context.Context, seconds float64) {
	if m == nil {
		return
	}
	m.webhookToRunningLatency.Record(ctx, seconds)
}

// RecordWebhook records webhook delivery validity.
func (m *Metrics) RecordWebhook(ctx context.Context, valid bool, reason string) {
	if m == nil {
		return
	}
	m.webhooksTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.Bool("valid", valid),
		attribute.String(attrReason, reason),
	))
}

// RecordOrphanRunnerDeleted increments orphan runner deletions.
func (m *Metrics) RecordOrphanRunnerDeleted(ctx context.Context) {
	if m == nil {
		return
	}
	m.orphanRunnersDeleted.Add(ctx, 1)
}

// RecordOrphanRunnerSkipped increments orphan runner skips.
func (m *Metrics) RecordOrphanRunnerSkipped(ctx context.Context, reason string) {
	if m == nil {
		return
	}
	m.orphanRunnersSkipped.Add(ctx, 1, metric.WithAttributes(attribute.String(attrReason, reason)))
}

// RecordJobDuration records pod running → completed duration.
func (m *Metrics) RecordJobDuration(ctx context.Context, seconds float64) {
	if m == nil {
		return
	}
	m.jobDuration.Record(ctx, seconds)
}

// RecordDispatchError increments dispatch error counter.
func (m *Metrics) RecordDispatchError(ctx context.Context, reason string) {
	if m == nil {
		return
	}
	m.dispatchErrors.Add(ctx, 1, metric.WithAttributes(attribute.String(attrReason, reason)))
}

// RecordCacheSidecarFailure increments cache sidecar failure counter.
func (m *Metrics) RecordCacheSidecarFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.cacheSidecarFailures.Add(ctx, 1)
}

// RecordTimingsFromRegistry emits histograms when lifecycle milestones are reached.
func (m *Metrics) RecordTimingsFromRegistry(ctx context.Context, reg *JobTraceRegistry, jobID string, event lifecycleEvent) {
	if m == nil || reg == nil {
		return
	}
	webhook, dispatch, jobCreated, running := reg.Timestamps(jobID)
	now := time.Now()

	switch event {
	case lifecycleDispatchComplete:
		if !webhook.IsZero() && !jobCreated.IsZero() {
			m.RecordDispatchLatency(ctx, jobCreated.Sub(webhook).Seconds())
		}
	case lifecyclePodRunning:
		if !jobCreated.IsZero() && !running.IsZero() {
			m.RecordScheduleLatency(ctx, running.Sub(jobCreated).Seconds())
		}
		if !webhook.IsZero() && !running.IsZero() {
			m.RecordWebhookToRunningLatency(ctx, running.Sub(webhook).Seconds())
		}
	case lifecyclePodCompleted:
		if !running.IsZero() {
			m.RecordJobDuration(ctx, now.Sub(running).Seconds())
		}
	case lifecycleDispatchStart:
		_ = dispatch
	}
}

type lifecycleEvent int

const (
	lifecycleDispatchStart lifecycleEvent = iota
	lifecycleDispatchComplete
	lifecyclePodRunning
	lifecyclePodCompleted
)
