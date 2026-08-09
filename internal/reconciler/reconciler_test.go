package reconciler_test

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
	"github.com/muandane/gha-scheduler/internal/reconciler"
)

func TestReconcilerSkipsExistingK8sJob(t *testing.T) {
	stale := time.Now().Add(-2 * time.Minute)
	gh := &fakeGH{
		runs: []ghclient.WorkflowRun{{ID: 10}},
		jobs: map[int64][]ghclient.WorkflowJob{
			10: {{ID: 200, RunID: 10, Status: "queued", CreatedAt: stale}},
		},
	}
	d := &fakeDispatch{}
	k8s := fake.NewSimpleClientset()
	_, err := k8s.BatchV1().Jobs("gha-runners").Create(context.Background(), &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing",
			Namespace: "gha-runners",
			Labels:    map[string]string{k8sjob.LabelGHJob: "200"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	r := reconciler.New(reconciler.Config{
		Namespace:      "gha-runners",
		Repos:          []string{"org/repo"},
		StaleThreshold: 30 * time.Second,
	}, gh, d, k8s)

	if err := r.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(d.calls) != 0 {
		t.Fatalf("expected no dispatch, got %d", len(d.calls))
	}
}

type fakeGH struct {
	runs []ghclient.WorkflowRun
	jobs map[int64][]ghclient.WorkflowJob
}

func (f *fakeGH) ListRuns(ctx context.Context, owner, repo string, statuses []string) ([]ghclient.WorkflowRun, error) {
	return f.runs, nil
}

func (f *fakeGH) ListRunJobs(ctx context.Context, owner, repo string, runID int64) ([]ghclient.WorkflowJob, error) {
	return f.jobs[runID], nil
}

type fakeDispatch struct {
	calls []dispatch.Request
}

func (f *fakeDispatch) Dispatch(ctx context.Context, req dispatch.Request) error {
	f.calls = append(f.calls, req)
	return nil
}

func TestReconcilerDispatchesStaleQueuedJob(t *testing.T) {
	stale := time.Now().Add(-2 * time.Minute)
	gh := &fakeGH{
		runs: []ghclient.WorkflowRun{{ID: 10, Status: "queued"}},
		jobs: map[int64][]ghclient.WorkflowJob{
			10: {{
				ID:        200,
				RunID:     10,
				Status:    "queued",
				Labels:    []string{"runs-on=10", "cpu=2"},
				CreatedAt: stale,
			}},
		},
	}
	d := &fakeDispatch{}
	k8s := fake.NewSimpleClientset()

	r := reconciler.New(reconciler.Config{
		Namespace:      "gha-runners",
		Repos:          []string{"org/repo"},
		Interval:       time.Hour,
		StaleThreshold: 30 * time.Second,
		LabelDefaults:  dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	}, gh, d, k8s)

	if err := r.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(d.calls) != 1 {
		t.Fatalf("dispatch calls: %d", len(d.calls))
	}
	if d.calls[0].JobID != "200" || d.calls[0].RunID != "10" {
		t.Fatalf("dispatch req: %+v", d.calls[0])
	}
}

func TestReconcilerSkipsFreshQueuedJob(t *testing.T) {
	fresh := time.Now()
	gh := &fakeGH{
		runs: []ghclient.WorkflowRun{{ID: 10}},
		jobs: map[int64][]ghclient.WorkflowJob{
			10: {{
				ID:        200,
				RunID:     10,
				Status:    "queued",
				CreatedAt: fresh,
			}},
		},
	}
	d := &fakeDispatch{}
	k8s := fake.NewSimpleClientset()

	r := reconciler.New(reconciler.Config{
		Namespace:      "gha-runners",
		Repos:          []string{"org/repo"},
		StaleThreshold: 30 * time.Second,
	}, gh, d, k8s)

	if err := r.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(d.calls) != 0 {
		t.Fatalf("expected no dispatch, got %d", len(d.calls))
	}
}

func TestReconcilerDispatchesQueuedJobInInProgressRun(t *testing.T) {
	stale := time.Now().Add(-2 * time.Minute)
	gh := &fakeGH{
		runs: []ghclient.WorkflowRun{{ID: 10, Status: "in_progress"}},
		jobs: map[int64][]ghclient.WorkflowJob{
			10: {{
				ID:        201,
				RunID:     10,
				Status:    "queued",
				Labels:    []string{"runs-on=10", "cpu=2"},
				CreatedAt: stale,
			}},
		},
	}
	d := &fakeDispatch{}
	k8s := fake.NewSimpleClientset()

	r := reconciler.New(reconciler.Config{
		Namespace:      "gha-runners",
		Repos:          []string{"org/repo"},
		StaleThreshold: 30 * time.Second,
		LabelDefaults:  dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	}, gh, d, k8s)

	if err := r.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(d.calls) != 1 {
		t.Fatalf("dispatch calls: %d", len(d.calls))
	}
	if d.calls[0].JobID != "201" {
		t.Fatalf("dispatch req: %+v", d.calls[0])
	}
}
