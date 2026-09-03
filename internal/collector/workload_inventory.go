package collector

import (
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
)

// collectWorkloads enumerates every workload of every kind (healthy or not) into
// snapshot.Workloads, enriched with pod-template labels, config/PVC references and the
// nodes its pods run on. This is the complete inventory the dependency graph mirrors.
func (c *DefaultClusterCollector) collectWorkloads(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot, nodesByWorkload map[string]map[string]bool) error {
	nodes := func(kind, ns, name string) []string {
		return sortedKeys(nodesByWorkload[kind+"/"+ns+"/"+name])
	}

	depList := &appsv1.DeploymentList{}
	if err := c.Client.List(ctx, depList); err != nil {
		return err
	}
	for i := range depList.Items {
		d := &depList.Items[i]
		if excluded[d.Namespace] {
			continue
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		w := newWorkload("Deployment", d.Namespace, d.Name, &d.Spec.Template, nodes)
		w.Status = replicaStatus(d.Status.ReadyReplicas, desired)
		snapshot.Workloads = append(snapshot.Workloads, w)
	}

	ssList := &appsv1.StatefulSetList{}
	if err := c.Client.List(ctx, ssList); err != nil {
		return err
	}
	for i := range ssList.Items {
		s := &ssList.Items[i]
		if excluded[s.Namespace] {
			continue
		}
		desired := int32(1)
		if s.Spec.Replicas != nil {
			desired = *s.Spec.Replicas
		}
		w := newWorkload("StatefulSet", s.Namespace, s.Name, &s.Spec.Template, nodes)
		w.Status = replicaStatus(s.Status.ReadyReplicas, desired)
		snapshot.Workloads = append(snapshot.Workloads, w)
	}

	dsList := &appsv1.DaemonSetList{}
	if err := c.Client.List(ctx, dsList); err != nil {
		return err
	}
	for i := range dsList.Items {
		ds := &dsList.Items[i]
		if excluded[ds.Namespace] {
			continue
		}
		w := newWorkload("DaemonSet", ds.Namespace, ds.Name, &ds.Spec.Template, nodes)
		w.Status = replicaStatus(ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)
		snapshot.Workloads = append(snapshot.Workloads, w)
	}

	jobList := &batchv1.JobList{}
	if err := c.Client.List(ctx, jobList); err != nil {
		return err
	}
	for i := range jobList.Items {
		j := &jobList.Items[i]
		if excluded[j.Namespace] {
			continue
		}
		w := newWorkload("Job", j.Namespace, j.Name, &j.Spec.Template, nodes)
		w.Status = jobStatus(j)
		snapshot.Workloads = append(snapshot.Workloads, w)
	}

	cronList := &batchv1.CronJobList{}
	if err := c.Client.List(ctx, cronList); err != nil {
		return err
	}
	for i := range cronList.Items {
		cj := &cronList.Items[i]
		if excluded[cj.Namespace] {
			continue
		}
		w := newWorkload("CronJob", cj.Namespace, cj.Name, &cj.Spec.JobTemplate.Spec.Template, nodes)
		w.Status = "Scheduled"
		if cj.Spec.Suspend != nil && *cj.Spec.Suspend {
			w.Status = "Suspended"
		}
		w.Nodes = nil // CronJobs have no long-running pods of their own
		snapshot.Workloads = append(snapshot.Workloads, w)
	}

	return nil
}

// collectPVCs gathers PersistentVolumeClaims (stateful data at risk during a node drain).
func (c *DefaultClusterCollector) collectPVCs(ctx context.Context, excluded map[string]bool, snapshot *ClusterSnapshot) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := c.Client.List(ctx, pvcList); err != nil {
		return
	}
	bindingModes, defaultSC := c.storageClassBindingModes(ctx)
	for _, pvc := range pvcList.Items {
		if excluded[pvc.Namespace] {
			continue
		}
		sc := ""
		if pvc.Spec.StorageClassName != nil {
			sc = *pvc.Spec.StorageClassName
		}
		// A PVC with no explicit StorageClass uses the cluster's default one.
		lookup := sc
		if lookup == "" {
			lookup = defaultSC
		}
		snapshot.PVCs = append(snapshot.PVCs, PVCRef{
			Namespace:    pvc.Namespace,
			Name:         pvc.Name,
			StorageClass: sc,
			Phase:        string(pvc.Status.Phase),
			BindingMode:  bindingModes[lookup],
		})
	}
}

// storageClassBindingModes maps StorageClass name -> volumeBindingMode and returns the default
// StorageClass name (the one annotated as default). Best-effort: returns empty results when
// StorageClasses can't be listed, so PVC scoring falls back to phase alone.
func (c *DefaultClusterCollector) storageClassBindingModes(ctx context.Context) (map[string]string, string) {
	modes := map[string]string{}
	defaultSC := ""
	scList := &storagev1.StorageClassList{}
	if err := c.Client.List(ctx, scList); err != nil {
		return modes, defaultSC
	}
	for _, sc := range scList.Items {
		if sc.VolumeBindingMode != nil {
			modes[sc.Name] = string(*sc.VolumeBindingMode)
		}
		if sc.Annotations["storageclass.kubernetes.io/is-default-class"] == annotationTrue {
			defaultSC = sc.Name
		}
	}
	return modes, defaultSC
}

