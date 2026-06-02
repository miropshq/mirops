package analysis

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
	Total     int `json:"total"`
	Base      int `json:"base"`
	Health    int `json:"health"`
	Capacity  int `json:"capacity"`
	Stability int `json:"stability"`
	Risk      int `json:"risk"`
	AI        int `json:"ai,omitempty"`
}

type Metrics struct {
	Pods          PodMetrics           `json:"pods"`
	Resources     ResourceMetrics      `json:"resources"`
	Stability     StabilityMetrics     `json:"stability"`
	Compatibility CompatibilityMetrics `json:"compatibility"`
}

type PodMetrics struct {
	Total    int `json:"total"`
	NotReady int `json:"notReady"`
	Restarts int `json:"restarts"`
}

type ResourceMetrics struct {
	CPUPressure    float64 `json:"cpuPressure"`
	MemoryPressure float64 `json:"memoryPressure"`
}

type StabilityMetrics struct {
	PodDelta     float64 `json:"podDelta"`
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
	UnstableCluster    bool `json:"unstableCluster"`
}

type Decision struct {
	Threshold int    `json:"threshold"`
	Allow     bool   `json:"allow"`
	Level     string `json:"level"`
}
