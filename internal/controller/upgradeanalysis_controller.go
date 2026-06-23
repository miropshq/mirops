/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	miropsv1 "github.com/miropshq/mirops/api/v1"
	"github.com/miropshq/mirops/internal/analysis"
	"github.com/miropshq/mirops/internal/collector"
	"github.com/miropshq/mirops/internal/compat"
	"github.com/miropshq/mirops/internal/exporter"
	"github.com/miropshq/mirops/internal/graph"
)

// UpgradeAnalysisReconciler reconciles a UpgradeAnalysis object
type UpgradeAnalysisReconciler struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	Collector  collector.ClusterCollector
	ReportsDir string
	// OperatorNamespace is where the operator runs (from POD_NAMESPACE). Since UpgradeAnalysis
	// is cluster-scoped, the operator reads its own dependencies (AI secret, compat ConfigMap)
	// and the report's cloud-credentials secret from here, not from the CR's namespace.
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch

// Reconcile implements the reconciliation loop for UpgradeAnalysis
func (r *UpgradeAnalysisReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	ua := &miropsv1.UpgradeAnalysis{}
	if err := r.Client.Get(ctx, req.NamespacedName, ua); err != nil {
		log.Error(err, "unable to fetch UpgradeAnalysis")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling UpgradeAnalysis", "name", ua.Name, "operatorNamespace", r.OperatorNamespace)

	// Skip only for a periodic resync within the interval (avoids calling the AI on every status
	// update). A spec change (generation bump) or an on-demand refresh (the mirops.io/refresh
	// annotation changed) always forces an immediate re-analysis.
	if interval := ua.Spec.Resync.Interval.Duration; interval > 0 && ua.Status.LastAnalysisTime != nil {
		specChanged := ua.Generation != ua.Status.ObservedGeneration
		refreshRequested := ua.Annotations[refreshAnnotation] != ua.Status.LastRefresh
		if !specChanged && !refreshRequested {
			next := ua.Status.LastAnalysisTime.Add(interval)
			if time.Now().Before(next) {
				return ctrl.Result{RequeueAfter: time.Until(next)}, nil
			}
		}
	}

	// Collect cluster snapshot
	scope := collector.Scope{
		Mode:              string(ua.Spec.Scope.Mode),
		ExcludeNamespaces: ua.Spec.Scope.ExcludeNamespaces,
	}
	if scope.Mode == "" {
		scope.Mode = "all"
	}
	snapshot, err := r.Collector.Collect(ctx, scope)
	if err != nil {
		log.Error(err, "failed to collect cluster snapshot")
		return ctrl.Result{}, err
	}
	snapshot.PreviousTotalPods = ua.Status.LastTotalPods
	snapshot.PreviousRestarts = ua.Status.LastTotalRestarts

	log.Info("Cluster snapshot collected",
		"clusterVersion", snapshot.ClusterVersion,
		"namespaceCount", snapshot.NamespaceCount,
		"totalPods", snapshot.TotalPods,
		"resourceCount", len(snapshot.Resources),
	)

	// Reject invalid configuration: target version must be higher than the current
	// cluster version. This is a user config error, not an analysis decision, so it
	// sets decision=ERROR (distinct from the SAFE/WARNING/BLOCK analysis results)
	// and stops without running the analysis.
	if !versionIsHigher(ua.Spec.TargetVersion, snapshot.ClusterVersion) {
		tMaj, tMin := majorMinor(ua.Spec.TargetVersion)
		cMaj, cMin := majorMinor(snapshot.ClusterVersion)
		ua.Status.Decision = "ERROR"
		if tMaj == cMaj && tMin == cMin {
			ua.Status.Reason = fmt.Sprintf("cluster is already running version %s — targetVersion must be higher than the current version", snapshot.ClusterVersion)
		} else {
			ua.Status.Reason = fmt.Sprintf("targetVersion %s is lower than current cluster version %s — downgrades are not supported", ua.Spec.TargetVersion, snapshot.ClusterVersion)
		}
		ua.Status.LastAnalysisTime = &metav1.Time{Time: time.Now().UTC()}
		if err := r.Client.Status().Update(ctx, ua); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Mirops engine — Compatibility: evaluate detected add-ons against the target version
	// BEFORE scoring so incompatible add-ons feed the risk score (AddonIssues).
	matrix, err := r.loadCompatMatrix(ctx, r.OperatorNamespace)
	if err != nil {
		log.Error(err, "failed to load compatibility matrix, using built-in defaults")
		matrix = compat.DefaultMatrix()
	}
	addonResults, incompatible := compat.Evaluate(snapshot.DetectedAddons, ua.Spec.TargetVersion, matrix)
	snapshot.AddonIssues = incompatible

	// Calculate upgrade readiness score
	report := analysis.Calculate(snapshot, ua.Spec.TargetVersion, ua.Spec.ScoringProfile)
	now := time.Now().UTC()
	report.GeneratedAt = now.Format(time.RFC3339)
	report.Cluster = ua.Name

	// Mirops engine — Mirror + Dependency Graph + Risk. Build the logical graph, seed
	// risk from add-on compatibility, propagate along edges, and attach to the report.
	addonStatus := make(map[string]string, len(addonResults))
	for _, a := range addonResults {
		addonStatus[a.Name] = a.Status
	}
	g := graph.BuildFromSnapshot(snapshot)
	nsRisk := g.ApplyRisk(addonStatus)
	report.Addons = addonResults
	report.Graph = g
	report.Risk = &analysis.RiskBreakdown{ByNamespace: nsRisk}

	ua.Status.AddonsChecked = len(addonResults)
	ua.Status.IncompatibleAddons = incompatible

	log.Info("Analysis completed",
		"decision", report.Decision.Level,
		"baseScore", report.Scores.Base.Score,
		"reason", report.Reason,
		"addonsChecked", len(addonResults),
		"incompatibleAddons", incompatible,
	)

	// Apply AI score if enabled (base*0.7 + ai*0.3)
	if ua.Spec.AI.Enabled {
		aiScore, reasoning, actions, aiErr := r.scoreWithAI(ctx, ua, report)
		if aiErr != nil {
			ua.Status.AIError = classifyAIError(ua.Spec.AI.Provider, aiErr)
			log.Error(aiErr, "AI scoring failed, proceeding with base score only", "aiError", ua.Status.AIError)
		} else {
			ua.Status.AIError = ""
			model := ua.Spec.AI.Model
			if model == "" {
				if ua.Spec.AI.Provider == miropsv1.AIProviderOpenAI {
					model = "gpt-4o"
				} else {
					model = "claude-sonnet-4-6"
				}
			}
			analysis.ApplyAIScore(report, aiScore, reasoning, model, ua.Spec.ScoringProfile)
			ua.Status.AIModel = model
			log.Info("AI score applied",
				"aiScore", aiScore,
				"totalScore", report.Scores.Total,
				"model", model,
			)
			if ua.Spec.AI.Remediation.Enabled && len(actions) > 0 {
				if err := r.createRemediationPlan(ctx, ua, actions); err != nil {
					log.Error(err, "failed to create RemediationPlan")
				}
			}
		}
	}

	// Final decision authority: layer the deterministic graph/compat blockers (incompatible
	// add-ons, Lost PVCs) on top of the snapshot conditions and record every blocker. Runs
	// last so it sees the full mirror and has the last word over the base + AI decision.
	analysis.ApplyGraphDecision(report)

	// Always write report to the local reports directory so the HTTP server can serve it
	// regardless of whether the configured source is file, s3, or blob.
	localPath := r.ReportsDir + "/" + ua.Name + ".json"
	localExp := exporter.NewFileExporter(localPath)
	if err := localExp.Export(report); err != nil {
		log.Error(err, "failed to write local report")
		return ctrl.Result{}, err
	}
	log.Info("Report written locally", "path", localPath)

	// Additionally export to the configured destination (S3, Azure Blob, or PVC).
	if t := ua.Spec.Source.Type; t == miropsv1.SourceTypeS3 || t == miropsv1.SourceTypeBlob || t == miropsv1.SourceTypePVC {
		exp, err := r.buildExporter(ctx, ua)
		if err != nil {
			log.Error(err, "failed to build exporter")
		} else if err := exp.Export(report); err != nil {
			log.Error(err, "failed to export report")
		} else {
			log.Info("Report exported", "type", t, "location", exp.Location())
		}
	}

	// Update CR status
	ua.Status.Decision = report.Decision.Level
	ua.Status.TotalScore = report.Scores.Total
	ua.Status.AIScore = report.Scores.AI.Score
	ua.Status.AIReasoning = report.AIReasoning
	ua.Status.Reason = report.Reason
	ua.Status.ReportPath = localPath
	ua.Status.LastAnalysisTime = &metav1.Time{Time: now}
	ua.Status.LastRefresh = ua.Annotations[refreshAnnotation]
	ua.Status.ObservedGeneration = ua.Generation
	ua.Status.LastTotalPods = snapshot.TotalPods
	ua.Status.LastTotalRestarts = snapshot.TotalRestarts

	if err := r.Client.Status().Update(ctx, ua); err != nil {
		log.Error(err, "failed to update UpgradeAnalysis status")
		return ctrl.Result{}, err
	}

	if interval := ua.Spec.Resync.Interval.Duration; interval > 0 {
		return ctrl.Result{RequeueAfter: interval}, nil
	}
	return ctrl.Result{}, nil
}

// majorMinor parses major and minor from a version string like "v1.30.1" or "1.30".
func majorMinor(version string) (int, int) {
	v := strings.TrimPrefix(version, "v")
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0
	}
	maj, _ := strconv.Atoi(parts[0])
	min, _ := strconv.Atoi(parts[1])
	return maj, min
}

// versionIsHigher returns true if target is strictly greater than current (major.minor only).
func versionIsHigher(target, current string) bool {
	tMaj, tMin := majorMinor(target)
	cMaj, cMin := majorMinor(current)
	if tMaj != cMaj {
		return tMaj > cMaj
	}
	return tMin > cMin
}

// SetupWithManager sets up the controller with the Manager.
func (r *UpgradeAnalysisReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&miropsv1.UpgradeAnalysis{}).
		Complete(r)
}

