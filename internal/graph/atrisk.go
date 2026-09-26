package graph

import "sort"

// AtRiskComponent is a component at or above the at-risk threshold, with where its risk comes from
// and what depends on it — i.e. what breaks if it fails.
type AtRiskComponent struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status,omitempty"`
	Risk      int    `json:"risk"`
	// InheritedFrom is the dependency this component's risk comes from when its own state is fine
	// and it is at risk only because something it depends on is. Empty when the risk is its own.
	InheritedFrom string `json:"inheritedFrom,omitempty"`
	// Dependents are the components that depend on this one, directly or transitively.
	Dependents []string `json:"dependents,omitempty"`
}

// AtRisk lists the components with risk >= atRiskThreshold, highest risk first. Call it after
// ApplyRisk, with the same addonStatus, so a component's own risk can be told apart from risk it
// inherits along a dependency edge.
func (g *Graph) AtRisk(addonStatus map[string]string) []AtRiskComponent {
	deps := make(map[string][]string)
	risk := make(map[string]int, len(g.Nodes))
	for _, n := range g.Nodes {
		risk[n.ID] = n.Risk
	}
	for _, e := range g.Edges {
		deps[e.From] = append(deps[e.From], e.To)
	}
	rdeps := g.reverseEdges()

	out := make([]AtRiskComponent, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.Risk < atRiskThreshold {
			continue
		}
		c := AtRiskComponent{
			ID: n.ID, Kind: n.Kind, Name: n.Name, Namespace: n.Namespace,
			Status: n.Status, Risk: n.Risk,
			Dependents: dependentsOf(rdeps, n.ID),
		}
		if baseRisk(n, addonStatus) < n.Risk {
			c.InheritedFrom = riskiest(deps[n.ID], risk)
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Risk != out[j].Risk {
			return out[i].Risk > out[j].Risk
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Dependents returns every component that depends on id, directly or transitively, sorted.
func (g *Graph) Dependents(id string) []string {
	return dependentsOf(g.reverseEdges(), id)
}

// reverseEdges maps a component to the components that depend on it (the edge From depends on To).
func (g *Graph) reverseEdges() map[string][]string {
	rdeps := make(map[string][]string)
	for _, e := range g.Edges {
		rdeps[e.To] = append(rdeps[e.To], e.From)
	}
	return rdeps
}

// dependentsOf walks the reverse edges breadth-first from id; the visited set guards cycles.
func dependentsOf(rdeps map[string][]string, id string) []string {
	seen := map[string]bool{id: true}
	queue := []string{id}
	var out []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range rdeps[cur] {
			if seen[d] {
				continue
			}
			seen[d] = true
			out = append(out, d)
			queue = append(queue, d)
		}
	}
	sort.Strings(out)
	return out
}

// riskiest returns the id with the highest risk (ties broken by id, so the result is stable).
func riskiest(ids []string, risk map[string]int) string {
	best := ""
	for _, id := range ids {
		if best == "" || risk[id] > risk[best] || (risk[id] == risk[best] && id < best) {
			best = id
		}
	}
	return best
}
