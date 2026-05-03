package collector

type ResourceRef struct {
	Namespace  string
	Name       string
	APIVersion string
	Kind       string
}

// PodIssue describes a pod that is not ready or has excessive restarts
type PodIssue struct {
	Namespace string
	Name      string
	Reason    string // e.g. "NotReady", "CrashLoopBackOff"
	Restarts  int
}

// NodeIssue describes a node with resource pressure
type NodeIssue struct {
	Name   string
	Reason string // e.g. "MemoryPressure", "DiskPressure", "CPUPressure"
}

// PDBIssue describes a PodDisruptionBudget that would block drain
type PDBIssue struct {
	Namespace string
	Name      string
}

type ClusterSnapshot struct {
	ClusterName    string
	ClusterVersion string
	NamespaceCount int
	Resources      []ResourceRef

	// Pod health
	TotalPods     int
	NotReadyPods  int
	TotalRestarts int
	PodIssues     []PodIssue

	// Capacity (CPU/Mem requests vs node capacity)
	CPURequests float64
	CPUCapacity float64
	MemRequests float64
	MemCapacity float64
	NodeIssues  []NodeIssue

	// Stability (compared to previous snapshot)
	PreviousTotalPods int
	PreviousRestarts  int

	// Risk
	DeprecatedAPIs int
	AddonIssues    int
	PDBBlocking    bool
	PDBIssues      []PDBIssue
}
