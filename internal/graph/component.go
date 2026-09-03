// Package graph is the mirops Infrastructure Mirror + Dependency Graph engine. It turns a
// ClusterSnapshot into a logical graph of components (workloads, network objects, add-ons,
// infra) and the dependencies between them, then scores risk per component and namespace.
package graph

// ComponentType groups components for the UI and for risk aggregation.
type ComponentType string

const (
	TypeWorkload ComponentType = "workload" // Deployment, StatefulSet, DaemonSet, Job, CronJob
	TypeNetwork  ComponentType = "network"  // Service, Ingress
	TypeAddon    ComponentType = "addon"    // Istio, Cert Manager, ...
	TypeConfig   ComponentType = "config"   // ConfigMap, Secret
	TypeStorage  ComponentType = "storage"  // PersistentVolumeClaim
	TypeInfra    ComponentType = "infra"    // Node
)

// Edge relationship types.
const (
	EdgeRoutesTo    = "routes-to"    // Ingress -> Service
	EdgeSelects     = "selects"      // Service -> workload
	EdgeUsesConfig  = "uses-config"  // workload -> ConfigMap/Secret
	EdgeUsesStorage = "uses-storage" // workload -> PVC
	EdgeRunsOn      = "runs-on"      // workload -> Node
	EdgeDependsOn   = "depends-on"   // workload/ingress -> add-on
)

// Component is a logical node in the cluster mirror.
type Component struct {
	ID        string        `json:"id"` // stable unique id: "Kind/namespace/name"
	Kind      string        `json:"kind"`
	Name      string        `json:"name"`
	Namespace string        `json:"namespace,omitempty"`
	Type      ComponentType `json:"type"`
	Version   string        `json:"version,omitempty"`
	Status    string        `json:"status,omitempty"`
	Risk      int           `json:"risk"` // 0 (none) .. 100 (critical), filled by the risk engine
}

// Edge is a directed dependency between two components, by ID.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// Graph is the full mirror: components and the dependencies between them.
type Graph struct {
	Nodes []Component `json:"nodes"`
	Edges []Edge      `json:"edges"`
}

// id builds a stable component id. Cluster-scoped components pass an empty namespace.
func id(kind, namespace, name string) string {
	if namespace == "" {
		return kind + "/" + name
	}
	return kind + "/" + namespace + "/" + name
}
