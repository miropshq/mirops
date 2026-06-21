package collector

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	discovery "k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DefaultClusterCollector struct {
	Client             client.Client
	DiscoveryClient    discovery.DiscoveryInterface
	WorkloadCollectors []WorkloadCollector
}

func NewClusterCollector(c client.Client, dc discovery.DiscoveryInterface) ClusterCollector {
	return &DefaultClusterCollector{
		Client:          c,
		DiscoveryClient: dc,
		WorkloadCollectors: []WorkloadCollector{
			NewDeploymentCollector(c),
		},
	}
}

func (c *DefaultClusterCollector) Collect(ctx context.Context, scope Scope) (*ClusterSnapshot, error) {
	snapshot := &ClusterSnapshot{}

	version, err := c.DiscoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}
	snapshot.ClusterVersion = version.GitVersion

	nsList := &corev1.NamespaceList{}
	if err := c.Client.List(ctx, nsList); err != nil {
		return nil, err
	}
	snapshot.NamespaceCount = len(nsList.Items)

	// Build exclusion set
	excluded := make(map[string]bool)
	for _, ns := range scope.ExcludeNamespaces {
		excluded[ns] = true
	}
	if scope.Mode == "application" {
		for ns := range systemNamespaces {
			excluded[ns] = true
		}
	}

	// Build ReplicaSet → Deployment map to resolve pod ownership
	rsToDeploy, err := c.buildRSToDeployMap(ctx)
	if err != nil {
		return nil, err
	}

	// Maps for grouping not-ready pods by parent workload key ("namespace/name")
	podsByDeployment := make(map[string][]WorkloadPod)
	podsByStatefulSet := make(map[string][]WorkloadPod)
	podsByDaemonSet := make(map[string][]WorkloadPod)
	podsByJob := make(map[string][]WorkloadPod)

	// nodesByWorkload maps a logical workload key ("Kind/namespace/name") to the set of
	// nodes its pods run on, for workload→Node (runs-on) edges in the mirror.
	nodesByWorkload := make(map[string]map[string]bool)

	// Collect pods across all (non-excluded) namespaces
	podList := &corev1.PodList{}
	if err := c.Client.List(ctx, podList); err != nil {
		return nil, err
	}
	for _, pod := range podList.Items {
		if excluded[pod.Namespace] {
			continue
		}
		snapshot.TotalPods++
		restarts := 0
		for _, cs := range pod.Status.ContainerStatuses {
			restarts += int(cs.RestartCount)
		}
		snapshot.TotalRestarts += restarts
		for _, c := range pod.Spec.Containers {
			if cpu, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
				snapshot.CPURequests += float64(cpu.MilliValue())
			}
			if mem, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
				snapshot.MemRequests += float64(mem.Value())
			}
		}

		// Count currently-crashing service pods that restart at an abnormal rate.
		if isRestartingAbnormally(&pod, time.Now()) {
			snapshot.RestartingPods++
		}

		// Record which node this pod runs on, attributed to its logical workload.
		recordPodNode(&pod, rsToDeploy, nodesByWorkload)

		if !isPodReady(&pod) {
			snapshot.NotReadyPods++
			reason := podNotReadyReason(&pod)
			snapshot.PodIssues = append(snapshot.PodIssues, PodIssue{
				Namespace: pod.Namespace,
				Name:      pod.Name,
				Reason:    reason,
				Restarts:  restarts,
			})
			wp := WorkloadPod{Name: pod.Name, Reason: reason, Restarts: restarts}
			groupNotReadyPod(&pod, wp, rsToDeploy, podsByDeployment, podsByStatefulSet, podsByDaemonSet, podsByJob)
		}
	}

	for _, wc := range c.WorkloadCollectors {
		resources, err := wc.Collect(ctx)
		if err != nil {
			return nil, err
		}
		snapshot.Resources = append(snapshot.Resources, resources...)
	}

	// Collect all deployments with pod details
	if err := c.collectDeployments(ctx, excluded, snapshot, podsByDeployment); err != nil {
		return nil, err
	}

	// Collect nodes — check for pressure/NotReady conditions
	if err := c.collectNodes(ctx, snapshot); err != nil {
		return nil, err
	}

	// Collect PodDisruptionBudgets
	if err := c.collectPDBs(ctx, excluded, snapshot); err != nil {
		return nil, err
	}

	// Collect StatefulSets
	if err := c.collectStatefulSets(ctx, excluded, snapshot, podsByStatefulSet); err != nil {
		return nil, err
	}

	// Collect DaemonSets
	if err := c.collectDaemonSets(ctx, excluded, snapshot, podsByDaemonSet); err != nil {
		return nil, err
	}

	// Collect active and terminally failed Jobs
	if err := c.collectJobs(ctx, excluded, snapshot, podsByJob); err != nil {
		return nil, err
	}

	// Collect Services and Ingresses (dependency-graph inputs)
	if err := c.collectServices(ctx, excluded, snapshot); err != nil {
		return nil, err
	}
	c.collectIngresses(ctx, excluded, snapshot)

	// Full workload inventory + PVCs for the logical mirror
	if err := c.collectWorkloads(ctx, excluded, snapshot, nodesByWorkload); err != nil {
		return nil, err
	}
	c.collectPVCs(ctx, excluded, snapshot)

	// Detect deprecated API usage
	c.detectDeprecatedAPIs(snapshot)

	// Detect installed add-ons (Istio, Cert Manager, ArgoCD, ...) for the compatibility engine
	c.detectAddons(ctx, snapshot)

	return snapshot, nil
}