// compatMatrixConfigMap is the optional ConfigMap (in the analysis namespace) whose
// "matrix.yaml" key overrides the built-in add-on compatibility matrix.
const compatMatrixConfigMap = "mirops-compatibility-matrix"

// refreshAnnotation forces an immediate re-analysis when its value changes, bypassing the
// resync interval. Bump it with: kubectl annotate upgradeanalysis <name> mirops.io/refresh="$(date +%s)" --overwrite
const refreshAnnotation = "mirops.io/refresh"

// loadCompatMatrix returns the built-in matrix overlaid with the optional ConfigMap
// override. A missing ConfigMap is not an error — the built-in defaults are used.
func (r *UpgradeAnalysisReconciler) loadCompatMatrix(ctx context.Context, namespace string) (compat.Matrix, error) {
	cm := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: compatMatrixConfigMap, Namespace: namespace}, cm)
	if err != nil {
		return compat.DefaultMatrix(), nil
	}
	return compat.LoadMatrix([]byte(cm.Data["matrix.yaml"]))
}

// buildExporter selects and configures the right exporter based on source.type.
// When credentialsSecret is set, it reads credentials from the referenced Secret.
func (r *UpgradeAnalysisReconciler) buildExporter(ctx context.Context, ua *miropsv1.UpgradeAnalysis) (exporter.Exporter, error) {
	src := ua.Spec.Source

	// Read optional credentials secret
	var secretData map[string][]byte
	if src.CredentialsSecret != "" {
		secret := &corev1.Secret{}
		if err := r.Client.Get(ctx, types.NamespacedName{
			Name:      src.CredentialsSecret,
			Namespace: r.OperatorNamespace,
		}, secret); err != nil {
			return nil, fmt.Errorf("reading credentials secret %q: %w", src.CredentialsSecret, err)
		}
		secretData = secret.Data
	}

	switch src.Type {
	case miropsv1.SourceTypeS3:
		return &exporter.S3Exporter{
			Bucket:          src.Bucket,
			Region:          src.Region,
			Key:             src.Key,
			AccessKeyID:     string(secretData["AWS_ACCESS_KEY_ID"]),
			SecretAccessKey: string(secretData["AWS_SECRET_ACCESS_KEY"]),
		}, nil
	case miropsv1.SourceTypeBlob:
		return &exporter.BlobExporter{
			AccountName:   src.AccountName,
			ContainerName: src.ContainerName,
			BlobName:      src.BlobName,
			ClientID:      string(secretData["AZURE_CLIENT_ID"]),
			ClientSecret:  string(secretData["AZURE_CLIENT_SECRET"]),
			TenantID:      string(secretData["AZURE_TENANT_ID"]),
		}, nil
	case miropsv1.SourceTypePVC:
		// A PVC destination is a filesystem write to a volume the Helm chart mounts. src.Path is
		// the mount directory; the report lands as <name>.json so multiple analyses don't collide.
		dir := src.Path
		if dir == "" {
			dir = "/mnt/mirops-reports"
		}
		return exporter.NewFileExporter(filepath.Join(dir, ua.Name+".json")), nil
	default:
		return exporter.NewFileExporter(src.Path), nil
	}
}
