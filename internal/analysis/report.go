package analysis

import (
	"github.com/miropshq/mirops/internal/compat"
	"github.com/miropshq/mirops/internal/graph"
)

// Report is the full JSON output written to the source destination
// and consumed by mirops-cli via --source flag.
// Field order: identity → verdict → scores → conditions → metrics → workloads → issues (verbose last).
type Report struct {
	GeneratedAt    string          `json:"generatedAt"`
	Cluster        string          `json:"cluster"`
	ClusterVersion string          `json:"clusterVersion"`
	TargetVersion  string          `json:"targetVersion"`
	Decision       Decision        `json:"decision"`
	Reason         string          `json:"reason"`
	AIReasoning    string          `json:"aiReasoning,omitempty"`
	Scores         Scores          `json:"scores"`
	Conditions     Conditions      `json:"conditions"`
	Metrics        Metrics         `json:"metrics"`
	Workloads      ReportWorkloads `json:"workloads"`
	Issues         []string        `json:"issues,omitempty"`

	// Mirops engine output (logical mirror): add-on compatibility, dependency graph,
	// and per-namespace risk. Populated by the controller after the base analysis.
	Addons []compat.AddonCompatibility `json:"addons,omitempty"`
	Graph  *graph.Graph                `json:"graph,omitempty"`
	Risk   *RiskBreakdown              `json:"risk,omitempty"`
}

// RiskBreakdown holds the engine's per-namespace risk aggregation. Per-component risk
// lives on each node inside Graph.
type RiskBreakdown struct {
	ByNamespace []graph.NamespaceRisk `json:"byNamespace,omitempty"`
}

// ReportWorkloads groups cluster resources by kind for easy inspection.
type ReportWorkloads struct {
	Nodes          []NodeReport          `json:"nodes"`
	Deployments    []DeploymentReport    `json:"deployments"`
	StatefulSets   []StatefulSetReport   `json:"statefulsets"`
	DaemonSets     []DaemonSetReport     `json:"daemonsets"`
	Jobs           []JobReport           `json:"jobs"`
	PDBs           []PDBReport           `json:"pdbs,omitempty"`
	DeprecatedAPIs []DeprecatedAPIReport `json:"deprecatedApis,omitempty"`
}

type NodeReport struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Conditions []string `json:"conditions,omitempty"`
}

type WorkloadPodReport struct {
	Name     string `json:"name"`
	Reason   string `json:"reason,omitempty"`
	Restarts int    `json:"restarts"`
}

type DeploymentReport struct {
	Namespace       string              `json:"namespace"`
	Name            string              `json:"name"`
	ReadyReplicas   int32               `json:"readyReplicas"`
	DesiredReplicas int32               `json:"desiredReplicas"`
	Pods            []WorkloadPodReport `json:"pods,omitempty"`
}

type StatefulSetReport struct {
	Namespace       string              `json:"namespace"`
	Name            string              `json:"name"`
	ReadyReplicas   int32               `json:"readyReplicas"`
	DesiredReplicas int32               `json:"desiredReplicas"`
	Pods            []WorkloadPodReport `json:"pods,omitempty"`
}

type DaemonSetReport struct {
	Namespace         string              `json:"namespace"`
	Name              string              `json:"name"`
	NumberUnavailable int32               `json:"numberUnavailable"`
	Pods              []WorkloadPodReport `json:"pods,omitempty"`
}

type JobReport struct {
	Namespace string              `json:"namespace"`
	Name      string              `json:"name"`
	Active    int32               `json:"active"`
	Status    string              `json:"status"`
	Reason    string              `json:"reason,omitempty"`
	Pods      []WorkloadPodReport `json:"pods,omitempty"`
}

type PDBReport struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type DeprecatedAPIReport struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	RemovedIn string `json:"removedIn"`
}

type Scores struct {
	Total int        `json:"total"`
	Base  BaseScores `json:"base"`
	AI    AIScores   `json:"ai,omitempty"`
}

// BaseScores is the rule-based component of the total score.
type BaseScores struct {
	Score         int    `json:"score"`
	Weight        string `json:"weight"`
	Contribution  int    `json:"contribution"`
	Health        int    `json:"health"`
	Capacity      int    `json:"capacity"`
	Stability     int    `json:"stability"`
	Compatibility int    `json:"compatibility"`
}

// AIScores is the AI model component of the total score.
// Only present when ai.enabled is true.
type AIScores struct {
	Score        int    `json:"score"`
	Weight       string `json:"weight"`
	Contribution int    `json:"contribution"`
	Model        string `json:"model,omitempty"`
}

type Metrics struct {
	Pods          PodMetrics           `json:"pods"`
	Resources     ResourceMetrics      `json:"resources"`
	Stability     StabilityMetrics     `json:"stability"`
	Compatibility CompatibilityMetrics `json:"compatibility"`
}

type PodMetrics struct {
	Total      int `json:"total"`
	NotReady   int `json:"notReady"`
	Restarts   int `json:"restarts"`
	Restarting int `json:"restarting,omitempty"` // service pods crashing at an abnormal rate
}

type ResourceMetrics struct {
	CPUPressure    float64 `json:"cpuPressure"`
	MemoryPressure float64 `json:"memoryPressure"`
}

type StabilityMetrics struct {
	PodDropRatio float64 `json:"podDropRatio"` // fraction of pods lost since the last run (0 on growth)
	RestartDelta int     `json:"restartDelta"`
}

type CompatibilityMetrics struct {
	DeprecatedAPIs int `json:"deprecatedApis"`
	AddonIssues    int `json:"addonIssues"`
}

type Conditions struct {
	PDBBlocking        bool `json:"pdbBlocking"`
	HighCPUPressure    bool `json:"highCpuPressure"`
	HighMemoryPressure bool `json:"highMemoryPressure"`
	UnstableCluster    bool `json:"unstableCluster"`  // notReady > profile.UnstableWarnPct
	SeverelyUnstable   bool `json:"severelyUnstable"` // notReady > profile.UnstableBlockPct
}

type Decision struct {
	Allow bool   `json:"allow"`
	Level string `json:"level"`
	// Blockers lists every critical condition that forced level=CRITICAL (allow=false),
	// so consumers (mirops-cli --enforce, Headlamp) can show all reasons, not just one.
	Blockers []string `json:"blockers,omitempty"`
}
