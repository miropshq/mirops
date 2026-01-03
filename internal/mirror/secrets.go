package mirror

import (
	"context"
	"fmt"
	//"liveim/pkg/kube"
	//"github.com/liveim/liveim/pkg/kube"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SecretMirrorImpl struct {
	K8sClient client.Client
}

func NewSecretMirror(k8sClient client.Client) SecretMirrorI {
	return &SecretMirrorImpl{
		K8sClient: k8sClient,
	}
}

func (s *SecretMirrorImpl) MirrorSecret(ctx context.Context, name, sourceNS, targetNS string) error {

	// Get secret from source
	src := &corev1.Secret{}
	if err := s.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: sourceNS}, src); err != nil {
		return fmt.Errorf("failed to get secret %s/%s: %w", sourceNS, name, err)
	}

	dst := src.DeepCopy()
	dst.ResourceVersion = ""
	dst.UID = ""
	dst.CreationTimestamp = metav1.Time{}
	dst.ManagedFields = nil
	dst.Namespace = targetNS

	existing := &corev1.Secret{}
	err := s.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNS}, existing)

	if err == nil {
		// SECRET YA EXISTE → ver si es immutable
		if existing.Immutable != nil && *existing.Immutable {
			// Borrar y recrear
			_ = s.K8sClient.Delete(ctx, existing)
			return s.K8sClient.Create(ctx, dst)
		}

		// Actualizar campos permitidos
		existing.Data = dst.Data
		existing.Type = dst.Type
		existing.Labels = dst.Labels
		existing.Annotations = dst.Annotations
		existing.Immutable = dst.Immutable

		return s.K8sClient.Update(ctx, existing)
	}
	// // Copy the secret into the target namespace
	// copy := &corev1.Secret{
	// 	ObjectMeta: metav1.ObjectMeta{
	// 		Name:      src.Name,
	// 		Namespace: targetNS,
	// 	},
	// 	Data: src.Data,
	// 	Type: src.Type,
	// }

	// // Create or Update
	// existing := &corev1.Secret{}
	// err := s.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNS}, existing)
	// if err == nil {
	// 	// Update
	// 	existing.Data = src.Data
	// 	existing.Type = src.Type
	// 	return s.K8sClient.Update(ctx, existing)
	// }

	// Create
	//return s.K8sClient.Create(ctx, copy)
	// SECRET NO EXISTE → crear
	return s.K8sClient.Create(ctx, dst)
}

// func (s *SecretMirrorImpl) SyncSecret(ctx context.Context, name, sourceNS, targetNS string) error {
// 	 return s.MirrorSecret(ctx, name, sourceNS, targetNS)
// }

// List all secret names in a namespace
func (s *SecretMirrorImpl) ListSecrets(ctx context.Context, namespace string) ([]string, error) {
	list := &corev1.SecretList{}
	if err := s.K8sClient.List(ctx, list, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, err
	}

	names := []string{}
	for _, sec := range list.Items {
		names = append(names, sec.Name)
	}

	return names, nil
}
