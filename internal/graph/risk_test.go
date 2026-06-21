package graph

import (
	"testing"

	"github.com/miropshq/mirops/internal/collector"
)

// TestPVCBindingModeRisk verifies PVC phase scoring, and that a Pending PVC bound by a
// WaitForFirstConsumer StorageClass is treated as normal (risk 0), not as a stuck volume.
func TestPVCBindingModeRisk(t *testing.T) {
	snap := &collector.ClusterSnapshot{
		PVCs: []collector.PVCRef{
			{Namespace: "a", Name: "wfc-pending", Phase: "Pending", BindingMode: "WaitForFirstConsumer"},
			{Namespace: "a", Name: "immediate-pending", Phase: "Pending", BindingMode: "Immediate"},
			{Namespace: "a", Name: "lost", Phase: "Lost"},
			{Namespace: "a", Name: "bound", Phase: "Bound", BindingMode: "WaitForFirstConsumer"},
		},
	}
	g := BuildFromSnapshot(snap)
	g.ApplyRisk(nil)

	risk := make(map[string]int, len(g.Nodes))
	for _, n := range g.Nodes {
		risk[n.Name] = n.Risk
	}
	cases := map[string]int{"wfc-pending": 0, "immediate-pending": 50, "lost": 90, "bound": 0}
	for name, want := range cases {
		if risk[name] != want {
			t.Errorf("PVC %q risk = %d, want %d", name, risk[name], want)
		}
	}
}

// TestApplyRiskPropagation verifies that an incompatible add-on raises its own risk and
// propagates a decayed share to the workload/ingress that depend on it.
func TestApplyRiskPropagation(t *testing.T) {
	snap := &collector.ClusterSnapshot{
		DetectedAddons: []collector.DetectedAddon{{Name: "istio", Version: "1.21.0"}},
		Workloads: []collector.Workload{{
			Kind: "Deployment", Namespace: "shop", Name: "frontend", Status: "Healthy",
			PodLabels: map[string]string{"app": "frontend"}, UsesIstioSidecar: true,
		}},
	}
	g := BuildFromSnapshot(snap)

	nsRisk := g.ApplyRisk(map[string]string{"istio": "incompatible"})

	risk := map[string]int{}
	for _, n := range g.Nodes {
		risk[n.ID] = n.Risk
	}
	if risk["Addon/istio"] != 90 {
		t.Errorf("istio addon risk = %d, want 90", risk["Addon/istio"])
	}
	// frontend is Healthy (base 0) but depends on incompatible istio -> inherits 0.6*90 = 54.
	if got := risk["Deployment/shop/frontend"]; got != 54 {
		t.Errorf("frontend inherited risk = %d, want 54", got)
	}

	// Namespace aggregation: shop has the frontend (54, at risk).
	var shop *NamespaceRisk
	for i := range nsRisk {
		if nsRisk[i].Namespace == "shop" {
			shop = &nsRisk[i]
		}
	}
	if shop == nil {
		t.Fatal("missing shop namespace risk")
	}
	if shop.Risk != 54 || shop.AtRisk != 1 {
		t.Errorf("shop risk=%d atRisk=%d, want risk=54 atRisk=1", shop.Risk, shop.AtRisk)
	}
}

// TestApplyRiskCompatibleAddon: a compatible add-on contributes no risk.
func TestApplyRiskCompatibleAddon(t *testing.T) {
	snap := &collector.ClusterSnapshot{
		DetectedAddons: []collector.DetectedAddon{{Name: "istio", Version: "1.24.0"}},
		Workloads: []collector.Workload{{
			Kind: "Deployment", Namespace: "shop", Name: "frontend", Status: "Healthy",
			PodLabels: map[string]string{"app": "frontend"}, UsesIstioSidecar: true,
		}},
	}
	g := BuildFromSnapshot(snap)
	g.ApplyRisk(map[string]string{"istio": "compatible"})

	for _, n := range g.Nodes {
		if n.Risk != 0 {
			t.Errorf("%s risk = %d, want 0", n.ID, n.Risk)
		}
	}
}

func TestApplyRiskFailedJobAndPVCPhases(t *testing.T) {
	snap := &collector.ClusterSnapshot{
		Workloads: []collector.Workload{
			{Kind: "Job", Namespace: "batch", Name: "migration", Status: "Failed"},
			{Kind: "Deployment", Namespace: "shop", Name: "api", Status: "Healthy", PVCs: []string{"data"}},
		},
		PVCs: []collector.PVCRef{
			{Namespace: "shop", Name: "data", Phase: "Lost"},
			{Namespace: "shop", Name: "cache", Phase: "Pending"},
			{Namespace: "shop", Name: "healthy", Phase: "Bound"},
		},
	}

	g := BuildFromSnapshot(snap)
	g.ApplyRisk(nil)

	risk := make(map[string]int, len(g.Nodes))
	for _, n := range g.Nodes {
		risk[n.ID] = n.Risk
	}
	want := map[string]int{
		"Job/batch/migration": 70,
		"PVC/shop/data":       90,
		"PVC/shop/cache":      50,
		"PVC/shop/healthy":    0,
		"Deployment/shop/api": 54,
	}
	for id, expected := range want {
		if got := risk[id]; got != expected {
			t.Errorf("%s risk = %d, want %d", id, got, expected)
		}
	}
}
