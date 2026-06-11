package graph

import (
	"testing"

	"github.com/miropshq/mirops/internal/collector"
)

// TestApplyRiskPropagation verifies that an incompatible add-on raises its own risk and
// propagates a decayed share to the workload/ingress that depend on it.
func TestApplyRiskPropagation(t *testing.T) {
	snap := &collector.ClusterSnapshot{
		DetectedAddons: []collector.DetectedAddon{{Name: "istio", Version: "1.21.0"}},
		DeploymentWorkloads: []collector.DeploymentWorkload{{
			Namespace: "shop", Name: "frontend", ReadyReplicas: 2, DesiredReplicas: 2,
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
		DeploymentWorkloads: []collector.DeploymentWorkload{{
			Namespace: "shop", Name: "frontend", ReadyReplicas: 2, DesiredReplicas: 2,
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
