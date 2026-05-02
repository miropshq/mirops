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

func (c *DefaultClusterCollector) Collect(ctx context.Context) (*ClusterSnapshot, error) {
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

	for _, wc := range c.WorkloadCollectors {
		resources, err := wc.Collect(ctx)
		if err != nil {
			return nil, err
		}
		snapshot.Resources = append(snapshot.Resources, resources...)
	}
	return snapshot, nil
}
