package collector

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentCollector struct {
	Client client.Client
}

func (d *DeploymentCollector) Collect(ctx context.Context) ([]ResourceRef, error) {
	list := &appsv1.DeploymentList{}
	if err := d.Client.List(ctx, list); err != nil {
		return nil, err
	}

	result := make([]ResourceRef, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, ResourceRef{
			Namespace:  item.Namespace,
			Name:       item.Name,
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		})
	}
	return result, nil
}

func NewDeploymentCollector(c client.Client) WorkloadCollector {
	return &DeploymentCollector{Client: c}
}