// buildRSToDeployMap builds a map of "namespace/replicaset-name" → deployment-name.
func (c *DefaultClusterCollector) buildRSToDeployMap(ctx context.Context) (map[string]string, error) {
	rsList := &appsv1.ReplicaSetList{}
	if err := c.Client.List(ctx, rsList); err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rsList.Items))
	for _, rs := range rsList.Items {
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" {
				m[rs.Namespace+"/"+rs.Name] = ref.Name
				break
			}
		}
	}
	return m, nil
}

// collectDeployments collects all deployments and attaches not-ready pods.
func (c *DefaultClusterCollector) collectDeployments(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot, podsByDeployment map[string][]WorkloadPod) error {
	depList := &appsv1.DeploymentList{}
	if err := c.Client.List(ctx, depList); err != nil {
		return err
	}
	for _, dep := range depList.Items {
		if excluded[dep.Namespace] {
			continue
		}
		desired := int32(1)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}
		key := dep.Namespace + "/" + dep.Name
		snapshot.DeploymentWorkloads = append(snapshot.DeploymentWorkloads, DeploymentWorkload{
			Namespace:        dep.Namespace,
			Name:             dep.Name,
			ReadyReplicas:    dep.Status.ReadyReplicas,
			DesiredReplicas:  desired,
			Pods:             podsByDeployment[key],
			PodLabels:        dep.Spec.Template.Labels,
			ConfigRefs:       podSpecConfigRefs(&dep.Spec.Template.Spec),
			UsesIstioSidecar: usesIstioSidecar(dep.Spec.Template.Labels, dep.Spec.Template.Annotations),
		})
	}
	return nil
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func podNotReadyReason(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		// Waiting state: CrashLoopBackOff, ImagePullBackOff, etc.
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
		// Between restarts: container is Terminated but will restart (CrashLoopBackOff)
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			if cs.RestartCount > 0 {
				return "CrashLoopBackOff"
			}
			return cs.State.Terminated.Reason
		}
	}
	if pod.Status.Phase == corev1.PodRunning {
		return "ReadinessProbeFailed"
	}
	if pod.Status.Phase != "" {
		return string(pod.Status.Phase)
	}
	return "NotReady"
}

