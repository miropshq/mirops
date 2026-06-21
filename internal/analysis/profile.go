package analysis

// ScoringProfile bundles the decision thresholds that define how strict upgrade-readiness
// scoring is. It is selected per-analysis via UpgradeAnalysis.spec.scoringProfile, so the
// same operator can score a production cluster strictly and a staging cluster leniently.
type ScoringProfile struct {
	Name             string
	UnstableWarnPct  float64 // notReady fraction above which the cluster can't be SAFE
	UnstableBlockPct float64 // notReady fraction above which the upgrade is BLOCKed
	SafeThreshold    int     // total score at/above which the cluster may be SAFE
	BlockThreshold   int     // total score below which the upgrade is BLOCKed
}

// Profile names (match the CRD enum).
const (
	ProfileProduction    = "production"
	ProfileNonProduction = "non-production"
)

// scoringProfiles are the built-in presets. production is strict (low tolerance for broken
// pods, high bar for SAFE); non-production is lenient.
var scoringProfiles = map[string]ScoringProfile{
	ProfileProduction: {
		Name:             ProfileProduction,
		UnstableWarnPct:  0.05, // >5% not ready → at least WARNING
		UnstableBlockPct: 0.30, // >30% not ready → BLOCK
		SafeThreshold:    90,
		BlockThreshold:   70,
	},
	ProfileNonProduction: {
		Name:             ProfileNonProduction,
		UnstableWarnPct:  0.15, // >15% not ready → at least WARNING
		UnstableBlockPct: 0.60, // >60% not ready → BLOCK
		SafeThreshold:    85,
		BlockThreshold:   60,
	},
}

// profileFor returns the named scoring profile, defaulting to production for an empty or
// unrecognized name (fail-safe: strict by default).
func profileFor(name string) ScoringProfile {
	if p, ok := scoringProfiles[name]; ok {
		return p
	}
	return scoringProfiles[ProfileProduction]
}
