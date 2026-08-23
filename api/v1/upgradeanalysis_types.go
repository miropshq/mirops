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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AIProvider identifies which AI backend to use for scoring
// +kubebuilder:validation:Enum=anthropic;openai
type AIProvider string

const (
	AIProviderAnthropic AIProvider = "anthropic"
	AIProviderOpenAI    AIProvider = "openai"
)

// RemediationConfig controls whether the AI can propose remediation actions
type RemediationConfig struct {
	// enabled allows the AI to propose remediation actions after scoring.
	// When true, a RemediationPlan CR is created with the proposed actions.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// maxRiskLevel is the highest risk level of actions the AI may propose.
	// Actions above this level are excluded from the plan.
	// +kubebuilder:default=low
	// +kubebuilder:validation:Enum=low;medium;high
	MaxRiskLevel RiskLevel `json:"maxRiskLevel,omitempty"`

	// autoApprove executes all proposed actions immediately without manual approval.
	// When false (default) the plan waits for spec.approved=true before executing.
	// +kubebuilder:default=false
	AutoApprove bool `json:"autoApprove,omitempty"`
}

// AIConfig enables AI-assisted scoring. When enabled, the final score is
// baseScore*0.7 + aiScore*0.3 instead of baseScore*1.0.
type AIConfig struct {
	// enabled activates AI scoring (base 70% + AI 30%)
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// provider is the AI backend to use
	// +kubebuilder:validation:Enum=anthropic;openai
	// +optional
	Provider AIProvider `json:"provider,omitempty"`

	// model is the model name to call (e.g. claude-sonnet-4-6)
	// +optional
	Model string `json:"model,omitempty"`

	// maxTokens is the maximum number of tokens the model may generate in its response. Applies to
	// both Anthropic and OpenAI. Higher allows a longer AI explanation but costs more; defaults to 2048.
	// +kubebuilder:default=2048
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=32768
	// +optional
	MaxTokens int32 `json:"maxTokens,omitempty"`

	// credentialsSecret is a Secret in the same namespace containing the API key.
	// Key name: ANTHROPIC_API_KEY or OPENAI_API_KEY
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`

	// remediation configures optional AI-proposed remediation actions.
	// +optional
	Remediation RemediationConfig `json:"remediation,omitempty"`
}

// ResyncConfig controls how often the analysis is re-run automatically.
type ResyncConfig struct {
	// interval is how often to re-run the analysis (e.g. "15m", "1h").
	// When empty, the analysis runs once and stops.
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`
}

// SourceType defines where the analysis report will be written
// +kubebuilder:validation:Enum=file;s3;blob;pvc
type SourceType string

const (
	SourceTypeFile SourceType = "file"
	SourceTypeS3   SourceType = "s3"
	SourceTypeBlob SourceType = "blob"
	// SourceTypePVC writes the report to a PersistentVolumeClaim mounted into the operator,
	// for on-premises clusters without cloud object storage. The PVC is mounted via the Helm
	// chart; source.path is the directory on that volume (report is written as <name>.mirops).
	SourceTypePVC SourceType = "pvc"
)

// ScopeMode defines which namespaces are included in the analysis
// +kubebuilder:validation:Enum=all;application
type ScopeMode string

const (
	// ScopeModeAll includes every namespace (system + application)
	ScopeModeAll ScopeMode = "all"
	// ScopeModeApplication includes only non-system namespaces
	ScopeModeApplication ScopeMode = "application"
)

// ScopeConfig controls which namespaces are analysed
type ScopeConfig struct {
	// mode controls which namespaces are included.
	// "all" includes system and application namespaces (default).
	// "application" excludes kube-system and other system namespaces.
	// +kubebuilder:default=all
	Mode ScopeMode `json:"mode"`

	// excludeNamespaces is an optional list of additional namespaces to skip.
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty"`
}

