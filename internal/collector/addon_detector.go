package collector

import (
	"context"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
)

// addonSignature describes how to recognize a cluster add-on.
type addonSignature struct {
	Name        string
	CRDGroups   []string // API groups that, if registered, indicate the add-on
	Deployments []string // deployment name substrings that indicate the add-on
}

// addonSignatures is the built-in catalogue of recognizable add-ons. Detection is
// best-effort: a deployment match yields a version, a CRD-group match flags presence.
var addonSignatures = []addonSignature{
	{Name: "istio", CRDGroups: []string{"networking.istio.io", "security.istio.io"}, Deployments: []string{"istiod"}},
	{Name: "cert-manager", CRDGroups: []string{"cert-manager.io"}, Deployments: []string{"cert-manager"}},
	{Name: "argocd", CRDGroups: []string{"argoproj.io"}, Deployments: []string{"argocd-server"}},
	{Name: "prometheus", CRDGroups: []string{"monitoring.coreos.com"}, Deployments: []string{"prometheus-operator"}},
	{Name: "ingress-nginx", Deployments: []string{"ingress-nginx-controller"}},
	{Name: "external-dns", Deployments: []string{"external-dns"}},
}

// detectAddons populates snapshot.DetectedAddons by matching deployments and registered
// API groups against the known signatures. Add-ons in system namespaces are included on
// purpose (they matter for upgrade compatibility regardless of scope).
func (c *DefaultClusterCollector) detectAddons(ctx context.Context, snapshot *ClusterSnapshot) {
	// Registered API groups (best-effort; partial results are still useful).
	groupSet := make(map[string]bool)
	if groups, err := c.DiscoveryClient.ServerGroups(); err == nil {
		for _, g := range groups.Groups {
			groupSet[g.Name] = true
		}
	}

	// All deployments cluster-wide (not scope-filtered: add-ons often live in system namespaces).
	depList := &appsv1.DeploymentList{}
	_ = c.Client.List(ctx, depList)

	for _, sig := range addonSignatures {
		addon, found := matchAddon(sig, depList.Items, groupSet)
		if found {
			snapshot.DetectedAddons = append(snapshot.DetectedAddons, addon)
		}
	}
}

// matchAddon checks a single signature against the cluster's deployments and API groups.
func matchAddon(sig addonSignature, deps []appsv1.Deployment, groupSet map[string]bool) (DetectedAddon, bool) {
	// Prefer a deployment match — it also gives us a version and namespace.
	for i := range deps {
		dep := &deps[i]
		for _, want := range sig.Deployments {
			if strings.Contains(dep.Name, want) {
				return DetectedAddon{
					Name:        sig.Name,
					Version:     addonVersion(dep),
					Namespace:   dep.Namespace,
					DetectedVia: "deployment",
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

// addonVersion extracts a version from the app.kubernetes.io/version label, falling back
// to the image tag of the first container.
func addonVersion(dep *appsv1.Deployment) string {
	if v := dep.Labels["app.kubernetes.io/version"]; v != "" {
		return normalizeVersion(v)
	}
	if v := dep.Spec.Template.Labels["app.kubernetes.io/version"]; v != "" {
		return normalizeVersion(v)
	}
	for _, ctr := range dep.Spec.Template.Spec.Containers {
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
