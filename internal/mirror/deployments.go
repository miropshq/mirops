package mirror

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentMirrorImpl struct {
	K8sClient client.Client
}

func NewDeploymentMirror(k8sClient client.Client) DeploymentMirrorI {
	return &DeploymentMirrorImpl{
		K8sClient: k8sClient,
	}
}

func (d *DeploymentMirrorImpl) MirrorDeployment(ctx context.Context, name, sourceNS, targetNS string) error {

	// 1. Get source Deployment
	src := &appsv1.Deployment{}
	if err := d.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: sourceNS}, src); err != nil {
		return fmt.Errorf("failed to get deployment %s/%s: %w", sourceNS, name, err)
	}

	// 2. Deep copy
	dst := src.DeepCopy()

	// 3. Clean metadata fields that cannot be copied
	dst.ResourceVersion = ""
	dst.UID = ""
	dst.CreationTimestamp = metav1.Time{}
	dst.ManagedFields = nil
	dst.Namespace = targetNS

	// It is safe to clear OwnerReferences so the object won't be deleted by a controller
	dst.OwnerReferences = nil

	// 4. Try to get the deployment in target namespace
	existing := &appsv1.Deployment{}
	err := d.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNS}, existing)

	if err == nil {
		// 5. Update existing deployment

		// Update fields manually (don't overwrite entire object)
		existing.Labels = dst.Labels
		existing.Annotations = dst.Annotations
		existing.Spec = dst.Spec

		return d.K8sClient.Update(ctx, existing)
	}

	// 6. Deployment does not exist — create it
	return d.K8sClient.Create(ctx, dst)
}

func (d *DeploymentMirrorImpl) ListDeployments(ctx context.Context, namespace string) ([]string, error) {
	list := &appsv1.DeploymentList{}
	if err := d.K8sClient.List(ctx, list, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(list.Items))
	for _, dep := range list.Items {
		names = append(names, dep.Name)
	}

	return names, nil
}
