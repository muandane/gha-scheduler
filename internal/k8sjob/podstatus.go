package k8sjob

import corev1 "k8s.io/api/core/v1"

const RunnerContainerName = "runner"

// RunnerContainerState returns the runner container state if present.
func RunnerContainerState(pod *corev1.Pod) (corev1.ContainerState, bool) {
	if pod == nil {
		return corev1.ContainerState{}, false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == RunnerContainerName {
			return cs.State, true
		}
	}
	return corev1.ContainerState{}, false
}

// RunnerCleanupReason reports why a runner pod should be deleted, or "" if still healthy.
func RunnerCleanupReason(state corev1.ContainerState) string {
	if state.Terminated != nil {
		return "runner_terminated"
	}
	if waiting := state.Waiting; waiting != nil {
		switch waiting.Reason {
		case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "InvalidImageName":
			return "runner_stuck"
		}
	}
	return ""
}

// PodRunnerCleanupReason returns a cleanup reason for the pod's runner container.
func PodRunnerCleanupReason(pod *corev1.Pod) string {
	state, ok := RunnerContainerState(pod)
	if !ok {
		return ""
	}
	return RunnerCleanupReason(state)
}
