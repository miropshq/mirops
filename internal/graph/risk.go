package graph

import "strings"

// riskDecay is how much of a dependency's risk a dependent inherits. A workload behind an
// incompatible add-on is risky, but somewhat less than the add-on itself.
const riskDecay = 0.6

// atRiskThreshold is the per-component risk at or above which a component is "at risk".
const atRiskThreshold = 50

// NamespaceRisk aggregates component risk for one namespace.
type NamespaceRisk struct {
	Namespace  string `json:"namespace"`
	Risk       int    `json:"risk"`       // highest component risk in the namespace
	Components int    `json:"components"` // components in the namespace
	AtRisk     int    `json:"atRisk"`     // components with risk >= atRiskThreshold
}

// ApplyRisk computes base risk per component, propagates it along dependency edges
// (a component inherits a decayed share of the risk of what it depends on), writes the
// effective risk back onto each node, and returns per-namespace aggregates.
//
// addonStatus maps an add-on name to its compatibility status (compatible | incompatible |
// unknown); incompatible add-ons seed high risk that flows to their dependents.
func (g *Graph) ApplyRisk(addonStatus map[string]string) []NamespaceRisk {
	base := make(map[string]int, len(g.Nodes))
	for _, n := range g.Nodes {
		base[n.ID] = baseRisk(n, addonStatus)
	}

	// Dependency adjacency: From depends on To.
	deps := make(map[string][]string)
	for _, e := range g.Edges {
		deps[e.From] = append(deps[e.From], e.To)
	}

	eff := make(map[string]int, len(g.Nodes))
	visiting := make(map[string]bool)
	var compute func(id string) int
	compute = func(id string) int {
		if v, ok := eff[id]; ok {
			return v
		}
		if visiting[id] {
			return base[id] // cycle guard
		}
		visiting[id] = true
		best := base[id]
		for _, d := range deps[id] {
			if inherited := int(float64(compute(d)) * riskDecay); inherited > best {
				best = inherited
			}
		}
		visiting[id] = false
		eff[id] = best
		return best
	}

	for i := range g.Nodes {
		g.Nodes[i].Risk = compute(g.Nodes[i].ID)
	}

	return aggregateByNamespace(g.Nodes)
}

// baseRisk assigns a component's intrinsic risk before propagation.
func baseRisk(c Component, addonStatus map[string]string) int {
	switch c.Type {
	case TypeAddon:
		switch addonStatus[c.Name] {
		case "incompatible":
			return 90
		case "unknown":
			return 40
		default:
			return 0
		}
	case TypeWorkload:
		switch {
		case c.Status == "Down":
			return 70
		case strings.HasPrefix(c.Status, "Degraded"):
			return 40
		case c.Status == "Scaled to zero":
			return 10
		default:
			return 0
		}
	case TypeInfra:
		if c.Status == "NotReady" {
			return 60
		}
		return 0
	default:
		return 0 // network, config: inherit only
	}
}

func aggregateByNamespace(nodes []Component) []NamespaceRisk {
	type acc struct {
		risk, count, atRisk int
	}
	byNS := make(map[string]*acc)
	order := []string{}
	for _, n := range nodes {
		ns := n.Namespace
		if ns == "" {
			ns = "cluster"
		}
		a, ok := byNS[ns]
		if !ok {
			a = &acc{}
			byNS[ns] = a
			order = append(order, ns)
		}
		a.count++
		if n.Risk > a.risk {
			a.risk = n.Risk
		}
		if n.Risk >= atRiskThreshold {
			a.atRisk++
		}
	}
	out := make([]NamespaceRisk, 0, len(order))
	for _, ns := range order {
		a := byNS[ns]
		out = append(out, NamespaceRisk{Namespace: ns, Risk: a.risk, Components: a.count, AtRisk: a.atRisk})
	}
	return out
}
