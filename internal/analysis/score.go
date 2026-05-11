package analysis

import (
	"fmt"
	"strings"

	"github.com/miropshq/mirops/internal/collector"
)

const (
	scoreThreshold = 70

	levelSafe    = "SAFE"
	levelWarning = "WARNING"
	levelBlock   = "BLOCK"
)

// Calculate computes the upgrade readiness report from a ClusterSnapshot.
// Formula based on:
//
//	Health   (25): 25 - (notReadyRatio*100*0.2) - (restartRatio*100*0.1)
//	Capacity (30): 30 - (cpuPressure*30) - (memPressure*30)
//	Stability(20): 20 - (podDelta*100*0.1) - (restartDelta*0.5)
//	Risk     (25): 25 - (deprecatedApis*5) - (addonIssues*10)
func Calculate(snap *collector.ClusterSnapshot, targetVersion string) *Report {
	m := buildMetrics(snap)
	c := buildConditions(snap, m)

	health := calcHealth(m)
	capacity := calcCapacity(m)
	stability := calcStability(m)
	risk := calcRisk(m)
	total := health + capacity + stability + risk

	// Hard overrides
	if c.PDBBlocking || c.HighCPUPressure || c.HighMemoryPressure {
		total = 0
	}

	level, allow := decide(total, c)

	issues := buildIssues(snap)
	workloads := buildWorkloads(snap)

	return &Report{
		Cluster:        snap.ClusterName,
		ClusterVersion: snap.ClusterVersion,
		TargetVersion:  targetVersion,
		Decision: Decision{
			Threshold: scoreThreshold,
			Allow:     allow,
			Level:     level,
		},
		Reason: buildReason(level, c, m, snap),
		Scores: Scores{
			Total:     total,
			Health:    health,
			Capacity:  capacity,
			Stability: stability,
			Risk:      risk,
		},
		Conditions: c,
		Metrics:    m,
		Workloads:  workloads,
		Issues:     issues,
	}
}

func buildMetrics(snap *collector.ClusterSnapshot) Metrics {
	return Metrics{
		Pods: PodMetrics{
			Total:    snap.TotalPods,
			NotReady: snap.NotReadyPods,
			Restarts: snap.TotalRestarts,
		},
		Resources: ResourceMetrics{
			CPUPressure:    snap.CPURequests / nonZero(snap.CPUCapacity),
			MemoryPressure: snap.MemRequests / nonZero(snap.MemCapacity),
		},
		Stability: StabilityMetrics{
			PodDelta:     abs(float64(snap.TotalPods-snap.PreviousTotalPods)) / float64(nonZeroInt(snap.TotalPods)),
			RestartDelta: snap.TotalRestarts - snap.PreviousRestarts,
		},
		Compatibility: CompatibilityMetrics{
			DeprecatedAPIs: snap.DeprecatedAPIs,
			AddonIssues:    snap.AddonIssues,
		},
	}
}

func buildConditions(snap *collector.ClusterSnapshot, m Metrics) Conditions {
	return Conditions{
		PDBBlocking:        snap.PDBBlocking,
		HighCPUPressure:    m.Resources.CPUPressure > 0.9,
		HighMemoryPressure: m.Resources.MemoryPressure > 0.9,
		UnstableCluster:    m.Pods.Total > 0 && float64(m.Pods.NotReady)/float64(nonZeroInt(m.Pods.Total)) > 0.05,
	}
}

func calcHealth(m Metrics) int {
	notReadyRatio := float64(m.Pods.NotReady) / float64(nonZeroInt(m.Pods.Total))
	restartRatio := float64(m.Pods.Restarts) / float64(nonZeroInt(m.Pods.Total))
	score := 25 - (notReadyRatio * 100 * 0.2) - (restartRatio * 100 * 0.1)
	return clamp(int(score), 25)
}

func calcCapacity(m Metrics) int {
	score := 30 - (m.Resources.CPUPressure * 30) - (m.Resources.MemoryPressure * 30)
	return clamp(int(score), 30)
}

func calcStability(m Metrics) int {
	score := 20 - (m.Stability.PodDelta * 100 * 0.1) - (float64(m.Stability.RestartDelta) * 0.5)
	return clamp(int(score), 20)
}

func calcRisk(m Metrics) int {
	score := 25 - float64(m.Compatibility.DeprecatedAPIs*5) - float64(m.Compatibility.AddonIssues*10)
	return clamp(int(score), 25)
}