// SourceConfig defines the output destination for the analysis report
type SourceConfig struct {
	// type is the storage backend for the report: file, s3, blob, or pvc
	// +kubebuilder:default=file
	Type SourceType `json:"type"`

	// path is the report file name when type is "file" (the directory is fixed at the operator's
	// reports dir; only the basename is used). Defaults to "<name>.mirops". When type is "pvc" it is
	// the directory on the mounted PersistentVolumeClaim (the report is written as <name>.mirops there).
	// +optional
	Path string `json:"path,omitempty"`

	// bucket is the S3 bucket name when type is "s3"
	// +optional
	Bucket string `json:"bucket,omitempty"`

	// region is the AWS region when type is "s3"
	// +optional
	Region string `json:"region,omitempty"`

	// key is the S3 object key (path inside bucket) when type is "s3"
	// +optional
	Key string `json:"key,omitempty"`

	// accountName is the Azure Storage account name when type is "blob"
	// +optional
	AccountName string `json:"accountName,omitempty"`

	// containerName is the Azure Blob container name when type is "blob"
	// +optional
	ContainerName string `json:"containerName,omitempty"`

	// blobName is the Azure Blob object name when type is "blob"
	// +optional
	BlobName string `json:"blobName,omitempty"`

	// credentialsSecret is the name of a Kubernetes Secret in the same namespace
	// containing cloud credentials. Optional when using IRSA (AWS) or Workload Identity (Azure).
	// For S3: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
	// For Blob: AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, AZURE_TENANT_ID
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

// UpgradeAnalysisSpec defines the desired state of UpgradeAnalysis
type UpgradeAnalysisSpec struct {
	// targetVersion is the Kubernetes version to upgrade to (e.g. "1.29")
	// +required
	TargetVersion string `json:"targetVersion"`

	// scoringProfile selects how strict the upgrade-readiness scoring is.
	// "production" (default) is strict; "non-production" is lenient.
	// +kubebuilder:validation:Enum=production;non-production
	// +kubebuilder:default=production
	// +optional
	ScoringProfile string `json:"scoringProfile,omitempty"`

	// scope controls which namespaces are included in the analysis.
	// Defaults to "all" (system + application namespaces).
	// +optional
	Scope ScopeConfig `json:"scope,omitempty"`

	// source defines where the analysis report will be written
	// +optional
	Source SourceConfig `json:"source,omitempty"`

	// ai configures optional AI-assisted scoring.
	// When enabled the final score becomes baseScore*0.7 + aiScore*0.3.
	// +optional
	AI AIConfig `json:"ai,omitempty"`

	// resync controls automatic re-analysis on an interval.
	// When omitted the analysis runs once per reconcile.
	// +optional
	Resync ResyncConfig `json:"resync,omitempty"`
}

// UpgradeAnalysisStatus defines the observed state of UpgradeAnalysis.
type UpgradeAnalysisStatus struct {
	// conditions represent the current state of the UpgradeAnalysis resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// decision is the result of the analysis: SAFE, WARNING, or CRITICAL
	// +optional
	Decision string `json:"decision,omitempty"`

	// totalScore is the computed upgrade readiness score (0-100)
	// +optional
	TotalScore int `json:"totalScore,omitempty"`

	// reason is a human-readable explanation of the decision
	// +optional
	Reason string `json:"reason,omitempty"`

	// lastAnalysisTime is when the last analysis was performed
	// +optional
	LastAnalysisTime *metav1.Time `json:"lastAnalysisTime,omitempty"`

	// lastRefresh is the value of the mirops.io/refresh annotation honored by the last analysis.
	// Bumping that annotation forces an immediate re-analysis even within the resync interval.
	// +optional
	LastRefresh string `json:"lastRefresh,omitempty"`

	// observedGeneration is the spec generation the last analysis ran against. A spec change
	// re-runs the analysis immediately, even within the resync interval.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// reportPath is where the JSON report was written (local file destination only)
	// +optional
	ReportPath string `json:"reportPath,omitempty"`

	// reportState is the outcome of persisting the report: "written" or "failed".
	// +optional
	// +kubebuilder:validation:Enum=written;failed
	ReportState string `json:"reportState,omitempty"`

	// reportError is the error message when the report could not be written to (or read back from)
	// its destination — e.g. an S3/Blob/PVC connection failure. Empty on success. Surfaced to the
	// UI so a remote-storage failure isn't hidden in the pod logs.
	// +optional
	ReportError string `json:"reportError,omitempty"`

	// reportLocation is where the report was written: a local path, or an s3://, blob, or pvc URI.
	// +optional
	ReportLocation string `json:"reportLocation,omitempty"`

	// aiScore is the score returned by the AI model (0-100), 0 when AI is disabled
	// +optional
	AIScore int `json:"aiScore,omitempty"`

	// aiReasoning is the explanation provided by the AI model, only set when ai.enabled is true
	// +optional
	AIReasoning string `json:"aiReasoning,omitempty"`

	// aiModel is the model used for AI scoring, only set when ai.enabled is true
	// +optional
	AIModel string `json:"aiModel,omitempty"`

	// aiError holds the error message if AI scoring was enabled but failed.
	// Empty when AI scoring is disabled or succeeded.
	// +optional
	AIError string `json:"aiError,omitempty"`

	// addonsChecked is the number of cluster add-ons evaluated for compatibility.
	// +optional
	AddonsChecked int `json:"addonsChecked,omitempty"`

	// incompatibleAddons is the number of add-ons found incompatible with targetVersion.
	// +optional
	IncompatibleAddons int `json:"incompatibleAddons,omitempty"`

	// lastTotalPods is the pod count from the previous reconciliation, used to compute stability delta
	// +optional
	LastTotalPods int `json:"lastTotalPods,omitempty"`

	// lastTotalRestarts is the restart count from the previous reconciliation, used to compute stability delta
	// +optional
	LastTotalRestarts int `json:"lastTotalRestarts,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target Version",type=string,JSONPath=`.spec.targetVersion`
// +kubebuilder:printcolumn:name="Decision",type=string,JSONPath=`.status.decision`
// +kubebuilder:printcolumn:name="Score",type=integer,JSONPath=`.status.totalScore`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UpgradeAnalysis is the Schema for the upgradeanalyses API
type UpgradeAnalysis struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UpgradeAnalysis
	// +required
	Spec UpgradeAnalysisSpec `json:"spec"`

	// status defines the observed state of UpgradeAnalysis
	// +optional
	Status UpgradeAnalysisStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UpgradeAnalysisList contains a list of UpgradeAnalysis
type UpgradeAnalysisList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UpgradeAnalysis `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UpgradeAnalysis{}, &UpgradeAnalysisList{})
}
