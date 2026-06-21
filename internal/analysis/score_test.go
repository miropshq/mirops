package analysis

import (
	"strings"
	"testing"

	"github.com/miropshq/mirops/internal/compat"
	"github.com/miropshq/mirops/internal/graph"
)

// baseReport builds a SAFE-looking report (high score, no conditions) so each test can layer a
// single signal and assert that ApplyGraphDecision reacts to exactly that signal.
func baseReport() *Report {
	return &Report{
		Decision: Decision{Level: levelSafe, Allow: true},
		Scores:   Scores{Total: 95},
	}
}

func TestApplyGraphDecision_SafeStaysSafe(t *testing.T) {
	r := baseReport()
	ApplyGraphDecision(r)
	if r.Decision.Level != levelSafe || !r.Decision.Allow {
		t.Fatalf("clean report should stay SAFE/allow, got %s/%v", r.Decision.Level, r.Decision.Allow)
	}
	if len(r.Decision.Blockers) != 0 {
		t.Fatalf("expected no blockers, got %v", r.Decision.Blockers)
	}
}

func TestApplyGraphDecision_IncompatibleAddonBlocks(t *testing.T) {
	r := baseReport()
	r.Addons = []compat.AddonCompatibility{
		{Name: "istio", Version: "1.21.0", Status: "incompatible", RequiredVersion: ">=1.24.0"},
		{Name: "cert-manager", Version: "1.17.0", Status: "compatible"},
	}
	ApplyGraphDecision(r)
	if r.Decision.Level != levelCritical || r.Decision.Allow {
		t.Fatalf("incompatible add-on must force CRITICAL/deny, got %s/%v", r.Decision.Level, r.Decision.Allow)
	}
	if len(r.Decision.Blockers) != 1 || !strings.Contains(r.Decision.Blockers[0], "istio") {
		t.Fatalf("expected one istio blocker, got %v", r.Decision.Blockers)
	}
	if !strings.Contains(r.Decision.Blockers[0], ">=1.24.0") {
		t.Fatalf("blocker should name the required version, got %q", r.Decision.Blockers[0])
	}
}

func TestApplyGraphDecision_LostPVCBlocks(t *testing.T) {
	r := baseReport()
	r.Graph = &graph.Graph{Nodes: []graph.Component{
		{ID: "PVC/test4/data", Kind: "PVC", Name: "data", Namespace: "test4", Type: graph.TypeStorage, Status: "Lost"},
		{ID: "PVC/test1/data", Kind: "PVC", Name: "data", Namespace: "test1", Type: graph.TypeStorage, Status: "Bound"},
	}}
	ApplyGraphDecision(r)
	if r.Decision.Level != levelCritical || r.Decision.Allow {
		t.Fatalf("Lost PVC must force CRITICAL/deny, got %s/%v", r.Decision.Level, r.Decision.Allow)
	}
	if len(r.Decision.Blockers) != 1 || !strings.Contains(r.Decision.Blockers[0], "test4/data") {
		t.Fatalf("expected one test4/data blocker, got %v", r.Decision.Blockers)
	}
}

func TestApplyGraphDecision_MultipleBlockers(t *testing.T) {
	r := baseReport()
	r.Conditions.SeverelyUnstable = true
	r.Metrics.Pods.Total = 6
	r.Metrics.Pods.NotReady = 2
	r.Addons = []compat.AddonCompatibility{{Name: "istio", Version: "1.21.0", Status: "incompatible", RequiredVersion: ">=1.24.0"}}
	ApplyGraphDecision(r)
	if r.Decision.Level != levelCritical || r.Decision.Allow {
		t.Fatalf("expected CRITICAL/deny, got %s/%v", r.Decision.Level, r.Decision.Allow)
	}
	if len(r.Decision.Blockers) != 2 {
		t.Fatalf("expected 2 blockers (pods + add-on), got %v", r.Decision.Blockers)
	}
}

// A propagated risk that doesn't stem from a deterministic fact (an unknown add-on at risk 40,
// a degraded workload) must NOT block on its own — it only informs risk.byNamespace.
func TestApplyGraphDecision_PropagatedRiskAloneDoesNotBlock(t *testing.T) {
	r := baseReport()
	r.Graph = &graph.Graph{Nodes: []graph.Component{
		{ID: "Deployment/test5/app", Kind: "Deployment", Name: "app", Namespace: "test5", Type: graph.TypeWorkload, Status: "Down", Risk: 70},
	}}
	ApplyGraphDecision(r)
	if r.Decision.Level != levelSafe || !r.Decision.Allow {
		t.Fatalf("a non-deterministic risk should not block, got %s/%v", r.Decision.Level, r.Decision.Allow)
	}
}
