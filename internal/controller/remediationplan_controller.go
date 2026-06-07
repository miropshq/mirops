package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	miropsv1 "github.com/miropshq/mirops/api/v1"
)

// RemediationPlanReconciler executes remediation actions when approved
type RemediationPlanReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=mirops.mirops.io,resources=remediationplans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mirops.mirops.io,resources=remediationplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;patch;update

func (r *RemediationPlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	plan := &miropsv1.RemediationPlan{}
	if err := r.Client.Get(ctx, req.NamespacedName, plan); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Only execute when approved and not already running/completed
	if !plan.Spec.Approved {
		return ctrl.Result{}, nil
	}
	if plan.Status.Phase == miropsv1.RemediationPhaseCompleted ||
		plan.Status.Phase == miropsv1.RemediationPhaseFailed ||
		plan.Status.Phase == miropsv1.RemediationPhaseRunning {
		return ctrl.Result{}, nil
	}

	log.Info("Executing RemediationPlan", "name", plan.Name, "actions", len(plan.Spec.Actions))

	now := metav1.Now()
	plan.Status.Phase = miropsv1.RemediationPhaseRunning
	plan.Status.ExecutedAt = &now
	if err := r.Client.Status().Update(ctx, plan); err != nil {
		return ctrl.Result{}, err
	}

	results := make([]miropsv1.ActionResult, 0, len(plan.Spec.Actions))
	allOK := true

	for _, action := range plan.Spec.Actions {
		if action.Skip {
			log.Info("Action skipped by user", "id", action.ID, "type", action.Type)
			results = append(results, miropsv1.ActionResult{ID: action.ID, Status: "skipped"})
			continue
		}
		result := r.executeAction(ctx, plan.Namespace, action)
		results = append(results, result)
		if result.Status == "failed" {
			allOK = false
		}
		log.Info("Action executed",
			"id", action.ID,
			"type", action.Type,
			"status", result.Status,
		)
	}

	completedAt := metav1.Now()
	plan.Status.Results = results
	plan.Status.CompletedAt = &completedAt
	if allOK {
		plan.Status.Phase = miropsv1.RemediationPhaseCompleted
	} else {
		plan.Status.Phase = miropsv1.RemediationPhaseFailed
	}

	if err := r.Client.Status().Update(ctx, plan); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("RemediationPlan finished", "phase", plan.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *RemediationPlanReconciler) executeAction(ctx context.Context, namespace string, action miropsv1.RemediationAction) miropsv1.ActionResult {
	now := metav1.Now()
	result := miropsv1.ActionResult{ID: action.ID, ExecutedAt: &now}

	ns := action.Namespace
	if ns == "" {
		ns = namespace
	}

	var err error
	switch action.Type {
	case miropsv1.ActionTypeRestartPod:
		err = r.restartPod(ctx, ns, action.Name)
	case miropsv1.ActionTypeDeletePod:
		err = r.deletePod(ctx, ns, action.Name)
	case miropsv1.ActionTypeScaleDeployment:
		err = r.scaleDeployment(ctx, ns, action.Name, action.Params)
	case miropsv1.ActionTypeCordonNode:
		err = r.cordonNode(ctx, action.Name)
	default:
		err = fmt.Errorf("unknown action type %q", action.Type)
	}

	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
	} else {
		result.Status = "success"
	}
	return result
}

func (r *RemediationPlanReconciler) restartPod(ctx context.Context, namespace, name string) error {
	pod := &corev1.Pod{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
		return err
	}
	return r.Client.Delete(ctx, pod)
}

func (r *RemediationPlanReconciler) deletePod(ctx context.Context, namespace, name string) error {
	pod := &corev1.Pod{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
		return err
	}
	grace := int64(0)
	return r.Client.Delete(ctx, pod, &client.DeleteOptions{GracePeriodSeconds: &grace})
}

func (r *RemediationPlanReconciler) scaleDeployment(ctx context.Context, namespace, name string, params map[string]string) error {
	dep := &appsv1.Deployment{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep); err != nil {
		return err
	}
	replicas := int32(1)
	if v, ok := params["replicas"]; ok {
		if _, err := fmt.Sscanf(v, "%d", &replicas); err != nil {
			return fmt.Errorf("invalid replicas value %q: %w", v, err)
		}
	}
	dep.Spec.Replicas = &replicas
	return r.Client.Update(ctx, dep)
}

func (r *RemediationPlanReconciler) cordonNode(ctx context.Context, name string) error {
	node := &corev1.Node{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: name}, node); err != nil {
		return err
	}
	node.Spec.Unschedulable = true
	return r.Client.Update(ctx, node)
}

func (r *RemediationPlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&miropsv1.RemediationPlan{}).
		Complete(r)
}

// createRemediationPlan is called by UpgradeAnalysisReconciler to create a plan from AI actions.
func (r *UpgradeAnalysisReconciler) createRemediationPlan(ctx context.Context, ua *miropsv1.UpgradeAnalysis, actions []aiActionEntry) error {
	log := logf.FromContext(ctx)

	// Filter actions by maxRiskLevel
	maxRisk := ua.Spec.AI.Remediation.MaxRiskLevel
	if maxRisk == "" {
		maxRisk = miropsv1.RiskLevelLow
	}

	planActions := make([]miropsv1.RemediationAction, 0, len(actions))
	for i, a := range actions {
		risk := miropsv1.RiskLevel(a.Risk)
		if !riskWithin(risk, maxRisk) {
			log.Info("Skipping action above maxRiskLevel", "type", a.Type, "risk", a.Risk)
			continue
		}
		planActions = append(planActions, miropsv1.RemediationAction{
			ID:        fmt.Sprintf("action-%d", i+1),
			Type:      miropsv1.ActionType(a.Type),
			Namespace: a.Namespace,
			Name:      a.Name,
			Reason:    a.Reason,
			Risk:      risk,
			Params:    a.Params,
		})
	}

	if len(planActions) == 0 {
		return nil
	}

	plan := &miropsv1.RemediationPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ua.Name + "-remediation",
			Namespace: ua.Namespace,
			Labels:    map[string]string{"mirops.io/analysis": ua.Name},
		},
		Spec: miropsv1.RemediationPlanSpec{
			UpgradeAnalysisRef: ua.Name,
			Approved:           ua.Spec.AI.Remediation.AutoApprove,
			Actions:            planActions,
		},
	}

	existing := &miropsv1.RemediationPlan{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: plan.Name, Namespace: plan.Namespace}, existing)
	if err == nil {
		// Already exists — only replace if previous one is completed or failed
		if existing.Status.Phase == miropsv1.RemediationPhaseCompleted ||
			existing.Status.Phase == miropsv1.RemediationPhaseFailed ||
			existing.Status.Phase == "" {
			existing.Spec = plan.Spec
			existing.Spec.Approved = false
			existing.Status = miropsv1.RemediationPlanStatus{}
			return r.Client.Update(ctx, existing)
		}
		return nil
	}

	now := time.Now()
	plan.Status.Phase = miropsv1.RemediationPhasePendingApproval
	_ = now
	log.Info("Creating RemediationPlan", "name", plan.Name, "actions", len(planActions))
	return r.Client.Create(ctx, plan)
}

// riskWithin returns true if risk is less than or equal to max.
func riskWithin(risk, max miropsv1.RiskLevel) bool {
	order := map[miropsv1.RiskLevel]int{
		miropsv1.RiskLevelLow:    1,
		miropsv1.RiskLevelMedium: 2,
		miropsv1.RiskLevelHigh:   3,
	}
	return order[risk] <= order[max]
}
