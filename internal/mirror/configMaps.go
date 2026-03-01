package mirror

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigMapMirrorImpl struct {
	K8sClient client.Client
}

func NewConfigMapMirror(k8sClient client.Client) ConfigMapMirrorI {
	return &ConfigMapMirrorImpl{
		K8sClient: k8sClient,
	}
}

func (c *ConfigMapMirrorImpl) MirrorConfigMap(ctx context.Context, name, sourceNS, targetNS string) error {

	// Get the source ConfigMap
	src := &corev1.ConfigMap{}
	if err := c.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: sourceNS}, src); err != nil {
		return fmt.Errorf("failed to get configmap %s/%s: %w", sourceNS, name, err)
	}

	// Deep copy
	dst := src.DeepCopy()

	// Clean metadata fields
	dst.ResourceVersion = ""
	dst.UID = ""
	dst.CreationTimestamp = metav1.Time{}
	dst.ManagedFields = nil

	// Set target namespace
	dst.Namespace = targetNS

	// Check if it already exists on target
	existing := &corev1.ConfigMap{}
	err := c.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNS}, existing)

	if err == nil {
		// Update existing ConfigMap
		existing.Data = dst.Data
		existing.BinaryData = dst.BinaryData
		existing.Labels = dst.Labels
		existing.Annotations = dst.Annotations

		return c.K8sClient.Update(ctx, existing)
	}

	// Create new
	return c.K8sClient.Create(ctx, dst)
}

func (c *ConfigMapMirrorImpl) ListConfigMaps(ctx context.Context, namespace string) ([]string, error) {
	list := &corev1.ConfigMapList{}
	if err := c.K8sClient.List(ctx, list, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, err
	}

	names := []string{}
	for _, cm := range list.Items {
		names = append(names, cm.Name)
	}

	return names, nil
}