// collectNodes checks each node for pressure conditions and NotReady status.
func (c *DefaultClusterCollector) collectNodes(ctx context.Context, snapshot *ClusterSnapshot) error {
	nodeList := &corev1.NodeList{}
	if err := c.Client.List(ctx, nodeList); err != nil {
		return err
	}
	for _, node := range nodeList.Items {
		if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
			snapshot.CPUCapacity += float64(cpu.MilliValue())
		}
		if mem, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
			snapshot.MemCapacity += float64(mem.Value())
		}
		status := "Ready"
		var conditions []string
		for _, cond := range node.Status.Conditions {
			if cond.Status != corev1.ConditionTrue {
				continue
			}
			switch cond.Type {
			case corev1.NodeMemoryPressure:
				conditions = append(conditions, "MemoryPressure")
				snapshot.NodeIssues = append(snapshot.NodeIssues, NodeIssue{Name: node.Name, Reason: "MemoryPressure"})
			case corev1.NodeDiskPressure:
				conditions = append(conditions, "DiskPressure")
				snapshot.NodeIssues = append(snapshot.NodeIssues, NodeIssue{Name: node.Name, Reason: "DiskPressure"})
			case corev1.NodePIDPressure:
				conditions = append(conditions, "PIDPressure")
				snapshot.NodeIssues = append(snapshot.NodeIssues, NodeIssue{Name: node.Name, Reason: "PIDPressure"})
			}
		}
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
				status = "NotReady"
				conditions = append(conditions, "NotReady")
				snapshot.NodeIssues = append(snapshot.NodeIssues, NodeIssue{Name: node.Name, Reason: "NotReady"})
			}
		}
		snapshot.NodeWorkloads = append(snapshot.NodeWorkloads, NodeWorkload{
			Name:       node.Name,
			Status:     status,
			Conditions: conditions,
		})
	}
	return nil
}

// collectPDBs detects PodDisruptionBudgets with disruptions allowed == 0.
func (c *DefaultClusterCollector) collectPDBs(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot) error {
	pdbList := &policyv1.PodDisruptionBudgetList{}
	if err := c.Client.List(ctx, pdbList); err != nil {
		return err
	}
	for _, pdb := range pdbList.Items {
		if excluded[pdb.Namespace] {
			continue
		}
		if pdb.Status.DisruptionsAllowed == 0 {
			snapshot.PDBBlocking = true
			snapshot.PDBIssues = append(snapshot.PDBIssues, PDBIssue{
				Namespace: pdb.Namespace,
				Name:      pdb.Name,
			})
		}
	}
	return nil
}

// collectStatefulSets detects StatefulSets that are not fully ready.
func (c *DefaultClusterCollector) collectStatefulSets(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot, podsByStatefulSet map[string][]WorkloadPod) error {
	ssList := &appsv1.StatefulSetList{}
	if err := c.Client.List(ctx, ssList); err != nil {
		return err
	}
	for _, ss := range ssList.Items {
		if excluded[ss.Namespace] {
			continue
		}
		snapshot.Resources = append(snapshot.Resources, ResourceRef{
			Namespace: ss.Namespace, Name: ss.Name, APIVersion: "apps/v1", Kind: "StatefulSet",
		})
		desired := int32(1)
		if ss.Spec.Replicas != nil {
			desired = *ss.Spec.Replicas
		}
		if ss.Status.ReadyReplicas < desired {
			key := ss.Namespace + "/" + ss.Name
			snapshot.StatefulSetIssues = append(snapshot.StatefulSetIssues, StatefulSetIssue{
				Namespace:     ss.Namespace,
				Name:          ss.Name,
				ReadyReplicas: ss.Status.ReadyReplicas,
				TotalReplicas: desired,
				Pods:          podsByStatefulSet[key],
			})
		}
	}
	return nil
}

