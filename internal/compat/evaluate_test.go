package compat

import (
	"testing"

	"github.com/miropshq/mirops/internal/collector"
)

func TestEvaluate(t *testing.T) {
	m := DefaultMatrix()

	tests := []struct {
		name       string
		addon      collector.DetectedAddon
		target     string
		wantStatus string
	}{
		{
			name:       "istio 1.21 compatible with 1.30",
			addon:      collector.DetectedAddon{Name: "istio", Version: "1.21.0"},
			target:     "1.30",
			wantStatus: StatusCompatible,
		},
		{
			name:       "istio 1.21 incompatible with 1.34",
			addon:      collector.DetectedAddon{Name: "istio", Version: "1.21.0"},
			target:     "1.34",
			wantStatus: StatusIncompatible,
		},
		{
			name:       "cert-manager 1.17 compatible with 1.34",
			addon:      collector.DetectedAddon{Name: "cert-manager", Version: "1.17.0"},
			target:     "1.34",
			wantStatus: StatusCompatible,
		},
		{
			name:       "unknown add-on has no data",
			addon:      collector.DetectedAddon{Name: "mystery-operator", Version: "1.0.0"},
			target:     "1.31",
			wantStatus: StatusUnknown,
		},
		{
			name:       "missing version is unknown",
			addon:      collector.DetectedAddon{Name: "istio", Version: ""},
			target:     "1.31",
			wantStatus: StatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, _ := Evaluate([]collector.DetectedAddon{tt.addon}, tt.target, m)
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			if results[0].Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (note: %q)", results[0].Status, tt.wantStatus, results[0].Note)
			}
		})
	}
}

// TestRequiredVersionInverseLookup verifies the Istio 1.20 → k8s 1.34 case: incompatible,
// and requiredVersion recommends the add-on version to upgrade to (>=1.24.0).
func TestRequiredVersionInverseLookup(t *testing.T) {
	m := DefaultMatrix()
	results, _ := Evaluate([]collector.DetectedAddon{{Name: "istio", Version: "1.20.0"}}, "1.34", m)

	r := results[0]
	if r.Status != StatusIncompatible {
		t.Fatalf("status = %q, want incompatible", r.Status)
	}
	if r.RequiredVersion != ">=1.24.0" {
		t.Errorf("requiredVersion = %q, want %q (upgrade target add-on version)", r.RequiredVersion, ">=1.24.0")
	}
}

func TestEvaluateCountsIncompatible(t *testing.T) {
	m := DefaultMatrix()
	addons := []collector.DetectedAddon{
		{Name: "istio", Version: "1.21.0"},        // incompatible with 1.34
		{Name: "cert-manager", Version: "1.17.0"}, // compatible with 1.34
	}
	_, incompatible := Evaluate(addons, "1.34", m)
	if incompatible != 1 {
		t.Errorf("incompatible count = %d, want 1", incompatible)
	}
}

func TestLoadMatrixOverride(t *testing.T) {
	override := []byte(`
addons:
  istio:
    - addonRange: ">=1.0.0"
      k8sRange: ">=1.0.0"
      note: "overridden"
`)
	m, err := LoadMatrix(override)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	results, _ := Evaluate([]collector.DetectedAddon{{Name: "istio", Version: "1.21.0"}}, "1.34", m)
	if results[0].Status != StatusCompatible {
		t.Errorf("override should make istio compatible, got %q", results[0].Status)
	}
	// Non-overridden add-on keeps built-in rules.
	if _, ok := m["cert-manager"]; !ok {
		t.Error("cert-manager rules should be preserved after override")
	}
}
