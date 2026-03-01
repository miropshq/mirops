package mirror

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ReplicaSetMirrorImpl struct {
	K8sClient client.Client
}

func NewReplicaSetMirror(k8sClient client.Client) ReplicaSetMirrorI {
	return &ReplicaSetMirrorImpl{
		K8sClient: k8sClient,
	}
}

func (r *ReplicaSetMirrorImpl) MirrorReplicaSet(ctx context.Context, name, sourceNS, targetNS string) error {

	src := &appsv1.ReplicaSet{}
	if err := r.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: sourceNS}, src); err != nil {
		return fmt.Errorf("failed to get replicaset %s/%s: %w", sourceNS, name, err)
	}

	dst := src.DeepCopy()

	dst.ResourceVersion = ""
	dst.UID = ""
	dst.CreationTimestamp = metav1.Time{}
	dst.ManagedFields = nil
	dst.Namespace = targetNS
	dst.OwnerReferences = nil

	existing := &appsv1.ReplicaSet{}
	err := r.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNS}, existing)

	if err == nil {
		existing.Labels = dst.Labels
		existing.Annotations = dst.Annotations
		existing.Spec = dst.Spec
		return r.K8sClient.Update(ctx, existing)
	}

	return r.K8sClient.Create(ctx, dst)
}

func (r *ReplicaSetMirrorImpl) ListReplicaSets(ctx context.Context, namespace string) ([]string, error) {
	list := &appsv1.ReplicaSetList{}
	if err := r.K8sClient.List(ctx, list, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, err
	}

	names := []string{}
	for _, rs := range list.Items {
		if !isOwnedByDeployment(rs.OwnerReferences) {
			names = append(names, rs.Name)
		}
	}

	return names, nil
}

func isOwnedByDeployment(owners []metav1.OwnerReference) bool {
	for _, o := range owners {
		if o.Kind == "Deployment" {
			return true
		}
	}
	return false
}
