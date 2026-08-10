package cleanup

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

type fakeLeaseChecker struct {
	locked map[string]bool
}

func (f *fakeLeaseChecker) IsDispatchLocked(_ context.Context, jobID string) (bool, error) {
	return f.locked[jobID], nil
}

type fakeCleanupMetrics struct {
	deleted int
	skipped map[string]int
	errors  int
}

func (m *fakeCleanupMetrics) RecordJobDeleted(_ context.Context, _ string) { m.deleted++ }
func (m *fakeCleanupMetrics) RecordJobCleanupSkipped(_ context.Context, reason string) {
	if m.skipped == nil {
		m.skipped = make(map[string]int)
	}
	m.skipped[reason]++
}
func (m *fakeCleanupMetrics) RecordJobCleanupError(_ context.Context) { m.errors++ }

func TestJobCleaner_deletesMatchingJob(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ghs-job-100-200",
			Namespace: "gha-runners",
			Labels:    map[string]string{k8sjob.LabelGHJob: "200"},
		},
	}
	k8s := fake.NewSimpleClientset(job)
	metrics := &fakeCleanupMetrics{}
	cleaner := NewJobCleaner(Config{Namespace: "gha-runners", Metrics: metrics}, k8s, &fakeLeaseChecker{})

	n, err := cleaner.CleanupByJobID(context.Background(), "200", "test")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted: got %d want 1", n)
	}
	if metrics.deleted != 1 {
		t.Fatalf("metric deleted: got %d want 1", metrics.deleted)
	}
	remaining, _ := k8s.BatchV1().Jobs("gha-runners").List(context.Background(), metav1.ListOptions{})
	if len(remaining.Items) != 0 {
		t.Fatalf("expected no jobs remaining, got %d", len(remaining.Items))
	}
}

func TestJobCleaner_skipsWhenDispatchLocked(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ghs-job-100-200",
			Namespace: "gha-runners",
			Labels:    map[string]string{k8sjob.LabelGHJob: "200"},
		},
	}
	k8s := fake.NewSimpleClientset(job)
	metrics := &fakeCleanupMetrics{}
	cleaner := NewJobCleaner(Config{Namespace: "gha-runners", Metrics: metrics}, k8s, &fakeLeaseChecker{locked: map[string]bool{"200": true}})

	n, err := cleaner.CleanupByJobID(context.Background(), "200", "test")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("deleted: got %d want 0", n)
	}
	if metrics.skipped["locked"] != 1 {
		t.Fatalf("expected locked skip, got %v", metrics.skipped)
	}
}

func TestJobCleaner_idempotentWhenNoJob(t *testing.T) {
	k8s := fake.NewSimpleClientset()
	cleaner := NewJobCleaner(Config{Namespace: "gha-runners"}, k8s, nil)

	n, err := cleaner.CleanupByJobID(context.Background(), "999", "test")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("deleted: got %d want 0", n)
	}
}
