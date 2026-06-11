// Package compat is the mirops Compatibility Engine: it holds knowledge of which add-on
// versions are compatible with which Kubernetes versions and evaluates a cluster's
// detected add-ons against a target upgrade version.
package compat

import (
	"sigs.k8s.io/yaml"
)

// CompatRule says: an add-on whose version satisfies AddonRange is supported on the
// Kubernetes versions described by K8sRange. Ranges use blang/semver syntax,
// e.g. ">=1.20.0 <1.22.0" or ">=1.29.0".
type CompatRule struct {
	AddonRange string `json:"addonRange"`
	K8sRange   string `json:"k8sRange"`
	Note       string `json:"note,omitempty"`
}

// Matrix maps an add-on name to its compatibility rules.
type Matrix map[string][]CompatRule

// AddonCompatibility is the per-add-on evaluation result. It is also embedded in the
// report JSON so the UI can render a compatibility table.
type AddonCompatibility struct {
	Name            string `json:"name"`
	Version         string `json:"version,omitempty"`
	Status          string `json:"status"` // compatible | incompatible | unknown
	RequiredVersion string `json:"requiredVersion,omitempty"`
	Note            string `json:"note,omitempty"`
}

// Compatibility status values.
const (
	StatusCompatible   = "compatible"
	StatusIncompatible = "incompatible"
	StatusUnknown      = "unknown"
)

// defaultMatrix is the built-in compatibility knowledge, used when no ConfigMap override
// is present. Values are illustrative starting points — operators can override per cluster.
var defaultMatrix = Matrix{
	"istio": {
		{AddonRange: ">=1.20.0 <1.22.0", K8sRange: ">=1.27.0 <=1.30.0"},
		{AddonRange: ">=1.22.0 <1.24.0", K8sRange: ">=1.28.0 <=1.31.0"},
		{AddonRange: ">=1.24.0", K8sRange: ">=1.29.0"},
	},
	"cert-manager": {
		{AddonRange: ">=1.13.0 <1.15.0", K8sRange: ">=1.25.0 <=1.30.0"},
		{AddonRange: ">=1.15.0", K8sRange: ">=1.27.0"},
	},
	"argocd": {
		{AddonRange: ">=2.9.0 <2.11.0", K8sRange: ">=1.25.0 <=1.29.0"},
		{AddonRange: ">=2.11.0", K8sRange: ">=1.26.0"},
	},
	"ingress-nginx": {
		{AddonRange: ">=1.10.0", K8sRange: ">=1.27.0 <=1.31.0"},
	},
	"prometheus": {
		{AddonRange: ">=0.70.0", K8sRange: ">=1.25.0"},
	},
}

// DefaultMatrix returns a copy of the built-in matrix.
func DefaultMatrix() Matrix {
	return cloneMatrix(defaultMatrix)
}

// LoadMatrix returns the built-in matrix overlaid with an optional YAML override (the
// content of the compatibility-matrix ConfigMap). Per add-on, override rules fully replace
// the built-in rules. Empty input returns the built-in matrix unchanged.
//
// Expected YAML shape:
//
//	addons:
//	  istio:
//	    - addonRange: ">=1.24.0"
//	      k8sRange: ">=1.29.0"
func LoadMatrix(yamlData []byte) (Matrix, error) {
	m := cloneMatrix(defaultMatrix)
	if len(yamlData) == 0 {
		return m, nil
	}
	var override struct {
		Addons map[string][]CompatRule `json:"addons"`
	}
	if err := yaml.Unmarshal(yamlData, &override); err != nil {
		return m, err
	}
	for name, rules := range override.Addons {
		m[name] = rules
	}
	return m, nil
}

func cloneMatrix(src Matrix) Matrix {
	out := make(Matrix, len(src))
	for name, rules := range src {
		cp := make([]CompatRule, len(rules))
		copy(cp, rules)
		out[name] = cp
	}
	return out
}
