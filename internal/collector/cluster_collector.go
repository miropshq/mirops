package collector

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type DefaultClusterCollector struct {
	Client             client.Client
	WorkloadCollectors []WorkloadCollector
}

func NewClusterCollector(c client.Client) ClusterCollectorI {
	return &DefaultClusterCollector{
		Client: c,
		WorkloadCollectors: []WorkloadCollector{
			NewDeploymentCollector(c),
		},
	}
}

func (c *DefaultClusterCollector) Collect(ctx context.Context) (*ClusterSnapshot, error) {
	snapshot := &ClusterSnapshot{}

	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	version, err := discoveryClient.ServerVersion()
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
