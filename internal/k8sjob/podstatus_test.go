package k8sjob_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

func TestPodRunnerCleanupReason(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "runner",
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{ExitCode: 1},
				},
			}},
		},
	}
	if got := k8sjob.PodRunnerCleanupReason(pod); got != "runner_terminated" {
		t.Fatalf("got %q want runner_terminated", got)
	}
}
