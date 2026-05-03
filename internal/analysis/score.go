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

	return &Report{
		Cluster:        snap.ClusterName,
		ClusterVersion: snap.ClusterVersion,
		TargetVersion:  targetVersion,
		Scores: Scores{
			Total:     total,
			Health:    health,
			Capacity:  capacity,
			Stability: stability,
			Risk:      risk,
		},
		Metrics:    m,
		Conditions: c,
		Decision: Decision{
			Threshold: scoreThreshold,
			Allow:     allow,
			Level:     level,
		},
		Reason: buildReason(level, c, m, snap),
		Issues: issues,
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
	issues := make([]string, 0, len(snap.PodIssues)+len(snap.NodeIssues)+len(snap.PDBIssues))
	for _, p := range snap.PodIssues {
		issues = append(issues, fmt.Sprintf("pod %s/%s: %s (restarts: %d)", p.Namespace, p.Name, p.Reason, p.Restarts))
	}
	for _, n := range snap.NodeIssues {
		issues = append(issues, fmt.Sprintf("node %s: %s", n.Name, n.Reason))
	}
	for _, pdb := range snap.PDBIssues {
		issues = append(issues, fmt.Sprintf("pdb %s/%s: would block drain", pdb.Namespace, pdb.Name))
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
	if m.Compatibility.DeprecatedAPIs > 0 || m.Compatibility.AddonIssues > 0 {
		return fmt.Sprintf("Risk detected: %d deprecated API(s), %d addon issue(s)", m.Compatibility.DeprecatedAPIs, m.Compatibility.AddonIssues)
	}
	if level == levelSafe {
		return "Cluster is ready for upgrade"
	}
	return "Cluster upgrade requires attention"
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
