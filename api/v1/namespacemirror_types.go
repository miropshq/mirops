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

// +k8s:deepcopy-gen=package,register
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NamespaceMirrorSpec defines the desired state of NamespaceMirror
type NamespaceMirrorSpec struct {
	// +kubebuilder:validation:Required
	SourceNamespace string `json:"sourceNamespace"`
	// +kubebuilder:validation:Required
	TargetNamespace string `json:"targetNamespace"`
}

// NamespaceMirrorStatus defines the observed state of NamespaceMirror.
type NamespaceMirrorStatus struct {
	LastSyncTime      string `json:"lastSyncTime,omitempty"`
	SecretsSynced     int    `json:"secretsSynced,omitempty"`
	ConfigMapsSynced  int    `json:"configMapsSynced,omitempty"`
	DeploymentsSynced int    `json:"deploymentsSynced,omitempty"`
	ServicesSynced    int    `json:"servicesSynced,omitempty"`
	ReplicaSetsSynced int    `json:"replicaSetsSynced,omitempty"`
	PodsSynced        int    `json:"podsSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=namespacemirrors,scope=Cluster,shortName=nsm,singular=namespacemirror
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// NamespaceMirror is the Schema for the namespacemirrors API
type NamespaceMirror struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`
	Spec              NamespaceMirrorSpec   `json:"spec,omitempty"`
	Status            NamespaceMirrorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// NamespaceMirrorList contains a list of NamespaceMirror

type NamespaceMirrorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespaceMirror `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NamespaceMirror{}, &NamespaceMirrorList{})
}
