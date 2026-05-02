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

// SourceType defines where the analysis report will be written
// +kubebuilder:validation:Enum=file;s3;blob
type SourceType string

const (
	SourceTypeFile SourceType = "file"
	SourceTypeS3   SourceType = "s3"
	SourceTypeBlob SourceType = "blob"
)

// SourceConfig defines the output destination for the analysis report
type SourceConfig struct {
	// type is the storage backend for the report: file, s3, or blob
	// +kubebuilder:default=file
	Type SourceType `json:"type"`

	// path is the file path when type is "file"
	// +optional
	Path string `json:"path,omitempty"`
}

// UpgradeAnalysisSpec defines the desired state of UpgradeAnalysis
type UpgradeAnalysisSpec struct {
	// targetVersion is the Kubernetes version to upgrade to (e.g. "1.29")
	// +required
	TargetVersion string `json:"targetVersion"`

	// source defines where the analysis report will be written
	// +optional
	Source SourceConfig `json:"source,omitempty"`
}

// UpgradeAnalysisStatus defines the observed state of UpgradeAnalysis.
type UpgradeAnalysisStatus struct {
	// conditions represent the current state of the UpgradeAnalysis resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// decision is the result of the analysis: SAFE, WARNING, or BLOCK
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

	// reportPath is where the JSON report was written
	// +optional
	ReportPath string `json:"reportPath,omitempty"`
}

// +kubebuilder:object:root=true
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
