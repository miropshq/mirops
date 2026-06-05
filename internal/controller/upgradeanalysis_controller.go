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
	"github.com/miropshq/mirops/internal/exporter"
)

// UpgradeAnalysisReconciler reconciles a UpgradeAnalysis object
type UpgradeAnalysisReconciler struct {
	Client    client.Client
	Scheme    *runtime.Scheme
	Collector collector.ClusterCollector
}

// +kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
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

	log.Info("Reconciling UpgradeAnalysis", "name", ua.Name, "namespace", ua.Namespace)

	// Skip if the last analysis ran within the resync interval to avoid
	// calling the AI API on every status update.
	if interval := ua.Spec.Resync.Interval.Duration; interval > 0 && ua.Status.LastAnalysisTime != nil {
		next := ua.Status.LastAnalysisTime.Add(interval)
		if time.Now().Before(next) {
			return ctrl.Result{RequeueAfter: time.Until(next)}, nil
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

	// Calculate upgrade readiness score
	report := analysis.Calculate(snapshot, ua.Spec.TargetVersion)
	now := time.Now().UTC()
	report.GeneratedAt = now.Format(time.RFC3339)
	report.Cluster = ua.Name

	log.Info("Analysis completed",
		"decision", report.Decision.Level,
		"baseScore", report.Scores.Base,
		"reason", report.Reason,
	)

	// Apply AI score if enabled (base*0.7 + ai*0.3)
	if ua.Spec.AI.Enabled {
		aiScore, reasoning, err := r.scoreWithAI(ctx, ua, report)
		if err != nil {
			log.Error(err, "AI scoring failed, proceeding with base score only")
		} else {
			analysis.ApplyAIScore(report, aiScore, reasoning)
			log.Info("AI score applied",
				"aiScore", aiScore,
				"totalScore", report.Scores.Total,
			)
		}
	}

	// Export report to configured source (default: file)
	exp, err := r.buildExporter(ctx, ua)
	if err != nil {
		log.Error(err, "failed to build exporter")
		return ctrl.Result{}, err
	}
	if err := exp.Export(report); err != nil {
		log.Error(err, "failed to export analysis report")
		return ctrl.Result{}, err
	}

	log.Info("Report written", "location", exp.Location())

	// Update CR status
	ua.Status.Decision = report.Decision.Level
	ua.Status.TotalScore = report.Scores.Total
	ua.Status.AIScore = report.Scores.AI
	ua.Status.AIReasoning = report.AIReasoning
	ua.Status.Reason = report.Reason
	ua.Status.ReportPath = exp.Location()
	ua.Status.LastAnalysisTime = &metav1.Time{Time: now}
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

// SetupWithManager sets up the controller with the Manager.
func (r *UpgradeAnalysisReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&miropsv1.UpgradeAnalysis{}).
		Complete(r)
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
			Namespace: ua.Namespace,
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
	default:
		return exporter.NewFileExporter(src.Path), nil
	}
}
