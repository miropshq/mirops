// Package compat is the mirops Compatibility Engine: it holds knowledge of which add-on
// versions are compatible with which Kubernetes versions and evaluates a cluster's
// detected add-ons against a target upgrade version.
package compat

import (
	_ "embed"
	"fmt"

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

//go:embed matrix.yaml
var embeddedMatrix []byte

// defaultMatrix is the built-in compatibility knowledge, loaded from the embedded matrix.yaml.
// Edit that file (by PR) to change the built-in rules; operators can also override per add-on at
// runtime via the mirops-compatibility-matrix ConfigMap (see LoadMatrix).
var defaultMatrix = mustLoadEmbedded()

// mustLoadEmbedded parses the embedded matrix.yaml at startup. A malformed file is a build-time
// mistake caught by tests, so panicking here (rather than returning an error) is appropriate.
func mustLoadEmbedded() Matrix {
	var doc struct {
		Addons Matrix `json:"addons"`
	}
	if err := yaml.Unmarshal(embeddedMatrix, &doc); err != nil {
		panic(fmt.Sprintf("compat: invalid embedded matrix.yaml: %v", err))
	}
	return doc.Addons
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
