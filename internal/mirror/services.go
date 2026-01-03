package mirror

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceMirrorImpl struct {
	K8sClient client.Client
}

func NewServiceMirror(k8sClient client.Client) ServiceMirrorI {
	return &ServiceMirrorImpl{
		K8sClient: k8sClient,
	}
}

func (s *ServiceMirrorImpl) MirrorService(ctx context.Context, name, sourceNS, targetNS string) error {

	// 1. Get source Service
	src := &corev1.Service{}
	if err := s.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: sourceNS}, src); err != nil {
		return fmt.Errorf("failed to get service %s/%s: %w", sourceNS, name, err)
	}

	// 2. Deep copy
	dst := src.DeepCopy()

	// 3. Clean metadata fields
	dst.ResourceVersion = ""
	dst.UID = ""
	dst.CreationTimestamp = metav1.Time{}
	dst.ManagedFields = nil
	dst.Namespace = targetNS
	dst.OwnerReferences = nil

	// 4. Check if service exists in target
	existing := &corev1.Service{}
	err := s.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNS}, existing)

	if err == nil {
		// ---------- UPDATE EXISTING SERVICE ----------
		// Preserve immutable fields
		dst.Spec.ClusterIP = existing.Spec.ClusterIP
		dst.Spec.ClusterIPs = existing.Spec.ClusterIPs
		dst.Spec.IPFamilies = existing.Spec.IPFamilies
		dst.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy

		// Apply update to the existing object
		existing.Labels = dst.Labels
		existing.Annotations = dst.Annotations
		existing.Spec = dst.Spec

		return s.K8sClient.Update(ctx, existing)
	}

	// ---------- CREATE NEW SERVICE ----------
	// Allow API server to assign ClusterIP
	dst.Spec.ClusterIP = ""
	dst.Spec.ClusterIPs = nil

	return s.K8sClient.Create(ctx, dst)
}

func (s *ServiceMirrorImpl) ListServices(ctx context.Context, namespace string) ([]string, error) {
	list := &corev1.ServiceList{}
	if err := s.K8sClient.List(ctx, list, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(list.Items))
	for _, svc := range list.Items {
		names = append(names, svc.Name)
	}

	return names, nil
}