func decide(total int, c Conditions) (string, bool) {
	if c.PDBBlocking || c.HighCPUPressure || c.HighMemoryPressure || total < scoreThreshold {
		return levelBlock, false
	}
	if total < 90 {
		return levelWarning, true
	}
	return levelSafe, true
}

func buildIssues(snap *collector.ClusterSnapshot) []string {
	total := len(snap.PodIssues) + len(snap.NodeIssues) + len(snap.PDBIssues) +
		len(snap.StatefulSetIssues) + len(snap.DaemonSetIssues) + len(snap.JobIssues) + len(snap.DeprecatedAPIList)
	issues := make([]string, 0, total)
	for _, p := range snap.PodIssues {
		issues = append(issues, fmt.Sprintf("pod %s/%s: %s (restarts: %d)", p.Namespace, p.Name, p.Reason, p.Restarts))
	}
	for _, n := range snap.NodeIssues {
		issues = append(issues, fmt.Sprintf("node %s: %s", n.Name, n.Reason))
	}
	for _, pdb := range snap.PDBIssues {
		issues = append(issues, fmt.Sprintf("pdb %s/%s: would block drain", pdb.Namespace, pdb.Name))
	}
	for _, ss := range snap.StatefulSetIssues {
		issues = append(issues, fmt.Sprintf("statefulset %s/%s: %d/%d replicas ready", ss.Namespace, ss.Name, ss.ReadyReplicas, ss.TotalReplicas))
	}
	for _, ds := range snap.DaemonSetIssues {
		issues = append(issues, fmt.Sprintf("daemonset %s/%s: %d pod(s) unavailable", ds.Namespace, ds.Name, ds.NumberUnavailable))
	}
	for _, j := range snap.JobIssues {
		issues = append(issues, fmt.Sprintf("job %s/%s: %d active pod(s) may be interrupted", j.Namespace, j.Name, j.Active))
	}
	for _, api := range snap.DeprecatedAPIList {
		issues = append(issues, fmt.Sprintf("deprecated API %s (%s) removed in k8s %s", api.Resource, api.Version, api.RemovedIn))
	}
	return issues
}

func buildReason(level string, c Conditions, m Metrics, snap *collector.ClusterSnapshot) string {
	if c.PDBBlocking {
		if len(snap.PDBIssues) > 0 {
			p := snap.PDBIssues[0]
			return fmt.Sprintf("Blocked by PodDisruptionBudget %s/%s", p.Namespace, p.Name)
		}
		return "Blocked by PodDisruptionBudget"
	}
	if len(snap.NodeIssues) > 0 {
		msgs := make([]string, 0, len(snap.NodeIssues))
		for _, n := range snap.NodeIssues {
			msgs = append(msgs, fmt.Sprintf("%s (%s)", n.Name, n.Reason))
		}
		return fmt.Sprintf("%d node(s) with issues: %s", len(snap.NodeIssues), strings.Join(msgs, ", "))
	}
	if c.HighCPUPressure {
		return fmt.Sprintf("High CPU pressure detected (%.0f%% of capacity)", m.Resources.CPUPressure*100)
	}
	if c.HighMemoryPressure {
		return fmt.Sprintf("High memory pressure detected (%.0f%% of capacity)", m.Resources.MemoryPressure*100)
	}
	if c.UnstableCluster && len(snap.PodIssues) > 0 {
		msgs := make([]string, 0, len(snap.PodIssues))
		for _, p := range snap.PodIssues {
			msgs = append(msgs, fmt.Sprintf("%s/%s (%s)", p.Namespace, p.Name, p.Reason))
		}
		return fmt.Sprintf("%d pod(s) not ready: %s", len(snap.PodIssues), strings.Join(msgs, ", "))
	}
	if len(snap.StatefulSetIssues) > 0 {
		ss := snap.StatefulSetIssues[0]
		return fmt.Sprintf("StatefulSet %s/%s not fully ready (%d/%d replicas)", ss.Namespace, ss.Name, ss.ReadyReplicas, ss.TotalReplicas)
	}
	if len(snap.DaemonSetIssues) > 0 {
		ds := snap.DaemonSetIssues[0]
		return fmt.Sprintf("DaemonSet %s/%s has %d unavailable pod(s)", ds.Namespace, ds.Name, ds.NumberUnavailable)
	}
	if len(snap.JobIssues) > 0 {
		return fmt.Sprintf("%d active job(s) may be interrupted during upgrade", len(snap.JobIssues))
	}
	if m.Compatibility.DeprecatedAPIs > 0 {
		return fmt.Sprintf("Risk detected: %d deprecated API(s) in use", m.Compatibility.DeprecatedAPIs)
	}
	if m.Compatibility.AddonIssues > 0 {
		return fmt.Sprintf("Risk detected: %d addon issue(s)", m.Compatibility.AddonIssues)
	}
	if level == levelSafe {
		return "Cluster is ready for upgrade"
	}
	return "Cluster upgrade requires attention"
}

