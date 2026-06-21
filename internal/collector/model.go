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

// Workload is a generic, fully-enumerated workload used to build the logical mirror.
// Unlike the *Issue structs (only populated on problems), every workload of every kind is
// captured here — healthy or not — so the dependency graph is a complete mirror.
type Workload struct {
	Kind             string // Deployment | StatefulSet | DaemonSet | Job | CronJob
	Namespace        string
	Name             string
	Status           string            // derived, kind-specific
	PodLabels        map[string]string // pod-template labels, for Service selector matching
	ConfigRefs       []ConfigRef       // ConfigMaps/Secrets consumed
	PVCs             []string          // PersistentVolumeClaim names consumed
	Nodes            []string          // nodes the workload's pods are scheduled on
	UsesIstioSidecar bool
}

// PVCRef represents a PersistentVolumeClaim for the dependency graph (stateful data at
// risk during a node drain).
type PVCRef struct {
	Namespace    string
	Name         string
	StorageClass string
	Phase        string // Bound | Pending | Lost
}

// DeploymentWorkload represents a deployment with its pod details
type DeploymentWorkload struct {
	Namespace        string            `json:"namespace"`
	Name             string            `json:"name"`
	ReadyReplicas    int32             `json:"readyReplicas"`
	DesiredReplicas  int32             `json:"desiredReplicas"`
	Pods             []WorkloadPod     `json:"pods,omitempty"`
	PodLabels        map[string]string `json:"-"` // pod-template labels, for Service selector matching
	ConfigRefs       []ConfigRef       `json:"-"` // ConfigMaps/Secrets consumed
	Nodes            []string          `json:"-"` // nodes the pods are scheduled on
	UsesIstioSidecar bool              `json:"-"` // pod template requests istio injection
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

// Job status values reported on JobIssue.Status.
const (
	JobStatusActive    = "Active"
	JobStatusFailed    = "Failed"
	JobStatusCompleted = "Completed"
	JobStatusPending   = "Pending"
)

// JobIssue describes an active Job that may be interrupted by the upgrade or a
// terminally failed Job that exhausted its retries.
type JobIssue struct {
	Namespace string
	Name      string
	Active    int32
	Status    string // Active | Failed
	Reason    string // Kubernetes Job condition reason, e.g. BackoffLimitExceeded
	Pods      []WorkloadPod
}

// DeprecatedAPI describes a resource using a deprecated API version
type DeprecatedAPI struct {
	Group     string
	Version   string
	Resource  string
	RemovedIn string // k8s version when it's removed
}

// ServiceRef represents a Service for the dependency graph.
type ServiceRef struct {
	Namespace string
	Name      string
	Type      string            // ClusterIP, NodePort, LoadBalancer, ExternalName
	Selector  map[string]string // matches pod labels of the backing workload
}

// IngressRef represents an Ingress and the Services it routes to.
type IngressRef struct {
	Namespace string
	Name      string
	Services  []string // backend service names
	HasTLS    bool     // signals a likely cert-manager dependency
}

// ConfigRef is a ConfigMap or Secret a workload consumes.
type ConfigRef struct {
	Kind string // "ConfigMap" | "Secret"
	Name string
}

// DetectedAddon is a recognized cluster add-on and the version found.
type DetectedAddon struct {
	Name        string // istio, cert-manager, argocd, prometheus, ingress-nginx, ...
	Version     string // from app.kubernetes.io/version label or image tag; "" if unknown
	Namespace   string
	DetectedVia string // "crd" | "deployment" | "namespace"
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

	// RestartingPods counts service pods (restartPolicy=Always) that are currently crashing
	// (CrashLoopBackOff / ImagePullBackOff / ErrImagePull) at an abnormal rate (>= 10
	// restarts/24h). Each such pod counts once; the health score uses the ratio
	// RestartingPods/TotalPods so it scales with cluster size.
	RestartingPods int

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

	// Mirror / dependency-graph inputs
	Services       []ServiceRef
	Ingresses      []IngressRef
	DetectedAddons []DetectedAddon
	Workloads      []Workload // all workloads, every kind, healthy or not
	PVCs           []PVCRef

	// Workload hierarchy (for report workloads section)
	NodeWorkloads       []NodeWorkload
	DeploymentWorkloads []DeploymentWorkload

	// Workload issues
	StatefulSetIssues []StatefulSetIssue
	DaemonSetIssues   []DaemonSetIssue
	JobIssues         []JobIssue
}
