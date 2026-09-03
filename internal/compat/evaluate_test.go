package compat

import (
	"testing"

	"github.com/miropshq/mirops/internal/collector"
)

// testMatrix is a synthetic fixture (fake add-on names and versions) so the engine tests
// exercise Evaluate's logic — compatible / incompatible / unknown and the requiredVersion
// inverse lookup — without encoding any real compatibility. The real data lives only in
// matrix.yaml and is validated separately by TestEmbeddedMatrixValid.
var testMatrix = Matrix{
	"addon-a": {
		{AddonRange: ">=1.0.0 <2.0.0", K8sRange: ">=1.27.0 <=1.30.0"},
		{AddonRange: ">=2.0.0", K8sRange: ">=1.29.0"},
	},
	"addon-b": {
		{AddonRange: ">=1.0.0", K8sRange: ">=1.27.0"},
	},
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name       string
		addon      collector.DetectedAddon
		target     string
		wantStatus string
	}{
		{
			name:       "version in range is compatible",
			addon:      collector.DetectedAddon{Name: "addon-a", Version: "1.5.0"},
			target:     "1.30",
			wantStatus: StatusCompatible,
		},
		{
			name:       "version out of range is incompatible",
			addon:      collector.DetectedAddon{Name: "addon-a", Version: "1.5.0"},
			target:     "1.34",
			wantStatus: StatusIncompatible,
		},
		{
			name:       "add-on with no rules is unknown",
			addon:      collector.DetectedAddon{Name: "addon-z", Version: "1.0.0"},
			target:     "1.31",
			wantStatus: StatusUnknown,
		},
		{
			name:       "missing version is unknown",
			addon:      collector.DetectedAddon{Name: "addon-a", Version: ""},
			target:     "1.31",
			wantStatus: StatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, _ := Evaluate([]collector.DetectedAddon{tt.addon}, tt.target, testMatrix)
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			if results[0].Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (note: %q)", results[0].Status, tt.wantStatus, results[0].Note)
			}
		})
	}
}

// TestRequiredVersionInverseLookup: an incompatible add-on reports which version to upgrade to
// (the rule whose k8sRange covers the target). This is the highest-stakes output — the upgrade
// recommendation the user follows.
func TestRequiredVersionInverseLookup(t *testing.T) {
	results, _ := Evaluate([]collector.DetectedAddon{{Name: "addon-a", Version: "1.5.0"}}, "1.34", testMatrix)

	r := results[0]
	if r.Status != StatusIncompatible {
		t.Fatalf("status = %q, want incompatible", r.Status)
	}
	if r.RequiredVersion != ">=2.0.0" {
		t.Errorf("requiredVersion = %q, want %q", r.RequiredVersion, ">=2.0.0")
	}
}

func TestEvaluateCountsIncompatible(t *testing.T) {
	addons := []collector.DetectedAddon{
		{Name: "addon-a", Version: "1.5.0"}, // incompatible with 1.34
		{Name: "addon-b", Version: "1.5.0"}, // compatible with 1.34
	}
	_, incompatible := Evaluate(addons, "1.34", testMatrix)
	if incompatible != 1 {
		t.Errorf("incompatible count = %d, want 1", incompatible)
	}
}

// TestLoadMatrixOverride checks the override mechanism: a ConfigMap override adds/replaces an
// add-on's rules while built-in add-ons are preserved.
func TestLoadMatrixOverride(t *testing.T) {
	override := []byte(`
addons:
  override-addon:
    - addonRange: ">=0.0.0"
      k8sRange: ">=0.0.0"
`)
	m, err := LoadMatrix(override)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	if len(m["override-addon"]) != 1 {
		t.Errorf("override add-on should have 1 rule, got %d", len(m["override-addon"]))
	}
	// A built-in add-on keeps its rules after an unrelated override.
	if _, ok := m["istio"]; !ok {
		t.Error("built-in rules should be preserved after override")
	}
}
