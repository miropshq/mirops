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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ActionType defines what kind of remediation action to perform
// +kubebuilder:validation:Enum=restart-pod;scale-deployment;cordon-node;delete-pod
type ActionType string

const (
	ActionTypeRestartPod      ActionType = "restart-pod"
	ActionTypeScaleDeployment ActionType = "scale-deployment"
	ActionTypeCordonNode      ActionType = "cordon-node"
	ActionTypeDeletePod       ActionType = "delete-pod"
)

// RiskLevel describes the risk of a remediation action
// +kubebuilder:validation:Enum=low;medium;high
type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

// RemediationPhase describes the current phase of the plan
// +kubebuilder:validation:Enum=pending-approval;running;completed;failed
type RemediationPhase string

const (
	RemediationPhasePendingApproval RemediationPhase = "pending-approval"
	RemediationPhaseRunning         RemediationPhase = "running"
	RemediationPhaseCompleted       RemediationPhase = "completed"
	RemediationPhaseFailed          RemediationPhase = "failed"
)

// RemediationAction describes a single action proposed by the AI
type RemediationAction struct {
	// id is a unique identifier for this action
	ID string `json:"id"`

	// type is the kind of action to perform
	Type ActionType `json:"type"`

	// namespace is the namespace of the target resource
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// name is the name of the target resource
	Name string `json:"name"`

	// reason is the AI explanation for why this action is needed
	Reason string `json:"reason"`

	// risk is the assessed risk level of this action
	// +kubebuilder:default=low
	Risk RiskLevel `json:"risk"`

	// params holds type-specific parameters (e.g. replicas for scale-deployment)
	// +optional
	Params map[string]string `json:"params,omitempty"`
}

// ActionResult records the outcome of an executed action
type ActionResult struct {
	// id matches the RemediationAction ID
	ID string `json:"id"`

	// status is success, failed, or skipped
	Status string `json:"status"`

	// error holds the error message if status is failed
	// +optional
	Error string `json:"error,omitempty"`

	// executedAt is when the action was attempted
	// +optional
	ExecutedAt *metav1.Time `json:"executedAt,omitempty"`
}

// RemediationPlanSpec defines the desired state of RemediationPlan
type RemediationPlanSpec struct {
	// upgradeAnalysisRef is the name of the UpgradeAnalysis that generated this plan
	UpgradeAnalysisRef string `json:"upgradeAnalysisRef"`

	// approved must be set to true by the user to allow execution
	// +kubebuilder:default=false
	Approved bool `json:"approved"`

	// actions is the list of remediation actions proposed by the AI
	Actions []RemediationAction `json:"actions"`
}

// RemediationPlanStatus defines the observed state of RemediationPlan
type RemediationPlanStatus struct {
	// phase is the current state of the plan
	// +optional
	Phase RemediationPhase `json:"phase,omitempty"`

	// results holds the outcome of each executed action
	// +optional
	Results []ActionResult `json:"results,omitempty"`

	// executedAt is when execution started
	// +optional
	ExecutedAt *metav1.Time `json:"executedAt,omitempty"`

	// completedAt is when all actions finished
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Analysis",type=string,JSONPath=`.spec.upgradeAnalysisRef`
// +kubebuilder:printcolumn:name="Approved",type=boolean,JSONPath=`.spec.approved`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// RemediationPlan is created by the operator when AI proposes fixes.
// Set spec.approved to true to execute the actions.
type RemediationPlan struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec RemediationPlanSpec `json:"spec"`

	// +optional
	Status RemediationPlanStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// RemediationPlanList contains a list of RemediationPlan
type RemediationPlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemediationPlan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemediationPlan{}, &RemediationPlanList{})
}
