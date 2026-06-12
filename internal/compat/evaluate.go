package compat

import (
	"fmt"
	"strings"

	"github.com/blang/semver/v4"

	"github.com/miropshq/mirops/internal/collector"
)

// Evaluate checks each detected add-on against the matrix for the target Kubernetes
// version and returns one result per add-on. It also returns the count of incompatible
// add-ons, which the caller stores in ClusterSnapshot.AddonIssues so the existing risk
// score penalizes them.
func Evaluate(addons []collector.DetectedAddon, targetVersion string, m Matrix) ([]AddonCompatibility, int) {
	target, terr := parseSemver(targetVersion)
	results := make([]AddonCompatibility, 0, len(addons))
	incompatible := 0

	for _, a := range addons {
		res := evaluateAddon(a, target, terr == nil, m[a.Name])
		if res.Status == StatusIncompatible {
			incompatible++
		}
		results = append(results, res)
	}
	return results, incompatible
}

func evaluateAddon(a collector.DetectedAddon, target semver.Version, targetOK bool, rules []CompatRule) AddonCompatibility {
	res := AddonCompatibility{Name: a.Name, Version: a.Version, Status: StatusUnknown}

	if len(rules) == 0 {
		res.Note = "no compatibility data for this add-on"
		return res
	}
	if !targetOK {
		res.Note = "target version could not be parsed"
		return res
	}

	av, averr := parseSemver(a.Version)
	if a.Version == "" || averr != nil {
		res.Note = "add-on version unknown; cannot match a specific rule"
		return res
	}

	// Find rules that apply to the detected add-on version.
	var applicable []CompatRule
	for _, rule := range rules {
		ar, err := semver.ParseRange(rule.AddonRange)
		if err != nil {
			continue
		}
		if ar(av) {
			applicable = append(applicable, rule)
		}
	}
	if len(applicable) == 0 {
		res.Note = "no compatibility rule matches the detected add-on version"
		return res
	}

	// Compatible if any applicable rule's k8s range includes the target.
	for _, rule := range applicable {
		kr, err := semver.ParseRange(rule.K8sRange)
		if err != nil {
			continue
		}
		if kr(target) {
			res.Status = StatusCompatible
			res.Note = rule.Note
			return res
		}
	}

	// Incompatible: the detected version does not support the target. Do an inverse lookup
	// across ALL rules for this add-on to recommend the version to upgrade TO.
	res.Status = StatusIncompatible
	res.RequiredVersion = requiredAddonVersion(rules, target)
	if res.RequiredVersion != "" {
		res.Note = fmt.Sprintf("current version supports k8s %s; upgrade to add-on %s for the target version",
			applicable[0].K8sRange, res.RequiredVersion)
	} else {
		res.Note = fmt.Sprintf("current version supports k8s %s; no known add-on version supports the target version",
			applicable[0].K8sRange)
	}
	return res
}

// requiredAddonVersion returns the add-on version range whose rule supports the target
// Kubernetes version, i.e. the version the operator should upgrade the add-on to.
func requiredAddonVersion(rules []CompatRule, target semver.Version) string {
	for _, rule := range rules {
		if kr, err := semver.ParseRange(rule.K8sRange); err == nil && kr(target) {
			return rule.AddonRange
		}
	}
	return ""
}

// parseSemver normalizes a Kubernetes/add-on version string into a semver.Version.
// Accepts "v1.30.1", "1.31", "1.21.4-distroless", "v1.30.1+k3s1".
func parseSemver(v string) (semver.Version, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return semver.Parse(parts[0] + "." + parts[1] + "." + parts[2])
}
