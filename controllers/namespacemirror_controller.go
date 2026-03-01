/*
Copyright 2025.

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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	mirrorv1 "github.com/liveim/liveim/api/v1"
	"github.com/liveim/liveim/internal/mirror"
)

// NamespaceMirrorReconciler reconciles a NamespaceMirror object
type NamespaceMirrorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Mirror *mirror.NamespaceMirror
}

type SyncCounts struct {
	Secrets     int
	ConfigMaps  int
	Deployments int
	Services    int
	ReplicaSets int
	Pods        int
}

// +kubebuilder:rbac:groups=mirror.mirror.dev,resources=namespacemirrors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mirror.mirror.dev,resources=namespacemirrors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mirror.mirror.dev,resources=namespacemirrors/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NamespaceMirror object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *NamespaceMirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// TODO(user): your logic here
	nm := &mirrorv1.NamespaceMirror{}
	if err := r.Get(ctx, req.NamespacedName, nm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	src := nm.Spec.SourceNamespace
	dst := nm.Spec.TargetNamespace

	logger.Info("Mirroring namespace", "source", src, "target", dst)

	// →→→ Your existing code gets reused here
	// if err := r.mirrorEverything(ctx, src, dst); err != nil {
	// 	logger.Error(err, "mirror failed")
	// 	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	// }

	counts, err := r.mirrorEverything(ctx, src, dst)
	if err != nil {
		logger.Error(err, "mirror failed")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	nm.Status.LastSyncTime = time.Now().Format(time.RFC3339)
	nm.Status.SecretsSynced = counts.Secrets
	nm.Status.ConfigMapsSynced = counts.ConfigMaps
	nm.Status.DeploymentsSynced = counts.Deployments
	nm.Status.ServicesSynced = counts.Services
	nm.Status.ReplicaSetsSynced = counts.ReplicaSets
	nm.Status.PodsSynced = counts.Pods

	// _ = r.Status().Update(ctx, nm)

	if err := r.Status().Update(ctx, nm); err != nil {
		logger.Error(err, "Failed updating status")
	}

	// repeat mirror every 30 seconds
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil

	//return ctrl.Result{}, nil
}

// wrap all your mirror logic in this function
func (r *NamespaceMirrorReconciler) mirrorEverything(ctx context.Context, src, dst string) (*SyncCounts, error) {

	counts := &SyncCounts{}

	// Secrets
	secrets, _ := r.Mirror.SecretMirror.ListSecrets(ctx, src)
	_ = r.Mirror.MirrorNamespaceSecrets(ctx, src, dst, secrets)
	counts.Secrets = len(secrets)

	// ConfigMaps
	cms, _ := r.Mirror.ConfigMapMirror.ListConfigMaps(ctx, src)
	_ = r.Mirror.MirrorNamespaceConfigMaps(ctx, src, dst, cms)
	counts.ConfigMaps = len(cms)

	// Deployments
	deps, _ := r.Mirror.DeploymentMirror.ListDeployments(ctx, src)
	_ = r.Mirror.MirrorNamespaceDeployments(ctx, src, dst, deps)
	counts.Deployments = len(deps)

	// Services
	svcs, _ := r.Mirror.ServiceMirror.ListServices(ctx, src)
	_ = r.Mirror.MirrorNamespaceServices(ctx, src, dst, svcs)
	counts.Services = len(svcs)

	// ReplicaSets
	rss, _ := r.Mirror.ReplicaSetMirror.ListReplicaSets(ctx, src)
	_ = r.Mirror.MirrorNamespaceReplicaSets(ctx, src, dst, rss)
	counts.ReplicaSets = len(rss)

	// Pods
	pods, _ := r.Mirror.PodMirror.ListPods(ctx, src)
	_ = r.Mirror.MirrorNamespacePods(ctx, src, dst, pods)
	counts.Pods = len(pods)

	return counts, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceMirrorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()

	c := mgr.GetClient()

	r.Mirror = mirror.NewNamespaceMirror(
		mirror.NewSecretMirror(c),
		mirror.NewConfigMapMirror(c),
		mirror.NewDeploymentMirror(c),
		mirror.NewServiceMirror(c),
		mirror.NewReplicaSetMirror(c),
		mirror.NewPodMirror(c),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&mirrorv1.NamespaceMirror{}).
		Named("namespacemirror").
		Complete(r)
}
