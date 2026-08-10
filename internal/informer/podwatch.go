package informer

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"

	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

// LifecycleEmitter receives pod lifecycle events.
type LifecycleEmitter interface {
	PodScheduled(ctx context.Context, attrs map[string]string)
	PodRunning(ctx context.Context, attrs map[string]string)
	PodCompleted(ctx context.Context, attrs map[string]string)
	CacheSidecarFailed(ctx context.Context)
}

// PodWatcher maps pod updates to lifecycle span emissions.
type PodWatcher struct {
	emitter      LifecycleEmitter
	onRunnerExit func(ctx context.Context, jobID string)
	mu           sync.Mutex
	seen         map[string]map[string]struct{}
}

// NewPodWatcher creates a PodWatcher.
func NewPodWatcher(emitter LifecycleEmitter) *PodWatcher {
	return &PodWatcher{
		emitter: emitter,
		seen:    make(map[string]map[string]struct{}),
	}
}

// SetOnRunnerExit registers a callback when the runner container exits or fails to start.
func (w *PodWatcher) SetOnRunnerExit(fn func(ctx context.Context, jobID string)) {
	w.onRunnerExit = fn
}

// OnUpdate handles pod update events from an informer.
func (w *PodWatcher) OnUpdate(ctx context.Context, oldPod, newPod *corev1.Pod) {
	if newPod == nil {
		return
	}
	w.processPod(ctx, newPod, oldPod)
}

// OnAdd handles pod add events (fast-completing jobs that skip Running updates).
func (w *PodWatcher) OnAdd(ctx context.Context, pod *corev1.Pod) {
	if pod == nil {
		return
	}
	w.processPod(ctx, pod, nil)
}

func (w *PodWatcher) processPod(ctx context.Context, pod, oldPod *corev1.Pod) {
	attrs := podAttrs(pod)
	podKey := pod.Namespace + "/" + pod.Name

	if !wasScheduled(oldPod) && isScheduled(pod) {
		w.emitOnce(podKey, "scheduled", func() {
			w.emitter.PodScheduled(ctx, attrs)
		})
	}
	if !wasRunning(oldPod) && isRunning(pod) {
		w.emitOnce(podKey, "running", func() {
			w.emitter.PodRunning(ctx, attrs)
		})
	} else if oldPod == nil && isTerminal(pod) && !wasRunning(pod) {
		w.emitOnce(podKey, "running", func() {
			w.emitter.PodRunning(ctx, attrs)
		})
	}
	if shouldReportCacheSidecarFailure(pod) {
		w.emitOnce(podKey, "cache_sidecar_failed", func() {
			w.emitter.CacheSidecarFailed(ctx)
		})
	}
	w.maybeRunnerExit(ctx, podKey, pod, oldPod)
	if !isTerminal(oldPod) && isTerminal(pod) {
		attrs["exit_code"] = exitCode(pod)
		w.emitOnce(podKey, "completed", func() {
			w.emitter.PodCompleted(ctx, attrs)
		})
	}
}

func (w *PodWatcher) maybeRunnerExit(ctx context.Context, podKey string, pod, oldPod *corev1.Pod) {
	if w.onRunnerExit == nil {
		return
	}
	jobID := pod.Labels[k8sjob.LabelGHJob]
	if jobID == "" {
		return
	}
	nowReason := k8sjob.PodRunnerCleanupReason(pod)
	if nowReason == "" {
		return
	}
	oldReason := ""
	if oldPod != nil {
		oldReason = k8sjob.PodRunnerCleanupReason(oldPod)
	}
	if oldReason != "" {
		return
	}
	w.emitOnce(podKey, "runner_exit", func() {
		w.onRunnerExit(ctx, jobID)
	})
}

func (w *PodWatcher) emitOnce(podKey, event string, fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seen[podKey] == nil {
		w.seen[podKey] = make(map[string]struct{})
	}
	if _, ok := w.seen[podKey][event]; ok {
		return
	}
	w.seen[podKey][event] = struct{}{}
	fn()
}

func podAttrs(pod *corev1.Pod) map[string]string {
	return map[string]string{
		"run_id": pod.Labels[k8sjob.LabelRunID],
		"job_id": pod.Labels[k8sjob.LabelJobID],
		"pod":    pod.Name,
		"phase":  string(pod.Status.Phase),
	}
}

func isScheduled(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func wasScheduled(pod *corev1.Pod) bool { return isScheduled(pod) }

func isRunning(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Running != nil {
			return true
		}
	}
	return pod.Status.Phase == corev1.PodRunning
}

func wasRunning(pod *corev1.Pod) bool { return isRunning(pod) }

func shouldReportCacheSidecarFailure(pod *corev1.Pod) bool {
	if pod == nil || !hasCacheSidecarContainer(pod) {
		return false
	}
	runnerUp := false
	sidecarBad := false
	for _, cs := range pod.Status.ContainerStatuses {
		switch cs.Name {
		case "runner":
			runnerUp = cs.State.Running != nil
		case "cache-sidecar":
			if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
				sidecarBad = true
			}
			if cs.State.Waiting != nil {
				sidecarBad = true
			}
			if cs.RestartCount > 0 && !cs.Ready {
				sidecarBad = true
			}
		}
	}
	return runnerUp && sidecarBad
}

func hasCacheSidecarContainer(pod *corev1.Pod) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name == "cache-sidecar" {
			return true
		}
	}
	return false
}

func isTerminal(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

func exitCode(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil {
			return itoa(int(cs.State.Terminated.ExitCode))
		}
	}
	if pod.Status.Phase == corev1.PodSucceeded {
		return "0"
	}
	return "1"
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
