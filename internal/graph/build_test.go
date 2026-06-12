package graph

import (
	"testing"

	"github.com/miropshq/mirops/internal/collector"
)

func TestBuildFromSnapshot(t *testing.T) {
	snap := &collector.ClusterSnapshot{
		DetectedAddons: []collector.DetectedAddon{
			{Name: "istio", Version: "1.21.0"},
			{Name: "cert-manager", Version: "1.17.0"},
		},
		NodeWorkloads: []collector.NodeWorkload{{Name: "node-1", Status: "Ready"}},
		Workloads: []collector.Workload{{
			Kind: "Deployment", Namespace: "shop", Name: "frontend", Status: "Healthy",
			PodLabels:        map[string]string{"app": "frontend"},
			ConfigRefs:       []collector.ConfigRef{{Kind: "ConfigMap", Name: "frontend-config"}},
			Nodes:            []string{"node-1"},
			UsesIstioSidecar: true,
		}},
		Services: []collector.ServiceRef{{
			Namespace: "shop", Name: "frontend-svc", Selector: map[string]string{"app": "frontend"},
		}},
		Ingresses: []collector.IngressRef{{
			Namespace: "shop", Name: "shop-ing", Services: []string{"frontend-svc"}, HasTLS: true,
		}},
	}

	g := BuildFromSnapshot(snap)

	// Nodes: 2 addons + 1 node + 1 deployment + 1 service + 1 ingress + 1 configmap = 7
	if len(g.Nodes) != 7 {
		t.Errorf("node count = %d, want 7", len(g.Nodes))
	}

	want := map[string]bool{
		"Ingress/shop/shop-ing -> Service/shop/frontend-svc (routes-to)":           false,
		"Service/shop/frontend-svc -> Deployment/shop/frontend (selects)":          false,
		"Deployment/shop/frontend -> ConfigMap/shop/frontend-config (uses-config)": false,
		"Deployment/shop/frontend -> Addon/istio (depends-on)":                     false,
		"Ingress/shop/shop-ing -> Addon/cert-manager (depends-on)":                 false,
	}
	for _, e := range g.Edges {
		key := e.From + " -> " + e.To + " (" + e.Type + ")"
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("missing expected edge: %s", key)
		}
	}
}
