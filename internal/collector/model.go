package collector

type ResourceRef struct {
	Namespace  string
	Name       string
	APIVersion string
	Kind       string
}

type ClusterSnapshot struct {
	ClusterName    string
	ClusterVersion string
	NamespaceCount int
	Resources      []ResourceRef

	// Pod health
	TotalPods    int
	NotReadyPods int
	TotalRestarts int

	// Capacity (CPU/Mem requests vs node capacity)
	CPURequests  float64
	CPUCapacity  float64
	MemRequests  float64
	MemCapacity  float64

	// Stability (compared to previous snapshot)
	PreviousTotalPods int
	PreviousRestarts  int

	// Risk
	DeprecatedAPIs int
	AddonIssues    int
	PDBBlocking    bool
}