func buildWorkloads(snap *collector.ClusterSnapshot) ReportWorkloads {
	nodes := make([]NodeReport, 0, len(snap.NodeWorkloads))
	for _, n := range snap.NodeWorkloads {
		nodes = append(nodes, NodeReport{Name: n.Name, Status: n.Status, Conditions: n.Conditions})
	}

	deployments := make([]DeploymentReport, 0, len(snap.DeploymentWorkloads))
	for _, d := range snap.DeploymentWorkloads {
		pods := make([]WorkloadPodReport, 0, len(d.Pods))
		for _, p := range d.Pods {
			pods = append(pods, WorkloadPodReport{Name: p.Name, Reason: p.Reason, Restarts: p.Restarts})
		}
		deployments = append(deployments, DeploymentReport{
			Namespace: d.Namespace, Name: d.Name,
			ReadyReplicas: d.ReadyReplicas, DesiredReplicas: d.DesiredReplicas,
			Pods: pods,
		})
	}

	statefulsets := make([]StatefulSetReport, 0, len(snap.StatefulSetIssues))
	for _, ss := range snap.StatefulSetIssues {
		pods := make([]WorkloadPodReport, 0, len(ss.Pods))
		for _, p := range ss.Pods {
			pods = append(pods, WorkloadPodReport{Name: p.Name, Reason: p.Reason, Restarts: p.Restarts})
		}
		statefulsets = append(statefulsets, StatefulSetReport{
			Namespace: ss.Namespace, Name: ss.Name,
			ReadyReplicas: ss.ReadyReplicas, DesiredReplicas: ss.TotalReplicas,
			Pods: pods,
		})
	}

	daemonsets := make([]DaemonSetReport, 0, len(snap.DaemonSetIssues))
	for _, ds := range snap.DaemonSetIssues {
		pods := make([]WorkloadPodReport, 0, len(ds.Pods))
		for _, p := range ds.Pods {
			pods = append(pods, WorkloadPodReport{Name: p.Name, Reason: p.Reason, Restarts: p.Restarts})
		}
		daemonsets = append(daemonsets, DaemonSetReport{
			Namespace: ds.Namespace, Name: ds.Name,
			NumberUnavailable: ds.NumberUnavailable,
			Pods:              pods,
		})
	}

	jobs := make([]JobReport, 0, len(snap.JobIssues))
	for _, j := range snap.JobIssues {
		pods := make([]WorkloadPodReport, 0, len(j.Pods))
		for _, p := range j.Pods {
			pods = append(pods, WorkloadPodReport{Name: p.Name, Reason: p.Reason, Restarts: p.Restarts})
		}
		jobs = append(jobs, JobReport{
			Namespace: j.Namespace, Name: j.Name,
			Active: j.Active, Pods: pods,
		})
	}

	pdbs := make([]PDBReport, 0, len(snap.PDBIssues))
	for _, pdb := range snap.PDBIssues {
		pdbs = append(pdbs, PDBReport{Namespace: pdb.Namespace, Name: pdb.Name})
	}

	apis := make([]DeprecatedAPIReport, 0, len(snap.DeprecatedAPIList))
	for _, api := range snap.DeprecatedAPIList {
		apis = append(apis, DeprecatedAPIReport{
			Group: api.Group, Version: api.Version,
			Resource: api.Resource, RemovedIn: api.RemovedIn,
		})
	}

	return ReportWorkloads{
		Nodes:          nodes,
		Deployments:    deployments,
		StatefulSets:   statefulsets,
		DaemonSets:     daemonsets,
		Jobs:           jobs,
		PDBs:           pdbs,
		DeprecatedAPIs: apis,
	}
}

func nonZero(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}

func nonZeroInt(v int) int {
	if v == 0 {
		return 1
	}
	return v
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func clamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}
