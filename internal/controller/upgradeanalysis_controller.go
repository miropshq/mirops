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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

// Reconcile implements the reconciliation loop for UpgradeAnalysis
func (r *UpgradeAnalysisReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	ua := &miropsv1.UpgradeAnalysis{}
	if err := r.Client.Get(ctx, req.NamespacedName, ua); err != nil {
		log.Error(err, "unable to fetch UpgradeAnalysis")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling UpgradeAnalysis", "name", ua.Name, "namespace", ua.Namespace)

	// Collect cluster snapshot
	snapshot, err := r.Collector.Collect(ctx)
	if err != nil {
		log.Error(err, "failed to collect cluster snapshot")
		return ctrl.Result{}, err
	}

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
		"totalScore", report.Scores.Total,
		"reason", report.Reason,
	)

	// Export report to configured source (default: file)
	sourcePath := ua.Spec.Source.Path
	exp := exporter.NewFileExporter(sourcePath)
	if err := exp.Export(report); err != nil {
		log.Error(err, "failed to export analysis report")
		return ctrl.Result{}, err
	}

	log.Info("Report written", "path", exp.Path)

	// Update CR status
	ua.Status.Decision = report.Decision.Level
	ua.Status.TotalScore = report.Scores.Total
	ua.Status.Reason = report.Reason
	ua.Status.ReportPath = exp.Path
	ua.Status.LastAnalysisTime = &metav1.Time{Time: now}

	if err := r.Client.Status().Update(ctx, ua); err != nil {
		log.Error(err, "failed to update UpgradeAnalysis status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *UpgradeAnalysisReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&miropsv1.UpgradeAnalysis{}).
		Complete(r)
}

