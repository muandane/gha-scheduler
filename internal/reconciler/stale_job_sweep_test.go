package reconciler

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/muandane/gha-scheduler/internal/cleanup"
	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

type fakeWorkflowJobGH struct {
	jobs map[int64]ghclient.WorkflowJob
}

func (f *fakeWorkflowJobGH) GetWorkflowJob(_ context.Context, _, _ string, jobID int64) (ghclient.WorkflowJob, error) {
	return f.jobs[jobID], nil
}

type fakeStaleMetrics struct {
	found   int
	deleted int
	skipped map[string]int
}

func (m *fakeStaleMetrics) RecordStaleJobFound(_ context.Context)        { m.found++ }
func (m *fakeStaleMetrics) RecordJobDeleted(_ context.Context, _ string) { m.deleted++ }
func (m *fakeStaleMetrics) RecordJobCleanupSkipped(_ context.Context, reason string) {
	if m.skipped == nil {
		m.skipped = make(map[string]int)
	}
	m.skipped[reason]++
}
func (m *fakeStaleMetrics) RecordJobCleanupError(_ context.Context) {}

func newStaleSweepTest(t *testing.T, k8sJobs []*batchv1.Job, pods []corev1.Pod, gh *fakeWorkflowJobGH) (*StaleJobSweep, *fakeStaleMetrics) {
	t.Helper()
	objs := make([]any, 0, len(k8sJobs)+len(pods))
	for _, j := range k8sJobs {
		objs = append(objs, j)
	}
	for i := range pods {
		objs = append(objs, &pods[i])
	}
	k8s := fake.NewSimpleClientset()
	for _, obj := range objs {
		switch v := obj.(type) {
		case *batchv1.Job:
			_, _ = k8s.BatchV1().Jobs(v.Namespace).Create(context.Background(), v, metav1.CreateOptions{})
		case *corev1.Pod:
			_, _ = k8s.CoreV1().Pods(v.Namespace).Create(context.Background(), v, metav1.CreateOptions{})
		}
	}
	metrics := &fakeStaleMetrics{}
	cleaner := cleanup.NewJobCleaner(cleanup.Config{Namespace: "gha-runners", Metrics: metrics}, k8s, &fakeLeaseChecker{})
	sweep := NewStaleJobSweep(StaleJobSweepConfig{
		Namespace:        "gha-runners",
		CleanupGrace:     30 * time.Second,
		StuckThreshold:   15 * time.Minute,
		MaxRuntime:       6 * time.Hour,
		GHStatusCacheTTL: 2 * time.Minute,
		Metrics:          metrics,
	}, gh, k8s, cleaner, &fakeLeaseChecker{})
	return sweep, metrics
}

func TestStaleJobSweep_ghCompletedJobDeleted(t *testing.T) {
	completedAt := time.Now().Add(-2 * time.Minute)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "ghs-job-100-200",
			Namespace:         "gha-runners",
			Labels:            map[string]string{k8sjob.LabelGHJob: "200", k8sjob.LabelOwner: "org", k8sjob.LabelRepo: "repo"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
		},
	}
	gh := &fakeWorkflowJobGH{jobs: map[int64]ghclient.WorkflowJob{
		200: {ID: 200, Status: "completed", CompletedAt: completedAt},
	}}
	sweep, metrics := newStaleSweepTest(t, []*batchv1.Job{job}, nil, gh)

	if err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if metrics.found != 1 {
		t.Fatalf("found: got %d want 1", metrics.found)
	}
	if metrics.deleted != 1 {
		t.Fatalf("deleted: got %d want 1", metrics.deleted)
	}
}

func TestStaleJobSweep_stuckPendingDeleted(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "ghs-job-100-201",
			Namespace:         "gha-runners",
			Labels:            map[string]string{k8sjob.LabelGHJob: "201", k8sjob.LabelOwner: "org", k8sjob.LabelRepo: "repo"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-20 * time.Minute)),
		},
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ghs-job-100-201-abc",
			Namespace: "gha-runners",
			Labels:    map[string]string{k8sjob.LabelGHJob: "201"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	sweep, metrics := newStaleSweepTest(t, []*batchv1.Job{job}, []corev1.Pod{pod}, &fakeWorkflowJobGH{jobs: map[int64]ghclient.WorkflowJob{}})

	if err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if metrics.deleted != 1 {
		t.Fatalf("deleted: got %d want 1", metrics.deleted)
	}
}

func TestStaleJobSweep_terminalPodDeleted(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "ghs-job-100-202",
			Namespace:         "gha-runners",
			Labels:            map[string]string{k8sjob.LabelGHJob: "202"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-5 * time.Minute)),
		},
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ghs-job-100-202-abc",
			Namespace: "gha-runners",
			Labels:    map[string]string{k8sjob.LabelGHJob: "202"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	sweep, metrics := newStaleSweepTest(t, []*batchv1.Job{job}, []corev1.Pod{pod}, &fakeWorkflowJobGH{jobs: map[int64]ghclient.WorkflowJob{}})

	if err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if metrics.deleted != 1 {
		t.Fatalf("deleted: got %d want 1", metrics.deleted)
	}
}
