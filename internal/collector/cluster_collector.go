package collector

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DefaultClusterCollector struct {
	Client             client.Client
	DiscoveryClient    discovery.DiscoveryInterface
	WorkloadCollectors []WorkloadCollector
}

func NewClusterCollector(c client.Client, dc discovery.DiscoveryInterface) ClusterCollector {
	return &DefaultClusterCollector{
		Client:          c,
		DiscoveryClient: dc,
		WorkloadCollectors: []WorkloadCollector{
			NewDeploymentCollector(c),
		},
	}
}

func (c *DefaultClusterCollector) Collect(ctx context.Context, scope Scope) (*ClusterSnapshot, error) {
	snapshot := &ClusterSnapshot{}

	version, err := c.DiscoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}
	snapshot.ClusterVersion = version.GitVersion

	nsList := &corev1.NamespaceList{}
	if err := c.Client.List(ctx, nsList); err != nil {
		return nil, err
	}
	snapshot.NamespaceCount = len(nsList.Items)

	// Build exclusion set
	excluded := make(map[string]bool)
	for _, ns := range scope.ExcludeNamespaces {
		excluded[ns] = true
	}
	if scope.Mode == "application" {
		for ns := range systemNamespaces {
			excluded[ns] = true
		}
	}

	// Collect pods across all (non-excluded) namespaces
	podList := &corev1.PodList{}
	if err := c.Client.List(ctx, podList); err != nil {
		return nil, err
	}
	for _, pod := range podList.Items {
		if excluded[pod.Namespace] {
			continue
		}
		snapshot.TotalPods++
		restarts := 0
		for _, cs := range pod.Status.ContainerStatuses {
			restarts += int(cs.RestartCount)
		}
		snapshot.TotalRestarts += restarts

		if !isPodReady(&pod) {
			snapshot.NotReadyPods++
			reason := podNotReadyReason(&pod)
			snapshot.PodIssues = append(snapshot.PodIssues, PodIssue{
				Namespace: pod.Namespace,
				Name:      pod.Name,
				Reason:    reason,
				Restarts:  restarts,
			})
		}
	}

	for _, wc := range c.WorkloadCollectors {
		resources, err := wc.Collect(ctx)
		if err != nil {
			return nil, err
		}
		snapshot.Resources = append(snapshot.Resources, resources...)
	}
	return snapshot, nil
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func podNotReadyReason(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		// Waiting state: CrashLoopBackOff, ImagePullBackOff, etc.
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
		// Between restarts: container is Terminated but will restart (CrashLoopBackOff)
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			if cs.RestartCount > 0 {
				return "CrashLoopBackOff"
			}
			return cs.State.Terminated.Reason
		}
	}
	if pod.Status.Phase == corev1.PodRunning {
		return "ReadinessProbeFailed"
	}
	if pod.Status.Phase != "" {
		return string(pod.Status.Phase)
	}
	return "NotReady"
}