// newWorkload builds a Workload from a pod template, extracting labels, config/PVC refs,
// sidecar usage and the nodes its pods run on.
func newWorkload(kind, ns, name string, tmpl *corev1.PodTemplateSpec, nodes func(kind, ns, name string) []string) Workload {
	return Workload{
		Kind:             kind,
		Namespace:        ns,
		Name:             name,
		PodLabels:        tmpl.Labels,
		ConfigRefs:       podSpecConfigRefs(&tmpl.Spec),
		PVCs:             podSpecPVCs(&tmpl.Spec),
		Nodes:            nodes(kind, ns, name),
		UsesIstioSidecar: usesIstioSidecar(tmpl.Labels, tmpl.Annotations),
	}
}

// recordPodNode attributes a pod's node to its logical workload, for runs-on edges.
func recordPodNode(pod *corev1.Pod, rsToDeploy map[string]string, nodesByWorkload map[string]map[string]bool) {
	if pod.Spec.NodeName == "" {
		return
	}
	kind, name, ok := resolveWorkload(pod, rsToDeploy)
	if !ok {
		return
	}
	key := kind + "/" + pod.Namespace + "/" + name
	if nodesByWorkload[key] == nil {
		nodesByWorkload[key] = make(map[string]bool)
	}
	nodesByWorkload[key][pod.Spec.NodeName] = true
}

// groupNotReadyPod files a not-ready pod under its parent workload for the report's
// per-workload pod listing.
func groupNotReadyPod(pod *corev1.Pod, wp WorkloadPod, rsToDeploy map[string]string, byDeployment, byStatefulSet, byDaemonSet, byJob map[string][]WorkloadPod) {
	for _, ref := range pod.OwnerReferences {
		switch ref.Kind {
		case "ReplicaSet":
			if dName, ok := rsToDeploy[pod.Namespace+"/"+ref.Name]; ok {
				key := pod.Namespace + "/" + dName
				byDeployment[key] = append(byDeployment[key], wp)
			}
		case "StatefulSet":
			key := pod.Namespace + "/" + ref.Name
			byStatefulSet[key] = append(byStatefulSet[key], wp)
		case "DaemonSet":
			key := pod.Namespace + "/" + ref.Name
			byDaemonSet[key] = append(byDaemonSet[key], wp)
		case "Job":
			key := pod.Namespace + "/" + ref.Name
			byJob[key] = append(byJob[key], wp)
		}
	}
}

// resolveWorkload maps a pod to its logical workload (kind, name). ReplicaSet-owned pods
// resolve to their Deployment; otherwise the direct controller owner is used.
func resolveWorkload(pod *corev1.Pod, rsToDeploy map[string]string) (kind, name string, ok bool) {
	for _, ref := range pod.OwnerReferences {
		switch ref.Kind {
		case "ReplicaSet":
			if dName, found := rsToDeploy[pod.Namespace+"/"+ref.Name]; found {
				return "Deployment", dName, true
			}
		case "StatefulSet", "DaemonSet", "Job":
			return ref.Kind, ref.Name, true
		}
	}
	return "", "", false
}

// podSpecPVCs returns the PersistentVolumeClaim names a pod spec mounts.
func podSpecPVCs(spec *corev1.PodSpec) []string {
	var claims []string
	seen := make(map[string]bool)
	for _, v := range spec.Volumes {
		if v.PersistentVolumeClaim != nil && !seen[v.PersistentVolumeClaim.ClaimName] {
			seen[v.PersistentVolumeClaim.ClaimName] = true
			claims = append(claims, v.PersistentVolumeClaim.ClaimName)
		}
	}
	return claims
}

// replicaStatus derives a coarse status from ready vs desired replicas.
func replicaStatus(ready, desired int32) string {
	switch {
	case desired == 0:
		return "Scaled to zero"
	case ready >= desired:
		return "Healthy"
	case ready == 0:
		return "Down"
	default:
		return fmt.Sprintf("Degraded (%d/%d)", ready, desired)
	}
}

// jobStatus derives a status string from a Job's counters.
func jobStatus(j *batchv1.Job) string {
	if failed, _ := jobFailedReason(j); failed {
		return JobStatusFailed
	}
	switch {
	case j.Status.Active > 0:
		return JobStatusActive
	case j.Status.Succeeded > 0:
		return JobStatusCompleted
	default:
		return JobStatusPending
	}
}

// jobFailedReason returns true only for a terminal Job failure. Failed pod attempts by
// themselves are not enough: Kubernetes may still retry until backoffLimit is exhausted.
func jobFailedReason(j *batchv1.Job) (bool, string) {
	for _, condition := range j.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			reason := condition.Reason
			if reason == "" {
				reason = "Failed"
			}
			return true, reason
		}
	}
	return false, ""
}

// sortedKeys returns the keys of a set as a stable, sorted slice (nil set -> nil).
func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
