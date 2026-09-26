package analysis

import (
	"github.com/miropshq/mirops/internal/collector"
	"github.com/miropshq/mirops/internal/graph"
)

// jobStatusFailed is a Job that exhausted its retries — a current-state problem, unlike a running
// Job, which only matters because a drain would interrupt it.
const jobStatusFailed = "Failed"

// MirrorReport is the ClusterMirror's published report: the cluster's current state (dependency
// graph, risk, live problems) plus whether this install runs upgrade analysis.
//
// It holds only what is true whatever version comes next; everything tied to a target version or to
// draining nodes (verdict, score, add-on compatibility, removed APIs, PDBs, jobs a drain would
// interrupt) stays in the UpgradeAnalysis report. It always exists while a mirror runs, so it is also
// where a consumer learns that upgrade analysis is disabled — there is no UpgradeAnalysis report to
// ask in that case.
type MirrorReport struct {
	Kind           string        `json:"kind"`
	GeneratedAt    string        `json:"generatedAt"`
	Mirror         string        `json:"mirror"`
	ClusterVersion string        `json:"clusterVersion"`
	Upgrade        MirrorUpgrade `json:"upgrade"`
	Summary        MirrorSummary `json:"summary"`
	Pods           PodMetrics    `json:"pods"`

	// AtRisk are the components at or above the at-risk threshold, with what depends on each one.
	AtRisk    []graph.AtRiskComponent `json:"atRisk,omitempty"`
	Risk      *RiskBreakdown          `json:"risk,omitempty"`
	Addons    []MirrorAddon           `json:"addons,omitempty"`
	Workloads ReportWorkloads         `json:"workloads"`
	Issues    []string                `json:"issues,omitempty"`
	Graph     *graph.Graph            `json:"graph,omitempty"`
}

// MirrorUpgrade says whether this install runs upgrade analysis (Helm upgrade.enabled) and, when it
// does, summarizes each UpgradeAnalysis so a consumer can pick one and fetch its full report.
type MirrorUpgrade struct {
	// Enabled is always written (no omitempty): false is exactly the signal a consumer needs.
	Enabled  bool                     `json:"enabled"`
	Analyses []UpgradeAnalysisSummary `json:"analyses,omitempty"`
	// Error is set when upgrade analysis is enabled but the analyses couldn't be listed, so an empty
	// Analyses list is never mistaken for "none created yet".
	Error string `json:"error,omitempty"`
}

// UpgradeAnalysisSummary is one UpgradeAnalysis as of the mirror's last rebuild (it can lag the
// analysis by up to one mirror refresh interval; LastAnalysisTime says how fresh the verdict is).
type UpgradeAnalysisSummary struct {
	Name          string `json:"name"`
	TargetVersion string `json:"targetVersion"`
	// Decision is SAFE, WARNING, CRITICAL or ERROR; empty while the first run is still in flight.
	Decision         string `json:"decision,omitempty"`
	Score            int    `json:"score"`
	LastAnalysisTime string `json:"lastAnalysisTime,omitempty"`
	// Report is the analysis report's name on the reports server: GET /reports/<report>.
	Report string `json:"report"`
}

// MirrorSummary is the mirror's headline numbers.
type MirrorSummary struct {
	Components       int `json:"components"`
	Edges            int `json:"edges"`
	AtRisk           int `json:"atRisk"`
	Namespaces       int `json:"namespaces"`
	NamespacesAtRisk int `json:"namespacesAtRisk"`
}

// MirrorAddon is an add-on detected in the cluster. The mirror only inventories it; whether it is
// compatible with a target version is the UpgradeAnalysis report's job.
type MirrorAddon struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Version   string `json:"version,omitempty"`
}

// BuildMirrorReport assembles the mirror report from the snapshot and the graph ApplyRisk(nil) has
// already scored (no target version, so add-ons carry no compatibility risk). The caller sets
// GeneratedAt, Mirror and Upgrade.
func BuildMirrorReport(snap *collector.ClusterSnapshot, g *graph.Graph, nsRisk []graph.NamespaceRisk) *MirrorReport {
	summary := MirrorSummary{Components: len(g.Nodes), Edges: len(g.Edges), Namespaces: len(nsRisk)}
	for _, nr := range nsRisk {
		summary.AtRisk += nr.AtRisk
		if nr.AtRisk > 0 {
			summary.NamespacesAtRisk++
		}
	}

	addons := make([]MirrorAddon, 0, len(snap.DetectedAddons))
	for _, a := range snap.DetectedAddons {
		addons = append(addons, MirrorAddon{Name: a.Name, Namespace: a.Namespace, Version: a.Version})
	}

	return &MirrorReport{
		Kind:           ReportKindClusterMirror,
		ClusterVersion: snap.ClusterVersion,
		Summary:        summary,
		Pods:           buildMetrics(snap).Pods,
		AtRisk:         g.AtRisk(nil),
		Risk:           &RiskBreakdown{ByNamespace: nsRisk},
		Addons:         addons,
		Workloads:      stateWorkloads(snap),
		Issues:         stateIssues(snap),
		Graph:          g,
	}
}

// stateWorkloads is the workload inventory minus what only matters for an upgrade: PDBs that would
// stall a drain, APIs removed in a target version, and running jobs a drain would interrupt (only
// failed jobs are a current-state problem).
func stateWorkloads(snap *collector.ClusterSnapshot) ReportWorkloads {
	w := buildWorkloads(snap)
	w.PDBs = nil
	w.DeprecatedAPIs = nil
	failed := make([]JobReport, 0, len(w.Jobs))
	for _, j := range w.Jobs {
		if j.Status == jobStatusFailed {
			failed = append(failed, j)
		}
	}
	w.Jobs = failed
	return w
}
