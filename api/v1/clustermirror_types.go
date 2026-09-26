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

// RefreshMode controls how the mirror is kept up to date.
// +kubebuilder:validation:Enum=interval
type RefreshMode string

const (
	// RefreshModeInterval rebuilds the whole mirror on a fixed timer. It is the only mode
	// today; watch/informer-driven incremental refresh is planned but not implemented yet.
	RefreshModeInterval RefreshMode = "interval"
)

// RefreshConfig controls how often the mirror is rebuilt.
type RefreshConfig struct {
	// mode is how the mirror refreshes. Only "interval" is supported today (a full rebuild
	// every interval). Watch-based refresh will be added later without breaking this field.
	// +kubebuilder:default=interval
	// +optional
	Mode RefreshMode `json:"mode,omitempty"`

	// interval is how often to rebuild the mirror when mode is "interval" (e.g. "5m", "1h").
	// +kubebuilder:default="5m"
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`
}

// ClusterMirrorSpec defines the desired state of ClusterMirror.
//
// A ClusterMirror is the long-lived, always-on logical mirror of the cluster: the component
// graph and per-namespace risk, rebuilt continuously. Unlike UpgradeAnalysis it carries no
// target version and emits no upgrade verdict — it reports the *current* operational risk so
// other consumers (UpgradeAnalysis via mirrorRef, and later PR blast-radius / change analysis)
// can read one live graph instead of each rebuilding its own.
type ClusterMirrorSpec struct {
	// scope controls which namespaces are included in the mirror. Reuses the same scoping as
	// UpgradeAnalysis. Defaults to "all" (system + application namespaces).
	// +optional
	Scope ScopeConfig `json:"scope,omitempty"`

	// refresh controls how often the mirror is rebuilt.
	// +optional
	Refresh RefreshConfig `json:"refresh,omitempty"`

	// source is where the mirror's report (<name>.mirror) is written on every rebuild — the same
	// destinations as an UpgradeAnalysis: file (default, the operator's reports dir), s3, blob or pvc.
	// Remote destinations keep no copy in the pod; the reports server reads them back on demand, and a
	// pipeline can read the report straight from the bucket.
	// +optional
	Source SourceConfig `json:"source,omitempty"`
}

// NamespaceMirrorStatus is the per-namespace risk summary published on the mirror status.
// It mirrors internal/graph.NamespaceRisk so the CR status matches the engine's aggregation.
type NamespaceMirrorStatus struct {
	// namespace is the namespace this summary is for.
	Namespace string `json:"namespace"`

	// maxRisk is the highest component risk in the namespace (0..100).
	MaxRisk int `json:"maxRisk"`

	// atRisk is the number of components in the namespace with risk at or above the engine's
	// at-risk threshold.
	AtRisk int `json:"atRisk"`
}

// ClusterMirrorStatus defines the observed state of ClusterMirror.
type ClusterMirrorStatus struct {
	// conditions represent the current state of the ClusterMirror resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// components is the number of nodes in the mirror graph.
	// +optional
	Components int `json:"components"`

	// edges is the number of dependency edges in the mirror graph.
	// +optional
	Edges int `json:"edges"`

	// atRisk is the number of components across the cluster with risk at or above the
	// engine's at-risk threshold.
	// +optional
	AtRisk int `json:"atRisk"`

	// lastSync is when the mirror was last rebuilt.
	// +optional
	LastSync *metav1.Time `json:"lastSync,omitempty"`

	// byNamespace is the per-namespace risk summary.
	// +optional
	ByNamespace []NamespaceMirrorStatus `json:"byNamespace,omitempty"`

	// syncError holds the error message when the last rebuild failed (e.g. a collector error), or when
	// the rebuild succeeded but its report couldn't be written. Empty on success.
	// +optional
	SyncError string `json:"syncError,omitempty"`

	// reportLocation is where the last report was written: a local path, or an s3://, blob or pvc URI.
	// +optional
	ReportLocation string `json:"reportLocation,omitempty"`

	// reportState is the outcome of writing the last report: "written" or "failed".
	// +optional
	// +kubebuilder:validation:Enum=written;failed
	ReportState string `json:"reportState,omitempty"`

	// reportError is the error message when the report could not be written to its destination —
	// e.g. an S3/Blob/PVC failure. Empty on success.
	// +optional
	ReportError string `json:"reportError,omitempty"`

	// observedGeneration is the spec generation the last rebuild ran against. A spec change
	// forces an immediate rebuild even within the refresh interval.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Components",type=integer,JSONPath=`.status.components`
// +kubebuilder:printcolumn:name="Edges",type=integer,JSONPath=`.status.edges`
// +kubebuilder:printcolumn:name="At Risk",type=integer,JSONPath=`.status.atRisk`
// +kubebuilder:printcolumn:name="Last Sync",type=date,JSONPath=`.status.lastSync`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterMirror is the Schema for the clustermirrors API. It is the always-on logical mirror
// of the cluster (component graph + per-namespace risk), rebuilt on an interval.
type ClusterMirror struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ClusterMirror
	// +required
	Spec ClusterMirrorSpec `json:"spec"`

	// status defines the observed state of ClusterMirror
	// +optional
	Status ClusterMirrorStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ClusterMirrorList contains a list of ClusterMirror
type ClusterMirrorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterMirror `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterMirror{}, &ClusterMirrorList{})
}
