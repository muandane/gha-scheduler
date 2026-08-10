package informer_test

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/muandane/gha-scheduler/internal/informer"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

type recordingEmitter struct {
	events []string
	attrs  []map[string]string
}

func (r *recordingEmitter) PodScheduled(_ context.Context, attrs map[string]string) {
	r.events = append(r.events, "job.pod_scheduled")
	r.attrs = append(r.attrs, attrs)
}

func (r *recordingEmitter) PodRunning(_ context.Context, attrs map[string]string) {
	r.events = append(r.events, "job.pod_running")
	r.attrs = append(r.attrs, attrs)
}

func (r *recordingEmitter) PodCompleted(_ context.Context, attrs map[string]string) {
	r.events = append(r.events, "job.pod_completed")
	r.attrs = append(r.attrs, attrs)
}

func (r *recordingEmitter) CacheSidecarFailed(_ context.Context) {
	r.events = append(r.events, "cache_sidecar_failed")
}

func pod(name string, phase corev1.PodPhase, scheduled, running bool) *corev1.Pod {
	now := metav1.NewTime(time.Unix(100, 0))
	conditions := []corev1.PodCondition{}
	if scheduled {
		conditions = append(conditions, corev1.PodCondition{
			Type:               corev1.PodScheduled,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: now,
		})
	}
	status := corev1.PodStatus{Phase: phase, Conditions: conditions}
	if running {
		status.ContainerStatuses = []corev1.ContainerStatus{{State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{StartedAt: now},
		}}}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8sjob.LabelRunID: "100",
				k8sjob.LabelJobID: "200",
			},
		},
		Status: status,
	}
}

func TestPodWatchEmitsLifecycleSpans(t *testing.T) {
	rec := &recordingEmitter{}
	w := informer.NewPodWatcher(rec)

	w.OnUpdate(context.Background(), pod("p1", corev1.PodPending, false, false), pod("p1", corev1.PodPending, true, false))
	w.OnUpdate(context.Background(), pod("p1", corev1.PodPending, true, false), pod("p1", corev1.PodRunning, true, true))
	w.OnUpdate(context.Background(), pod("p1", corev1.PodRunning, true, true), pod("p1", corev1.PodSucceeded, true, true))

	want := []string{"job.pod_scheduled", "job.pod_running", "job.pod_completed"}
	if len(rec.events) != len(want) {
		t.Fatalf("events: %v", rec.events)
	}
	for i, e := range want {
		if rec.events[i] != e {
			t.Fatalf("event[%d]: got %s want %s", i, rec.events[i], e)
		}
		if rec.attrs[i]["run_id"] != "100" || rec.attrs[i]["job_id"] != "200" {
			t.Fatalf("attrs[%d]: %v", i, rec.attrs[i])
		}
	}
}

func TestPodWatchDoesNotRepeatSpans(t *testing.T) {
	rec := &recordingEmitter{}
	w := informer.NewPodWatcher(rec)
	running := pod("p1", corev1.PodRunning, true, true)
	w.OnUpdate(context.Background(), running, running)
	if len(rec.events) != 0 {
		t.Fatalf("expected no duplicate events, got %v", rec.events)
	}
}

func TestPodWatchEmitsRunnerExit(t *testing.T) {
	rec := &recordingEmitter{}
	w := informer.NewPodWatcher(rec)
	var exited string
	w.SetOnRunnerExit(func(_ context.Context, jobID string) {
		exited = jobID
	})

	oldPod := podWithSidecar("p1", corev1.PodRunning, true, true, false)
	oldPod.Labels[k8sjob.LabelGHJob] = "200"
	newPod := podWithSidecar("p1", corev1.PodRunning, true, false, false)
	newPod.Labels[k8sjob.LabelGHJob] = "200"
	newPod.Status.ContainerStatuses[0] = corev1.ContainerStatus{
		Name: "runner",
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, FinishedAt: metav1.Now()},
		},
	}
	w.OnUpdate(context.Background(), oldPod, newPod)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && exited == "" {
		time.Sleep(5 * time.Millisecond)
	}
	if exited != "200" {
		t.Fatalf("runner exit job_id: got %q want 200", exited)
	}
}

func TestPodWatchEmitsCacheSidecarFailure(t *testing.T) {
	rec := &recordingEmitter{}
	w := informer.NewPodWatcher(rec)

	oldPod := podWithSidecar("p1", corev1.PodRunning, true, true, false)
	newPod := podWithSidecar("p1", corev1.PodRunning, true, true, true)
	w.OnUpdate(context.Background(), oldPod, newPod)

	if len(rec.events) != 1 || rec.events[0] != "cache_sidecar_failed" {
		t.Fatalf("events: %v", rec.events)
	}
}

func podWithSidecar(name string, phase corev1.PodPhase, scheduled, runnerRunning, sidecarFailed bool) *corev1.Pod {
	p := pod(name, phase, scheduled, runnerRunning)
	p.Spec.Containers = []corev1.Container{{Name: "runner"}, {Name: "cache-sidecar"}}
	statuses := []corev1.ContainerStatus{
		{Name: "runner", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
	}
	if sidecarFailed {
		statuses = append(statuses, corev1.ContainerStatus{
			Name: "cache-sidecar",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
			},
		})
	} else {
		statuses = append(statuses, corev1.ContainerStatus{
			Name:  "cache-sidecar",
			Ready: true,
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		})
	}
	p.Status.ContainerStatuses = statuses
	return p
}
