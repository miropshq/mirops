package mirror

import (
	"context"
	"fmt"
)

type NamespaceMirror struct {
	SecretMirror     SecretMirrorI
	ConfigMapMirror  ConfigMapMirrorI
	DeploymentMirror DeploymentMirrorI
	ServiceMirror    ServiceMirrorI
	ReplicaSetMirror ReplicaSetMirrorI
	PodMirror        PodMirrorI
}

func NewNamespaceMirror(secretMirror SecretMirrorI, cmMirror ConfigMapMirrorI, dep DeploymentMirrorI, svc ServiceMirrorI, replicaset ReplicaSetMirrorI, pod PodMirrorI) *NamespaceMirror {
	return &NamespaceMirror{
		SecretMirror:     secretMirror,
		ConfigMapMirror:  cmMirror,
		DeploymentMirror: dep,
		ServiceMirror:    svc,
		ReplicaSetMirror: replicaset,
		PodMirror:        pod,
	}
}

func (n *NamespaceMirror) MirrorNamespaceSecrets(ctx context.Context, sourceNS, targetNS string, secrets []string) error {

	if len(secrets) == 0 {
		list, err := n.SecretMirror.ListSecrets(ctx, sourceNS)
		if err != nil {
			return fmt.Errorf("failed to list secrets in %s: %w", sourceNS, err)
		}
		secrets = list
	}

	for _, name := range secrets {
		fmt.Printf("Mirroring secret %s from %s -> %s\n", name, sourceNS, targetNS)
		if err := n.SecretMirror.MirrorSecret(ctx, name, sourceNS, targetNS); err != nil {
			return err
		}
	}
	return nil
}

func (n *NamespaceMirror) MirrorNamespaceConfigMaps(ctx context.Context, sourceNS, targetNS string, cms []string) error {

	if len(cms) == 0 {
		list, err := n.ConfigMapMirror.ListConfigMaps(ctx, sourceNS)
		if err != nil {
			return fmt.Errorf("failed to list configmaps in %s: %w", sourceNS, err)
		}
		cms = list
	}

	for _, name := range cms {
		fmt.Printf("Mirroring ConfigMap %s from %s -> %s\n", name, sourceNS, targetNS)
		if err := n.ConfigMapMirror.MirrorConfigMap(ctx, name, sourceNS, targetNS); err != nil {
			return err
		}
	}
	return nil
}

func (n *NamespaceMirror) MirrorNamespaceDeployments(ctx context.Context, sourceNS, targetNS string, deployments []string) error {

	if len(deployments) == 0 {
		list, err := n.DeploymentMirror.ListDeployments(ctx, sourceNS)
		if err != nil {
			return fmt.Errorf("failed to list deployments in %s: %w", sourceNS, err)
		}
		deployments = list
	}

	for _, name := range deployments {
		fmt.Printf("Mirroring Deployment %s from %s -> %s\n", name, sourceNS, targetNS)
		if err := n.DeploymentMirror.MirrorDeployment(ctx, name, sourceNS, targetNS); err != nil {
			return err
		}
	}

	return nil
}

func (n *NamespaceMirror) MirrorNamespaceServices(ctx context.Context, sourceNS, targetNS string, services []string) error {

	if len(services) == 0 {
		list, err := n.ServiceMirror.ListServices(ctx, sourceNS)
		if err != nil {
			return fmt.Errorf("failed to list services in %s: %w", sourceNS, err)
		}
		services = list
	}

	for _, name := range services {
		fmt.Printf("Mirroring Service %s from %s -> %s\n", name, sourceNS, targetNS)
		if err := n.ServiceMirror.MirrorService(ctx, name, sourceNS, targetNS); err != nil {
			return err
		}
	}

	return nil
}

func (n *NamespaceMirror) MirrorNamespaceReplicaSets(ctx context.Context, sourceNS, targetNS string, replicasets []string) error {

	if len(replicasets) == 0 {
		list, err := n.ReplicaSetMirror.ListReplicaSets(ctx, sourceNS)
		if err != nil {
			return fmt.Errorf("failed to list replicasets in %s: %w", sourceNS, err)
		}
		replicasets = list
	}

	for _, name := range replicasets {
		fmt.Printf("Mirroring Replica Sets %s from %s -> %s\n", name, sourceNS, targetNS)
		if err := n.ReplicaSetMirror.MirrorReplicaSet(ctx, name, sourceNS, targetNS); err != nil {
			return err
		}
	}

	return nil
}

func (n *NamespaceMirror) MirrorNamespacePods(ctx context.Context, sourceNS, targetNS string, pods []string) error {

	if len(pods) == 0 {
		list, err := n.PodMirror.ListPods(ctx, sourceNS)
		if err != nil {
			return fmt.Errorf("failed to list pods in %s: %w", sourceNS, err)
		}
		pods = list
	}

	for _, name := range pods {
		fmt.Printf("Mirroring Pod %s from %s -> %s\n", name, sourceNS, targetNS)
		if err := n.PodMirror.MirrorPod(ctx, name, sourceNS, targetNS); err != nil {
			return err
		}
	}

	return nil
}
