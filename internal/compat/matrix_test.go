package compat

import "testing"

// TestEmbeddedMatrixValid guards the embedded matrix.yaml: it must parse, cover the expected
// add-ons, and have both ranges populated on every rule. A broken edit to matrix.yaml fails here.
func TestEmbeddedMatrixValid(t *testing.T) {
	m := DefaultMatrix()

	for _, addon := range []string{
		"istio", "cert-manager", "ingress-nginx", "argocd", "prometheus",
		"external-dns", "metrics-server", "cluster-autoscaler", "calico", "cilium",
	} {
		if len(m[addon]) == 0 {
			t.Errorf("embedded matrix.yaml has no rules for %q", addon)
		}
	}

	for name, rules := range m {
		for i, r := range rules {
			if r.AddonRange == "" || r.K8sRange == "" {
				t.Errorf("%s rule %d has an empty range: %+v", name, i, r)
			}
		}
	}
}
