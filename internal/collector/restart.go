package collector

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// AbnormalRestartRate24h is the projected restarts/24h above which a pod is considered to
// be restarting abnormally (a restart roughly every ~2.4h).
const AbnormalRestartRate24h = 10

// isRestartingAbnormally reports whether a pod is a currently-crashing service whose
// projected restart rate exceeds the abnormal threshold.
func isRestartingAbnormally(pod *corev1.Pod, now time.Time) bool {
	return podRestartRate24h(pod, now) >= AbnormalRestartRate24h
}

// isCurrentlyCrashing reports whether a pod is actively unhealthy right now (not a one-off
// restart that already recovered). Only long-running services count: workloads with
// restartPolicy != Always (Jobs / batch) retry by design, so their restarts are expected
// and must not be treated as a health problem.
func isCurrentlyCrashing(pod *corev1.Pod) bool {
	if pod.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil {
			switch w.Reason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull":
				return true
			}
		}
	}
	return false
}

// podRestartRate24h projects a currently-crashing pod's cumulative restarts to a per-24h
// rate (restarts / age × 24). Healthy pods and non-service workloads return 0. The 1h age
// floor prevents a single restart on a brand-new pod from projecting to an extreme rate.
func podRestartRate24h(pod *corev1.Pod, now time.Time) float64 {
	if !isCurrentlyCrashing(pod) {
		return 0
	}
	var restarts int32
	for _, cs := range pod.Status.ContainerStatuses {
		restarts += cs.RestartCount
	}
	if restarts == 0 {
		return 0
	}
	ageHours := now.Sub(pod.CreationTimestamp.Time).Hours()
	if ageHours < 1 {
		ageHours = 1
	}
	return float64(restarts) / ageHours * 24
}
