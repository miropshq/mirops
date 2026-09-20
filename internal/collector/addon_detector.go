package collector

import (
	"context"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// addonSignature describes how to recognize a cluster add-on.
type addonSignature struct {
	Name        string
	CRDGroups   []string // API groups that, if registered, indicate the add-on
	Deployments []string // deployment name substrings that indicate the add-on
	DaemonSets  []string // daemonset name substrings that indicate the add-on
}

// addonSignatures is the built-in catalogue of recognizable add-ons. It mirrors the add-ons
// covered by the compatibility matrix (mirops-compat) so every matrix rule can actually be
// exercised — the Name here must match the matrix key. Detection is best-effort: a workload
// match (Deployment or DaemonSet) yields a version and namespace; a CRD-group match only flags
// presence. Network/storage add-ons (calico, cilium, longhorn) run as DaemonSets, so those are
// scanned too — otherwise they would be detected without a version, or missed entirely.
var addonSignatures = []addonSignature{
	{Name: "istio", CRDGroups: []string{"networking.istio.io", "security.istio.io"}, Deployments: []string{"istiod"}},
	{Name: "cert-manager", CRDGroups: []string{"cert-manager.io"}, Deployments: []string{"cert-manager"}},
	{Name: "argocd", CRDGroups: []string{"argoproj.io"}, Deployments: []string{"argocd-server"}},
	{Name: "prometheus", CRDGroups: []string{"monitoring.coreos.com"}, Deployments: []string{"prometheus-operator"}},
	{Name: "ingress-nginx", Deployments: []string{"ingress-nginx-controller"}},
	{Name: "external-dns", Deployments: []string{"external-dns"}},
	{Name: "traefik", CRDGroups: []string{"traefik.io", "traefik.containo.us"}, Deployments: []string{"traefik"}},
	{Name: "kyverno", CRDGroups: []string{"kyverno.io"}, Deployments: []string{"kyverno"}},
	{Name: "gatekeeper", CRDGroups: []string{"templates.gatekeeper.sh", "constraints.gatekeeper.sh", "config.gatekeeper.sh"}, Deployments: []string{"gatekeeper-controller-manager"}},
	{Name: "keda", CRDGroups: []string{"keda.sh"}, Deployments: []string{"keda-operator"}},
	{Name: "metrics-server", Deployments: []string{"metrics-server"}},
	{Name: "cluster-autoscaler", Deployments: []string{"cluster-autoscaler"}},
	{Name: "karpenter", CRDGroups: []string{"karpenter.sh", "karpenter.k8s.aws"}, Deployments: []string{"karpenter"}},
	{Name: "aws-load-balancer-controller", CRDGroups: []string{"elbv2.k8s.aws"}, Deployments: []string{"aws-load-balancer-controller"}},
	{Name: "flux", CRDGroups: []string{"source.toolkit.fluxcd.io", "kustomize.toolkit.fluxcd.io", "helm.toolkit.fluxcd.io"}, Deployments: []string{"source-controller", "kustomize-controller", "helm-controller", "notification-controller"}},
	{Name: "calico", CRDGroups: []string{"crd.projectcalico.org", "operator.tigera.io"}, Deployments: []string{"calico-kube-controllers"}, DaemonSets: []string{"calico-node"}},
	{Name: "cilium", CRDGroups: []string{"cilium.io"}, Deployments: []string{"cilium-operator"}, DaemonSets: []string{"cilium"}},
	{Name: "longhorn", CRDGroups: []string{"longhorn.io"}, Deployments: []string{"longhorn-driver-deployer"}, DaemonSets: []string{"longhorn-manager"}},
}

// detectAddons populates snapshot.DetectedAddons by matching deployments, daemonsets and
// registered API groups against the known signatures. Add-ons in system namespaces are included
// on purpose (they matter for upgrade compatibility regardless of scope).
func (c *DefaultClusterCollector) detectAddons(ctx context.Context, snapshot *ClusterSnapshot) {
	// Registered API groups (best-effort; partial results are still useful).
	groupSet := make(map[string]bool)
	if groups, err := c.DiscoveryClient.ServerGroups(); err == nil {
		for _, g := range groups.Groups {
			groupSet[g.Name] = true
		}
	}

	// All deployments and daemonsets cluster-wide (not scope-filtered: add-ons often live in
	// system namespaces, and CNIs/storage run as DaemonSets rather than Deployments).
	depList := &appsv1.DeploymentList{}
	_ = c.Client.List(ctx, depList)
	dsList := &appsv1.DaemonSetList{}
	_ = c.Client.List(ctx, dsList)

	for _, sig := range addonSignatures {
		addon, found := matchAddon(sig, depList.Items, dsList.Items, groupSet)
		if found {
			snapshot.DetectedAddons = append(snapshot.DetectedAddons, addon)
		}
	}
}

// matchAddon checks a single signature against the cluster's deployments, daemonsets and API
// groups. A workload match is preferred because it also yields a version and namespace.
func matchAddon(sig addonSignature, deps []appsv1.Deployment, dsets []appsv1.DaemonSet, groupSet map[string]bool) (DetectedAddon, bool) {
	// Prefer a deployment match — it also gives us a version and namespace.
	for i := range deps {
		dep := &deps[i]
		for _, want := range sig.Deployments {
			if strings.Contains(dep.Name, want) {
				return DetectedAddon{
					Name:        sig.Name,
					Version:     workloadVersion(dep.Labels, dep.Spec.Template),
					Namespace:   dep.Namespace,
					DetectedVia: "deployment",
				}, true
			}
		}
	}
	// Then a daemonset match (CNIs, storage: calico, cilium, longhorn).
	for i := range dsets {
		ds := &dsets[i]
		for _, want := range sig.DaemonSets {
			if strings.Contains(ds.Name, want) {
				return DetectedAddon{
					Name:        sig.Name,
					Version:     workloadVersion(ds.Labels, ds.Spec.Template),
					Namespace:   ds.Namespace,
					DetectedVia: "daemonset",
				}, true
			}
		}
	}
	// Fall back to a registered CRD group (presence without a known version).
	for _, g := range sig.CRDGroups {
		if groupSet[g] {
			return DetectedAddon{Name: sig.Name, DetectedVia: "crd"}, true
		}
	}
	return DetectedAddon{}, false
}

// workloadVersion extracts a version from the app.kubernetes.io/version label (on the workload or
// its pod template), falling back to the image tag of the first container. It works for any
// workload with a pod template (Deployment, DaemonSet).
func workloadVersion(objLabels map[string]string, tmpl corev1.PodTemplateSpec) string {
	if v := objLabels["app.kubernetes.io/version"]; v != "" {
		return normalizeVersion(v)
	}
	if v := tmpl.Labels["app.kubernetes.io/version"]; v != "" {
		return normalizeVersion(v)
	}
	for _, ctr := range tmpl.Spec.Containers {
		if tag := imageTag(ctr.Image); tag != "" {
			return normalizeVersion(tag)
		}
	}
	return ""
}

// imageTag returns the tag portion of an image reference ("repo:tag@digest" -> "tag").
func imageTag(image string) string {
	// Drop digest if present.
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	// The tag is after the last ':' that is not part of a registry host:port.
	lastColon := strings.LastIndex(image, ":")
	lastSlash := strings.LastIndex(image, "/")
	if lastColon > lastSlash {
		return image[lastColon+1:]
	}
	return ""
}

// normalizeVersion strips a leading "v" so it parses as semver ("v1.21.0" -> "1.21.0").
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
