package mirror

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodMirrorImpl struct {
	K8sClient client.Client
}

func NewPodMirror(k8sClient client.Client) PodMirrorI {
	return &PodMirrorImpl{
		K8sClient: k8sClient,
	}
}

func (p *PodMirrorImpl) MirrorPod(ctx context.Context, name, sourceNS, targetNS string) error {
	fmt.Printf("[mirror][pod] Mirroring: %s from %s to %s\n", name, sourceNS, targetNS)
	src := &corev1.Pod{}

	err := p.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: sourceNS}, src)
	if err != nil {
		return fmt.Errorf("failed to get pod %s/%s: %w", sourceNS, name, err)
	}

	dst := src.DeepCopy()

	dst.ResourceVersion = ""
	dst.UID = ""
	dst.SelfLink = ""
	dst.CreationTimestamp = metav1.Time{}
	dst.ManagedFields = nil
	dst.Namespace = targetNS
	dst.OwnerReferences = nil
	dst.Status = corev1.PodStatus{}
	dst.Finalizers = nil
	dst.Spec.NodeName = ""
	dst.Spec.Hostname = ""
	dst.Spec.Subdomain = ""

	// Pods cannot be updated → only recreate
	existing := &corev1.Pod{}
	//err := p.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNS}, existing)

	err = p.K8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNS}, existing)
	if err == nil {
		fmt.Printf("[mirror][pod] Deleting existing pod: %s\n", name)
		_ = p.K8sClient.Delete(ctx, existing)
	}

	err = p.K8sClient.Create(ctx, dst)
	if err != nil {
		fmt.Printf("[mirror][pod] Error creating pod: %s\n", err)
		return err
	}

	fmt.Printf("[mirror][pod] Created: %s in %s\n", name, targetNS)
	return nil

	// return p.K8sClient.Create(ctx, dst)
}

func (p *PodMirrorImpl) ListPods(ctx context.Context, namespace string) ([]string, error) {
	list := &corev1.PodList{}
	if err := p.K8sClient.List(ctx, list, &client.ListOptions{Namespace: namespace}); err != nil {
		fmt.Printf("[mirror][pod] Error listing pods: %s\n", err)
		return nil, err
	}

	fmt.Printf("[mirror][pod] Found: %d\n", len(list.Items))

	names := []string{}
	for _, pod := range list.Items {
		owned := isOwnedByAnyObject(pod.OwnerReferences)
		fmt.Printf("[mirror][pod] Checking: %s (owned: %v)\n", pod.Name, owned)
		if !owned {
			names = append(names, pod.Name)
			fmt.Printf("[mirror][pod] Selected: %s\n", pod.Name)
		}
	}

	return names, nil
}

func isOwnedByAnyObject(owners []metav1.OwnerReference) bool {
	for _, o := range owners {
		// if o.Kind == "ReplicaSet" || o.Kind == "Deployment" {
		// 	return true
		// }
		switch o.Kind {
		case "ReplicaSet", "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob":
			return true
		}
	}
	return false
}