// collectDaemonSets detects DaemonSets with unavailable pods.
func (c *DefaultClusterCollector) collectDaemonSets(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot, podsByDaemonSet map[string][]WorkloadPod) error {
	dsList := &appsv1.DaemonSetList{}
	if err := c.Client.List(ctx, dsList); err != nil {
		return err
	}
	for _, ds := range dsList.Items {
		if excluded[ds.Namespace] {
			continue
		}
		snapshot.Resources = append(snapshot.Resources, ResourceRef{
			Namespace: ds.Namespace, Name: ds.Name, APIVersion: "apps/v1", Kind: "DaemonSet",
		})
		if ds.Status.NumberUnavailable > 0 {
			key := ds.Namespace + "/" + ds.Name
			snapshot.DaemonSetIssues = append(snapshot.DaemonSetIssues, DaemonSetIssue{
				Namespace:         ds.Namespace,
				Name:              ds.Name,
				NumberUnavailable: ds.Status.NumberUnavailable,
				Pods:              podsByDaemonSet[key],
			})
		}
	}
	return nil
}

// collectJobs detects active Jobs that may be interrupted during the upgrade and Jobs
// whose Failed condition confirms Kubernetes has stopped retrying them.
func (c *DefaultClusterCollector) collectJobs(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot, podsByJob map[string][]WorkloadPod) error {
	jobList := &batchv1.JobList{}
	if err := c.Client.List(ctx, jobList); err != nil {
		return err
	}
	for _, job := range jobList.Items {
		if excluded[job.Namespace] {
			continue
		}
		failed, reason := jobFailedReason(&job)
		if job.Status.Active > 0 || failed {
			key := job.Namespace + "/" + job.Name
			status := JobStatusActive
			if failed {
				status = JobStatusFailed
			}
			snapshot.JobIssues = append(snapshot.JobIssues, JobIssue{
				Namespace: job.Namespace,
				Name:      job.Name,
				Active:    job.Status.Active,
				Status:    status,
				Reason:    reason,
				Pods:      podsByJob[key],
			})
		}
	}
	return nil
}

// deprecatedAPIRemovedIn maps group/version combinations to the k8s version
// that removes them. Extend as needed for future deprecations.
var deprecatedAPIRemovedIn = map[string]string{
	"extensions/v1beta1/ingresses":                     "1.22",
	"networking.k8s.io/v1beta1/ingresses":              "1.22",
	"extensions/v1beta1/networkpolicies":               "1.16",
	"extensions/v1beta1/deployments":                   "1.16",
	"extensions/v1beta1/replicasets":                   "1.16",
	"extensions/v1beta1/daemonsets":                    "1.16",
	"rbac.authorization.k8s.io/v1alpha1/roles":         "1.29",
	"rbac.authorization.k8s.io/v1beta1/roles":          "1.22",
	"storage.k8s.io/v1beta1/csidrivers":                "1.22",
	"storage.k8s.io/v1beta1/storageclasses":            "1.22",
	"policy/v1beta1/poddisruptionbudgets":              "1.25",
	"policy/v1beta1/podsecuritypolicies":               "1.25",
	"autoscaling/v2beta1/horizontalpodautoscalers":     "1.26",
	"autoscaling/v2beta2/horizontalpodautoscalers":     "1.26",
	"flowcontrol.apiserver.k8s.io/v1beta1/flowschemas": "1.29",
	"flowcontrol.apiserver.k8s.io/v1beta2/flowschemas": "1.29",
}

// detectDeprecatedAPIs checks which API group/versions the cluster actually has
// registered against the known deprecation list.
func (c *DefaultClusterCollector) detectDeprecatedAPIs(snapshot *ClusterSnapshot) {
	_, apiLists, err := c.DiscoveryClient.ServerGroupsAndResources()
	if err != nil {
		// Non-fatal: partial results are still useful
		if apiLists == nil {
			return
		}
	}
	for _, apiList := range apiLists {
		for _, r := range apiList.APIResources {
			key := apiList.GroupVersion + "/" + r.Name
			if removedIn, found := deprecatedAPIRemovedIn[key]; found {
				snapshot.DeprecatedAPIs++
				snapshot.DeprecatedAPIList = append(snapshot.DeprecatedAPIList, DeprecatedAPI{
					Group:     r.Group,
					Version:   apiList.GroupVersion,
					Resource:  r.Name,
					RemovedIn: removedIn,
				})
			}
		}
	}
}
