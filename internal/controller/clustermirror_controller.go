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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	miropsv1 "github.com/miropshq/mirops/api/v1"
	"github.com/miropshq/mirops/internal/analysis"
	"github.com/miropshq/mirops/internal/collector"
	"github.com/miropshq/mirops/internal/graph"
)

// defaultMirrorInterval is the rebuild cadence used when spec.refresh.interval is unset or
// non-positive. The CRD also defaults it to 5m at admission; this guards direct/API creation.
const defaultMirrorInterval = 5 * time.Minute

// mirrorReportExt names the mirror report on the reports server (GET /reports/<name>.mirror). It
// differs from the UpgradeAnalysis ".mirops" extension so a mirror and an analysis that share a name
// never overwrite each other.
const mirrorReportExt = ".mirror"

// ClusterMirrorReconciler keeps the long-lived cluster mirror up to date. Unlike
// UpgradeAnalysisReconciler it carries no target version and runs no compatibility check or AI:
// it rebuilds the component graph on an interval and reports the cluster's *current* operational
// risk (down workloads, lost/pending PVCs, not-ready nodes propagated along dependency edges).
type ClusterMirrorReconciler struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	Collector  collector.ClusterCollector
	ReportsDir string
	// UpgradeEnabled mirrors MIROPS_UPGRADE_ENABLED (Helm upgrade.enabled). The mirror report
	// carries it, so consumers learn upgrade analysis is off without a missing report to guess from.
	UpgradeEnabled bool
}

// +kubebuilder:rbac:groups=mirops.mirops.io,resources=clustermirrors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mirops.mirops.io,resources=clustermirrors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mirops.mirops.io,resources=clustermirrors/finalizers,verbs=update

// Reconcile rebuilds the mirror for one ClusterMirror, publishes its report and requeues after the
// refresh interval.
func (r *ClusterMirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cm := &miropsv1.ClusterMirror{}
	if err := r.Client.Get(ctx, req.NamespacedName, cm); err != nil {
		// NotFound is normal — the CR was deleted; nothing to reconcile.
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch ClusterMirror")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	interval := cm.Spec.Refresh.Interval.Duration
	if interval <= 0 {
		interval = defaultMirrorInterval
	}

	scope := collector.Scope{
		Mode:              string(cm.Spec.Scope.Mode),
		ExcludeNamespaces: cm.Spec.Scope.ExcludeNamespaces,
	}
	if scope.Mode == "" {
		scope.Mode = "all"
	}

	snapshot, err := r.Collector.Collect(ctx, scope)
	if err != nil {
		log.Error(err, "failed to collect cluster snapshot for mirror")
		cm.Status.SyncError = err.Error()
		if uerr := r.Client.Status().Update(ctx, cm); uerr != nil {
			log.Error(uerr, "failed to update ClusterMirror status after collect error")
		}
		// Retry on the normal cadence rather than a tight error loop.
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	// Build the logical graph and propagate risk. addonStatus is nil: with no target version the
	// mirror does not evaluate add-on compatibility, so risk comes only from current cluster state.
	g := graph.BuildFromSnapshot(snapshot)
	nsRisk := g.ApplyRisk(nil)

	now := time.Now().UTC()
	report := analysis.BuildMirrorReport(snapshot, g, nsRisk)
	report.GeneratedAt = now.Format(time.RFC3339)
	report.Mirror = cm.Name
	report.Upgrade = r.upgradeSummary(ctx)

	// The status is derived from the report so the CR and the published report never disagree.
	byNS := make([]miropsv1.NamespaceMirrorStatus, 0, len(nsRisk))
	for _, nr := range nsRisk {
		byNS = append(byNS, miropsv1.NamespaceMirrorStatus{
			Namespace: nr.Namespace,
			MaxRisk:   nr.Risk,
			AtRisk:    nr.AtRisk,
		})
	}
	cm.Status.Components = report.Summary.Components
	cm.Status.Edges = report.Summary.Edges
	cm.Status.AtRisk = report.Summary.AtRisk
	cm.Status.ByNamespace = byNS
	cm.Status.LastSync = &metav1.Time{Time: now}
	cm.Status.SyncError = ""
	cm.Status.ObservedGeneration = cm.Generation

	path := filepath.Join(r.ReportsDir, cm.Name+mirrorReportExt)
	if err := writeMirrorReport(path, report); err != nil {
		log.Error(err, "failed to write mirror report", "path", path)
		cm.Status.SyncError = fmt.Sprintf("mirror rebuilt but its report could not be written: %v", err)
	}

	if err := r.Client.Status().Update(ctx, cm); err != nil {
		log.Error(err, "failed to update ClusterMirror status")
		return ctrl.Result{}, err
	}

	log.Info("Cluster mirror rebuilt",
		"name", cm.Name,
		"components", cm.Status.Components,
		"edges", cm.Status.Edges,
		"atRisk", cm.Status.AtRisk,
		"upgradeEnabled", report.Upgrade.Enabled,
		"interval", interval,
	)

	return ctrl.Result{RequeueAfter: interval}, nil
}

// upgradeSummary reports whether upgrade analysis runs in this install and, when it does, one line
// per UpgradeAnalysis (read from their status) so a consumer can pick one and fetch its report.
func (r *ClusterMirrorReconciler) upgradeSummary(ctx context.Context) analysis.MirrorUpgrade {
	if !r.UpgradeEnabled {
		return analysis.MirrorUpgrade{Enabled: false}
	}
	list := &miropsv1.UpgradeAnalysisList{}
	if err := r.Client.List(ctx, list); err != nil {
		logf.FromContext(ctx).Error(err, "failed to list UpgradeAnalyses for the mirror report")
		return analysis.MirrorUpgrade{Enabled: true, Error: fmt.Sprintf("could not list upgrade analyses: %v", err)}
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })

	out := analysis.MirrorUpgrade{Enabled: true}
	for i := range list.Items {
		ua := &list.Items[i]
		s := analysis.UpgradeAnalysisSummary{
			Name:          ua.Name,
			TargetVersion: ua.Spec.TargetVersion,
			Decision:      ua.Status.Decision,
			Score:         ua.Status.TotalScore,
			Report:        reportFileName(ua),
		}
		if ua.Status.LastAnalysisTime != nil {
			s.LastAnalysisTime = ua.Status.LastAnalysisTime.UTC().Format(time.RFC3339)
		}
		out.Analyses = append(out.Analyses, s)
	}
	return out
}

// writeMirrorReport writes the report through a temp file and a rename, so the reports server (which
// Headlamp polls) never serves a half-written file.
func writeMirrorReport(path string, report *analysis.MirrorReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling mirror report: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing mirror report: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("publishing mirror report: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterMirrorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&miropsv1.ClusterMirror{}).
		// Rebuild on spec changes and on the mirops.io/refresh annotation ("Refresh now"), but not
		// on our own Status().Update — the periodic rebuild is driven by RequeueAfter, so a status
		// write must not retrigger a fresh collect.
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)).
		Complete(r)
}
