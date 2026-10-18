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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RestoreVerificationSpec defines the desired state of RestoreVerification.
// The object restores one VolumeSnapshot and runs one verification command.
type RestoreVerificationSpec struct {
	// snapshotName is the VolumeSnapshot, in the same namespace, to restore.
	// +kubebuilder:validation:MinLength=1
	// +required
	SnapshotName string `json:"snapshotName"`

	// persistentVolumeClaimName is the claim the snapshot was taken from.
	// +kubebuilder:validation:MinLength=1
	// +required
	PersistentVolumeClaimName string `json:"persistentVolumeClaimName"`

	// verification is the image and command run against the restored volume.
	// +required
	Verification Verification `json:"verification"`
}

const (
	// RestoreVerificationPhasePending means the verification has been accepted and has not started.
	RestoreVerificationPhasePending = "Pending"

	// RestoreVerificationPhaseRunning means the restore is in progress.
	RestoreVerificationPhaseRunning = "Running"

	// RestoreVerificationPhaseSucceeded means the verification command succeeded.
	RestoreVerificationPhaseSucceeded = "Succeeded"

	// RestoreVerificationPhaseFailed means the verification command failed.
	RestoreVerificationPhaseFailed = "Failed"

	// RestoreVerificationConditionReady is True when the verification succeeded.
	RestoreVerificationConditionReady = "Ready"

	// RestoreVerificationConditionDegraded is True when the verification failed.
	RestoreVerificationConditionDegraded = "Degraded"
)

// RestoreVerificationStatus defines the observed state of RestoreVerification.
type RestoreVerificationStatus struct {
	// observedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// phase is Pending, Running, Succeeded, or Failed.
	// An empty phase becomes Pending, Pending becomes Running, and Running becomes Succeeded or Failed.
	// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// message is a human-readable summary of the phase.
	// +optional
	Message string `json:"message,omitempty"`

	// startTime is when the verification entered Running.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// completionTime is when the verification entered Succeeded or Failed.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// conditions represent the current state of the RestoreVerification resource.
	// Ready is True only after the verification succeeds.
	// Degraded is True when the verification fails.
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase",description="Verification phase"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Ready condition status"

// RestoreVerification is the Schema for the restoreverifications API
type RestoreVerification struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of RestoreVerification
	// +required
	Spec RestoreVerificationSpec `json:"spec"`

	// status defines the observed state of RestoreVerification
	// +optional
	Status RestoreVerificationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// RestoreVerificationList contains a list of RestoreVerification
type RestoreVerificationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []RestoreVerification `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &RestoreVerification{}, &RestoreVerificationList{})
		return nil
	})
}
