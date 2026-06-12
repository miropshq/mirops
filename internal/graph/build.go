package graph

import (
	"github.com/miropshq/mirops/internal/collector"
)

// BuildFromSnapshot is the Kubernetes adapter for the mirops engine: it projects a
// ClusterSnapshot into the generic component graph. The Component/Edge/Graph types are
// infrastructure-agnostic on purpose — other providers (VMs, cloud resources) can add
// their own builders later; today only the Kubernetes builder exists.
//
// The build runs in two passes: first all nodes, then all edges (so edges only ever link
// components that exist). Each step has a single responsibility.
func BuildFromSnapshot(snap *collector.ClusterSnapshot) *Graph {
	b := newBuilder()

	// 1. Nodes
	b.addAddons(snap)    // Addon/*
	b.addInfra(snap)     // Node/*
	b.addWorkloads(snap) // Deployment/*, StatefulSet/*, DaemonSet/*, Job/*, CronJob/*
	b.addNetwork(snap)   // Service/*, Ingress/*
	b.addStorage(snap)   // PVC/*

	// 2. Edges
	b.linkIngresses(snap) // Ingress -> Service, Ingress(TLS) -> cert-manager
	b.linkServices(snap)  // Service -> workload (selector match)
	b.linkWorkloads()     // workload -> config/storage/node/istio

	return &Graph{Nodes: b.nodes, Edges: b.edges}
}

// workloadRef holds the per-workload data needed to link edges in the second pass.
type workloadRef struct {
	id        string
	namespace string
	labels    map[string]string
	refs      []collector.ConfigRef
	pvcs      []string
	nodes     []string
	istio     bool
}

// builder accumulates the graph while it is being constructed.
type builder struct {
	nodes     []Component
	edges     []Edge
	exists    map[string]bool   // node id -> present
	addonID   map[string]string // add-on name -> node id
	workloads []workloadRef     // populated by addWorkloads, consumed by link*
}

func newBuilder() *builder {
	return &builder{
		exists:  make(map[string]bool),
		addonID: make(map[string]string),
	}
}

func (b *builder) addNode(c Component) {
	if b.exists[c.ID] {
		return
	}
	b.exists[c.ID] = true
	b.nodes = append(b.nodes, c)
}

// addEdge links two components only if both endpoints exist (no dangling edges).
func (b *builder) addEdge(from, to, typ string) {
	if !b.exists[from] || !b.exists[to] {
		return
	}
	b.edges = append(b.edges, Edge{From: from, To: to, Type: typ})
}

// --- Node passes -----------------------------------------------------------

func (b *builder) addAddons(snap *collector.ClusterSnapshot) {
	for _, a := range snap.DetectedAddons {
		nid := id("Addon", "", a.Name)
		b.addonID[a.Name] = nid
		b.addNode(Component{ID: nid, Kind: "Addon", Name: a.Name, Type: TypeAddon, Version: a.Version})
	}
}

func (b *builder) addInfra(snap *collector.ClusterSnapshot) {
	for _, n := range snap.NodeWorkloads {
		b.addNode(Component{ID: id("Node", "", n.Name), Kind: "Node", Name: n.Name, Type: TypeInfra, Status: n.Status})
	}
}

func (b *builder) addWorkloads(snap *collector.ClusterSnapshot) {
	for _, w := range snap.Workloads {
		nid := id(w.Kind, w.Namespace, w.Name)
		b.addNode(Component{
			ID: nid, Kind: w.Kind, Name: w.Name, Namespace: w.Namespace,
			Type: TypeWorkload, Status: w.Status,
		})
		b.workloads = append(b.workloads, workloadRef{
			id: nid, namespace: w.Namespace, labels: w.PodLabels,
			refs: w.ConfigRefs, pvcs: w.PVCs, nodes: w.Nodes, istio: w.UsesIstioSidecar,
		})
	}
}

func (b *builder) addNetwork(snap *collector.ClusterSnapshot) {
	for _, s := range snap.Services {
		b.addNode(Component{ID: id("Service", s.Namespace, s.Name), Kind: "Service", Name: s.Name, Namespace: s.Namespace, Type: TypeNetwork})
	}
	for _, ing := range snap.Ingresses {
		b.addNode(Component{ID: id("Ingress", ing.Namespace, ing.Name), Kind: "Ingress", Name: ing.Name, Namespace: ing.Namespace, Type: TypeNetwork})
	}
}

func (b *builder) addStorage(snap *collector.ClusterSnapshot) {
	for _, p := range snap.PVCs {
		b.addNode(Component{ID: id("PVC", p.Namespace, p.Name), Kind: "PVC", Name: p.Name, Namespace: p.Namespace, Type: TypeStorage, Status: p.Phase})
	}
}

// --- Edge passes -----------------------------------------------------------

func (b *builder) linkIngresses(snap *collector.ClusterSnapshot) {
	for _, ing := range snap.Ingresses {
		ingID := id("Ingress", ing.Namespace, ing.Name)
		for _, svc := range ing.Services {
			b.addEdge(ingID, id("Service", ing.Namespace, svc), EdgeRoutesTo)
		}
		if ing.HasTLS {
			if cm, ok := b.addonID["cert-manager"]; ok {
				b.addEdge(ingID, cm, EdgeDependsOn)
			}
		}
	}
}

func (b *builder) linkServices(snap *collector.ClusterSnapshot) {
	for _, s := range snap.Services {
		if len(s.Selector) == 0 {
			continue
		}
		svcID := id("Service", s.Namespace, s.Name)
		for _, w := range b.workloads {
			if w.namespace == s.Namespace && labelsMatch(s.Selector, w.labels) {
				b.addEdge(svcID, w.id, EdgeSelects)
			}
		}
	}
}

func (b *builder) linkWorkloads() {
	for _, w := range b.workloads {
		for _, ref := range w.refs {
			cid := id(ref.Kind, w.namespace, ref.Name)
			b.addNode(Component{ID: cid, Kind: ref.Kind, Name: ref.Name, Namespace: w.namespace, Type: TypeConfig})
			b.addEdge(w.id, cid, EdgeUsesConfig)
		}
		for _, claim := range w.pvcs {
			b.addEdge(w.id, id("PVC", w.namespace, claim), EdgeUsesStorage)
		}
		for _, node := range w.nodes {
			b.addEdge(w.id, id("Node", "", node), EdgeRunsOn)
		}
		if w.istio {
			if is, ok := b.addonID["istio"]; ok {
				b.addEdge(w.id, is, EdgeDependsOn)
			}
		}
	}
}

// --- Helpers ---------------------------------------------------------------

// labelsMatch reports whether every selector key/value is present in labels.
func labelsMatch(selector, labels map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}
