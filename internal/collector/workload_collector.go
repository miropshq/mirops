package collector

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentCollector struct {
	Client client.Client
}

func (d* DeploymentCollector) Collect(ctx context.Context) ([]ResourceRef, error){
	list := &appsv1.DeploymentList{}
	if err := d.Client.List(ctx, list); err != nil {
		return nil, err
	}

	var result []ResourceRef

	for _, item := range list.Items {
		result = append(result, ResourceRef{
			Namespace: item.Namespace,
			Name: item.Name,
			APIVersion: "apps/v1",
			Kind: "Deployment",
		})
	}
	return result, nil
}
