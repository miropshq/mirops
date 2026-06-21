package collector

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

// collectServices gathers Services (selector + type) for the dependency graph.
func (c *DefaultClusterCollector) collectServices(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot) error {
	svcList := &corev1.ServiceList{}
	if err := c.Client.List(ctx, svcList); err != nil {
		return err
	}
	for _, svc := range svcList.Items {
		if excluded[svc.Namespace] {
			continue
		}
		snapshot.Services = append(snapshot.Services, ServiceRef{
			Namespace: svc.Namespace,
			Name:      svc.Name,
			Type:      string(svc.Spec.Type),
			Selector:  svc.Spec.Selector,
		})
	}
	return nil
}

// collectIngresses gathers Ingresses and the Services they route to. Listing failures are
// treated as non-fatal (partial mirror is still useful), so it returns no error.
func (c *DefaultClusterCollector) collectIngresses(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot) {
	ingList := &networkingv1.IngressList{}
	if err := c.Client.List(ctx, ingList); err != nil {
		return
	}
	for _, ing := range ingList.Items {
		if excluded[ing.Namespace] {
			continue
		}
		ref := IngressRef{
			Namespace: ing.Namespace,
			Name:      ing.Name,
			HasTLS:    len(ing.Spec.TLS) > 0,
		}
		seen := make(map[string]bool)
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil && !seen[path.Backend.Service.Name] {
					seen[path.Backend.Service.Name] = true
					ref.Services = append(ref.Services, path.Backend.Service.Name)
				}
			}
		}
		if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil && !seen[ing.Spec.DefaultBackend.Service.Name] {
			ref.Services = append(ref.Services, ing.Spec.DefaultBackend.Service.Name)
		}
		snapshot.Ingresses = append(snapshot.Ingresses, ref)
	}
}

// podSpecConfigRefs extracts ConfigMap/Secret references from a pod spec (envFrom,
// env.valueFrom, and volumes), used to build workload→config edges in the graph.
func podSpecConfigRefs(spec *corev1.PodSpec) []ConfigRef {
	var refs []ConfigRef
	seen := make(map[string]bool)
	add := func(kind, name string) {
		if name == "" {
			return
		}
		key := kind + "/" + name
		if seen[key] {
			return
		}
		seen[key] = true
		refs = append(refs, ConfigRef{Kind: kind, Name: name})
	}
	for _, ctr := range spec.Containers {
		for _, ef := range ctr.EnvFrom {
			if ef.ConfigMapRef != nil {
				add("ConfigMap", ef.ConfigMapRef.Name)
			}
			if ef.SecretRef != nil {
				add("Secret", ef.SecretRef.Name)
			}
		}
		for _, e := range ctr.Env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil {
				add("ConfigMap", e.ValueFrom.ConfigMapKeyRef.Name)
			}
			if e.ValueFrom.SecretKeyRef != nil {
				add("Secret", e.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	for _, v := range spec.Volumes {
		if v.ConfigMap != nil {
			add("ConfigMap", v.ConfigMap.Name)
		}
		if v.Secret != nil {
			add("Secret", v.Secret.SecretName)
		}
	}
	return refs
}

// usesIstioSidecar reports whether a pod template requests Istio sidecar injection.
func usesIstioSidecar(labels, annotations map[string]string) bool {
	if labels["sidecar.istio.io/inject"] == annotationTrue {
		return true
	}
	if annotations["sidecar.istio.io/inject"] == annotationTrue {
		return true
	}
	return false
}
