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

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	miropsv1 "github.com/miropshq/mirops/api/v1"
	"github.com/miropshq/mirops/internal/collector"
)

// UpgradeAnalysisReconciler reconciles a UpgradeAnalysis object
type UpgradeAnalysisReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mirops.com,resources=upgradeanalyses/finalizers,verbs=update

// Reconcile implements the reconciliation loop for UpgradeAnalysis
func (r *UpgradeAnalysisReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the UpgradeAnalysis instance
	upgradeAnalysis := &miropsv1.UpgradeAnalysis{}
	if err := r.Client.Get(ctx, req.NamespacedName, upgradeAnalysis); err != nil {
		log.Error(err, "unable to fetch UpgradeAnalysis")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling UpgradeAnalysis", "name", upgradeAnalysis.Name, "namespace", upgradeAnalysis.Namespace)

	clusterCollector := collector.NewClusterCollector(r.Client)
	snapshot, err := clusterCollector.Collect(ctx)
	if err != nil {
		log.Error(err, "failed to collect cluster snapshot")
		return ctrl.Result{}, err
	}

	log.Info("Cluster snapshot collected",
		"clusterVersion", snapshot.ClusterVersion,
		"namespaceCount", snapshot.NamespaceCount,
		"resourceCount", len(snapshot.Resources),
	)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *UpgradeAnalysisReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&miropsv1.UpgradeAnalysis{}).
		Complete(r)
}
