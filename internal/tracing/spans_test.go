package tracing_test

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/muandane/gha-scheduler/internal/labelquery"
	"github.com/muandane/gha-scheduler/internal/tracing"
)

func TestLinkedTracePerJob(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")
	emitter := tracing.NewSpanEmitter(tracer, nil, nil)
	ctx := context.Background()

	attrs := map[string]string{"run_id": "1", "job_id": "42", "repo": "org/repo"}
	emitter.WebhookReceived(ctx, attrs)

	spec := labelquery.RunnerSpec{
		RunID: "1",
		CPU:   4,
		Arch:  "x64",
		Pool:  "spot",
		Cache: true,
	}
	emitter.DispatchStarted(ctx, "42", spec, "org/repo")
	emitter.PodScheduled(ctx, attrs)
	emitter.PodRunning(ctx, attrs)
	emitter.PodCompleted(ctx, map[string]string{"run_id": "1", "job_id": "42", "exit_code": "0"})

	spans := sr.Ended()
	if len(spans) < 4 {
		t.Fatalf("span count: %d", len(spans))
	}

	traceID := spans[0].SpanContext().TraceID()
	for i, sp := range spans {
		if sp.SpanContext().TraceID() != traceID {
			t.Fatalf("span[%d] trace ID mismatch: %s vs %s", i, sp.SpanContext().TraceID(), traceID)
		}
	}

	want := []string{
		"job.dispatch",
		"job.pod_scheduled",
		"job.pod_running",
		"job.pod_completed",
	}
	for i, name := range want {
		if spans[i].Name() != name {
			t.Fatalf("span[%d] name: got %s want %s", i, spans[i].Name(), name)
		}
	}
}

func TestTracingSpanNamesAndAttributes(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("gha-scheduler-test")
	emitter := tracing.NewSpanEmitter(tracer, nil, nil)
	ctx := context.Background()

	attrs := map[string]string{"run_id": "1", "job_id": "2"}
	emitter.WebhookReceived(ctx, attrs)
	emitter.PodScheduled(ctx, attrs)
	emitter.PodRunning(ctx, attrs)
	emitter.PodCompleted(ctx, map[string]string{"run_id": "1", "job_id": "2", "exit_code": "0"})

	spans := sr.Ended()
	if len(spans) < 3 {
		t.Fatalf("span count: %d", len(spans))
	}
}
