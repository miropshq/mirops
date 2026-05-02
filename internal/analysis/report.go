package analysis

// Report is the full JSON output written to the source destination
// and consumed by mirops-cli via --source flag.
type Report struct {
	GeneratedAt   string     `json:"generatedAt"`
	Cluster       string     `json:"cluster"`
	ClusterVersion string    `json:"clusterVersion"`
	TargetVersion string     `json:"targetVersion"`
	Scores        Scores     `json:"scores"`
	Metrics       Metrics    `json:"metrics"`
	Conditions    Conditions `json:"conditions"`
	Decision      Decision   `json:"decision"`
	Reason        string     `json:"reason"`
}

type Scores struct {
	Total    int `json:"total"`
	Health   int `json:"health"`
	Capacity int `json:"capacity"`
	Stability int `json:"stability"`
	Risk     int `json:"risk"`
}

type Metrics struct {
	Pods          PodMetrics          `json:"pods"`
	Resources     ResourceMetrics     `json:"resources"`
	Stability     StabilityMetrics    `json:"stability"`
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
	PDBBlocking         bool `json:"pdbBlocking"`
	HighCPUPressure     bool `json:"highCpuPressure"`
	HighMemoryPressure  bool `json:"highMemoryPressure"`
	UnstableCluster     bool `json:"unstableCluster"`
}

type Decision struct {
	Threshold int    `json:"threshold"`
	Allow     bool   `json:"allow"`
	Level     string `json:"level"`
}
