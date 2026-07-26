package analysis

import (
	"fmt"
	"strings"

	"github.com/miropshq/mirops/internal/collector"
)

const (
	levelSafe     = "SAFE"
	levelWarning  = "WARNING"
	levelCritical = "CRITICAL"
)

// Calculate computes the upgrade readiness report from a ClusterSnapshot.
// Formula based on:
//
//	Health   (25): 25 - (notReadyRatio*15) - (restartingRatio*10)   // both proportional
//	Capacity (30): 30 - (cpuPressure*15) - (memPressure*15)
//	Stability(20): 20 - (podDelta*100*0.1) - (restartDelta*0.5) - (restartingRatio*20)
//	Risk     (25): 25 - (deprecatedApis*5) - (addonIssues*10)
//
// An unstable cluster (>5% pods not ready) caps the decision at WARNING regardless of the
// total, so a small cluster with broken pods can't be declared SAFE.
func Calculate(snap *collector.ClusterSnapshot, targetVersion, profileName string) *Report {
	profile := profileFor(profileName)
	m := buildMetrics(snap)
	c := buildConditions(snap, m, profile)

	health := calcHealth(m)
	capacity := calcCapacity(m)
	stability := calcStability(m)
	risk := calcRisk(m)
	total := health + capacity + stability + risk

	// Hard overrides
	if c.PDBBlocking || c.HighCPUPressure || c.HighMemoryPressure {
		total = 0
	}

	level, allow := decide(total, c, profile)

	issues := buildIssues(snap)
	workloads := buildWorkloads(snap)

	return &Report{
		Cluster:        snap.ClusterName,
		ClusterVersion: snap.ClusterVersion,
		TargetVersion:  targetVersion,
		Decision: Decision{
			Allow: allow,
			Level: level,
		},
		Reason: buildReason(level, c, m, snap),
		Scores: Scores{
			Total: total,
			Base: BaseScores{
				Score:        total,
				Weight:       "100%",
				Contribution: total,
				Health:       health,
				Capacity:     capacity,
				Stability:    stability,
				Risk:         risk,
			},
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
			Total:      snap.TotalPods,
			NotReady:   snap.NotReadyPods,
			Restarts:   snap.TotalRestarts,
			Restarting: snap.RestartingPods,
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

func buildConditions(snap *collector.ClusterSnapshot, m Metrics, p ScoringProfile) Conditions {
	notReadyRatio := float64(m.Pods.NotReady) / float64(nonZeroInt(m.Pods.Total))
	return Conditions{
		PDBBlocking:        snap.PDBBlocking,
		HighCPUPressure:    m.Resources.CPUPressure > 0.9,
		HighMemoryPressure: m.Resources.MemoryPressure > 0.9,
		UnstableCluster:    m.Pods.Total > 0 && notReadyRatio > p.UnstableWarnPct,
		SeverelyUnstable:   m.Pods.Total > 0 && notReadyRatio > p.UnstableBlockPct,
	}
}

// calcHealth scores pod health out of 25 from two proportional signals, so it scales with
// cluster size (one bad pod out of 1000 is negligible; out of 6 it matters):
//   - notReady fraction → up to 15 points
//   - abnormally-restarting fraction → up to 10 points
//
// Together they partition the 25-point budget. A single critical pod that these ratios
// dilute is caught instead by the per-component graph risk and the decision guard.
func calcHealth(m Metrics) int {
	total := float64(nonZeroInt(m.Pods.Total))
	notReadyPenalty := float64(m.Pods.NotReady) / total * 15
	restartPenalty := float64(m.Pods.Restarting) / total * 10
	return clamp(int(25-notReadyPenalty-restartPenalty), 25)
}

func calcCapacity(m Metrics) int {
	// CPU and memory share the 30-point budget (15 each) so their combined penalty can't exceed
	// 30 — a cluster at 50% on both is healthy and must not score 0. Note: >90% on either is a
	// hard override (CRITICAL) handled in decide/collectBlockers; this is only the gauge below that.
	score := 30 - (m.Resources.CPUPressure * 15) - (m.Resources.MemoryPressure * 15)
	return clamp(int(score), 30)
}

func calcStability(m Metrics) int {
	// Abnormally-restarting pods (crash-loops at rate >= 10/24h) are instability even when the
	// restart delta is zero — a pod stuck crash-looping is not "stable". Penalise them so a chronic
	// crash-loop pulls the score below the SAFE bar (WARNING), while not blocking on its own.
	restartingRatio := float64(m.Pods.Restarting) / float64(nonZeroInt(m.Pods.Total))
	score := 20 -
		(m.Stability.PodDelta * 100 * 0.1) -
		(float64(m.Stability.RestartDelta) * 0.5) -
		(restartingRatio * 20)
	return clamp(int(score), 20)
}

func calcRisk(m Metrics) int {
	score := 25 - float64(m.Compatibility.DeprecatedAPIs*5) - float64(m.Compatibility.AddonIssues*10)
	return clamp(int(score), 25)
}

// decide derives the decision level from the snapshot-only conditions. The score no longer
// drives blocking on its own — it is a readiness gauge, not a gate. CRITICAL comes only from
// deterministic facts (a PDB that would stall the drain, CPU/memory exhaustion, or pods not
// ready beyond the profile's block ratio). A low score can still warrant a WARNING.
//
// Graph/compat facts (incompatible add-ons, Lost PVCs) are layered on afterwards by
// ApplyGraphDecision, which runs last with the full mirror in hand.
func decide(total int, c Conditions, p ScoringProfile) (string, bool) {
	if c.PDBBlocking || c.HighCPUPressure || c.HighMemoryPressure || c.SeverelyUnstable {
		return levelCritical, false
	}
	// An unstable cluster can't be declared SAFE even if the proportional score is high —
	// a small cluster with broken pods would otherwise pass. The thresholds come from the
	// scoring profile (production strict, non-production lenient).
	if c.UnstableCluster || total < p.SafeThreshold {
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
		if j.Status == "Failed" {
			issues = append(issues, fmt.Sprintf("job %s/%s: failed (%s)", j.Namespace, j.Name, j.Reason))
		} else {
			issues = append(issues, fmt.Sprintf("job %s/%s: %d active pod(s) may be interrupted", j.Namespace, j.Name, j.Active))
		}
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
		failed := 0
		for _, job := range snap.JobIssues {
			if job.Status == "Failed" {
				failed++
			}
		}
		if failed > 0 {
			return fmt.Sprintf("%d job(s) failed after exhausting retries", failed)
		}
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
			Active: j.Active, Status: j.Status, Reason: j.Reason, Pods: pods,
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

// ApplyAIScore blends an AI score into an existing report using the 70/30 formula:
//
//	total = base*0.7 + aiScore*0.3
//
// The decision level is re-evaluated against the new total.
// reasoning is only set when non-empty (ai.enabled == true path).
func ApplyAIScore(report *Report, aiScore int, reasoning, model, profileName string) {
	baseScore := report.Scores.Base.Score
	baseContribution := int(float64(baseScore) * 0.7)
	aiContribution := int(float64(aiScore) * 0.3)
	blended := baseContribution + aiContribution

	report.Scores.Base.Weight = "70%"
	report.Scores.Base.Contribution = baseContribution
	report.Scores.AI = AIScores{
		Score:        aiScore,
		Weight:       "30%",
		Contribution: aiContribution,
		Model:        model,
	}
	report.Scores.Total = blended
	if reasoning != "" {
		report.AIReasoning = reasoning
	}
	level, allow := decide(blended, report.Conditions, profileFor(profileName))
	report.Decision.Level = level
	report.Decision.Allow = allow
}

// ApplyGraphDecision is the final decision authority. It runs after Calculate, ApplyRisk and
// ApplyAIScore — with the full mirror (add-ons + graph) attached to the report — and escalates
// the decision to CRITICAL for any deterministic upgrade blocker. It unifies two layers:
//
//   - snapshot conditions already flagged by decide (PDB, CPU/memory, pods not ready), and
//   - graph/compat facts the base score can't see (incompatible add-ons, Lost PVCs).
//
// Every blocker is recorded on report.Decision.Blockers so mirops-cli --enforce and the UI can
// show all reasons, not just one; allow becomes (no blockers). The readiness score is left
// untouched — it is a gauge, not the gate. Propagated graph risk that doesn't stem from one of
// these deterministic facts informs risk.byNamespace but does not block on its own.
func ApplyGraphDecision(report *Report) {
	blockers := collectBlockers(report)
	report.Decision.Blockers = blockers
	if len(blockers) > 0 {
		report.Decision.Level = levelCritical
		report.Decision.Allow = false
	}
}

// collectBlockers gathers every critical (allow=false) reason from the finalized report.
func collectBlockers(report *Report) []string {
	var b []string
	c := report.Conditions
	if c.PDBBlocking {
		if len(report.Workloads.PDBs) > 0 {
			p := report.Workloads.PDBs[0]
			b = append(b, fmt.Sprintf("PodDisruptionBudget %s/%s would stall node drain", p.Namespace, p.Name))
		} else {
			b = append(b, "a PodDisruptionBudget would stall node drain")
		}
	}
	if c.HighCPUPressure {
		b = append(b, fmt.Sprintf("CPU pressure at %.0f%% of capacity", report.Metrics.Resources.CPUPressure*100))
	}
	if c.HighMemoryPressure {
		b = append(b, fmt.Sprintf("memory pressure at %.0f%% of capacity", report.Metrics.Resources.MemoryPressure*100))
	}
	if c.SeverelyUnstable {
		pods := report.Metrics.Pods
		pct := 0.0
		if pods.Total > 0 {
			pct = float64(pods.NotReady) / float64(pods.Total) * 100
		}
		b = append(b, fmt.Sprintf("%.0f%% of pods not ready (%d/%d)", pct, pods.NotReady, pods.Total))
	}
	// Graph/compat facts: an incompatible add-on or a Lost PVC seeds criticalRisk in the graph
	// and deterministically fails the upgrade, so each is a hard blocker on its own.
	for _, a := range report.Addons {
		if a.Status == "incompatible" {
			if a.RequiredVersion != "" {
				b = append(b, fmt.Sprintf("incompatible add-on: %s %s (upgrade to %s)", a.Name, a.Version, a.RequiredVersion))
			} else {
				b = append(b, fmt.Sprintf("incompatible add-on: %s %s", a.Name, a.Version))
			}
		}
	}
	if report.Graph != nil {
		for _, n := range report.Graph.Nodes {
			if n.Type == "storage" && n.Status == "Lost" {
				b = append(b, fmt.Sprintf("PVC %s/%s is Lost", n.Namespace, n.Name))
			}
		}
	}
	return b
}
