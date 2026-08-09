package tracing_test

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/muandane/gha-scheduler/internal/labelquery"
	"github.com/muandane/gha-scheduler/internal/store"
	sqlitestore "github.com/muandane/gha-scheduler/internal/store/sqlite"
	"github.com/muandane/gha-scheduler/internal/tracing"
)

func TestStoreEmitterPersistsLifecycle(t *testing.T) {
	st, err := sqlitestore.Open(t.TempDir() + "/jobs.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")
	span := tracing.NewSpanEmitter(tracer, nil, nil)
	emitter := tracing.NewStoreEmitter(span, st)

	ctx := context.Background()
	emitter.WebhookReceived(ctx, map[string]string{
		"repo": "org/repo", "run_id": "1", "job_id": "99",
	})
	spec := labelquery.RunnerSpec{CPU: 2, Arch: "x64", Pool: "default"}
	emitter.DispatchStarted(ctx, "99", spec, "org/repo")
	emitter.JobCreated("99")
	emitter.PodScheduled(ctx, map[string]string{"job_id": "99", "pod": "p1"})
	emitter.PodRunning(ctx, map[string]string{"job_id": "99"})
	emitter.PodCompleted(ctx, map[string]string{"job_id": "99", "exit_code": "0"})

	job, err := st.GetJob(ctx, "99")
	if err != nil {
		t.Fatal(err)
	}
	if job == nil {
		t.Fatal("expected job")
	}
	if job.Status != store.StatusSucceeded {
		t.Fatalf("status %s", job.Status)
	}
	if job.CPU != 2 {
		t.Fatalf("cpu %d", job.CPU)
	}
	if job.PodName != "p1" {
		t.Fatalf("pod %q", job.PodName)
	}
	if job.CompletedAt.IsZero() {
		t.Fatal("expected completed_at")
	}
	_ = time.Now()
}
