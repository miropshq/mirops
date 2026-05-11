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
	Reason string // e.g. "MemoryPressure", "DiskPressure", "NotReady"
}

// PDBIssue describes a PodDisruptionBudget that would block drain
type PDBIssue struct {
	Namespace string
	Name      string
}

// WorkloadPod is a pod grouped under its parent workload in the report
type WorkloadPod struct {
	Name     string `json:"name"`
	Reason   string `json:"reason,omitempty"`
	Restarts int    `json:"restarts"`
}

// NodeWorkload represents a node with its readiness status
type NodeWorkload struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Conditions []string `json:"conditions,omitempty"`
}

// DeploymentWorkload represents a deployment with its pod details
type DeploymentWorkload struct {
	Namespace       string        `json:"namespace"`
	Name            string        `json:"name"`
	ReadyReplicas   int32         `json:"readyReplicas"`
	DesiredReplicas int32         `json:"desiredReplicas"`
	Pods            []WorkloadPod `json:"pods,omitempty"`
}

// StatefulSetIssue describes a StatefulSet that is not fully ready
type StatefulSetIssue struct {
	Namespace     string
	Name          string
	ReadyReplicas int32
	TotalReplicas int32
	Pods          []WorkloadPod
}

// DaemonSetIssue describes a DaemonSet that has unavailable pods
type DaemonSetIssue struct {
	Namespace         string
	Name              string
	NumberUnavailable int32
	Pods              []WorkloadPod
}

// JobIssue describes an active Job that may be interrupted by the upgrade
type JobIssue struct {
	Namespace string
	Name      string
	Active    int32
	Pods      []WorkloadPod
}

// DeprecatedAPI describes a resource using a deprecated API version
type DeprecatedAPI struct {
	Group     string
	Version   string
	Resource  string
	RemovedIn string // k8s version when it's removed
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
	DeprecatedAPIs    int
	DeprecatedAPIList []DeprecatedAPI
	AddonIssues       int
	PDBBlocking       bool
	PDBIssues         []PDBIssue

	// Workload hierarchy (for report workloads section)
	NodeWorkloads       []NodeWorkload
	DeploymentWorkloads []DeploymentWorkload

	// Workload issues
	StatefulSetIssues []StatefulSetIssue
	DaemonSetIssues   []DaemonSetIssue
	JobIssues         []JobIssue
}
