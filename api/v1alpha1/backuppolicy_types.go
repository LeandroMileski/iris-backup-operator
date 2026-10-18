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

// Verification is the command run against a restored volume to prove a snapshot can be used.
type Verification struct {
	// image is the container image that runs the verification command.
	// +required
	Image string `json:"image"`

	// command is the container command, in exec form, run against the restored volume.
	// +kubebuilder:validation:MinItems=1
	// +required
	Command []string `json:"command"`

	// timeoutSeconds is how long the command may run.
	// +kubebuilder:validation:Minimum=1
	// +optional
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`
}

// BackupPolicySpec defines the desired state of BackupPolicy.
type BackupPolicySpec struct {
	// targetSelector selects the PersistentVolumeClaims to snapshot.
	// +required
	TargetSelector metav1.LabelSelector `json:"targetSelector"`

	// schedule is a five-field cron expression for when a backup run is due.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^(\S+\s+){4}\S+$`
	// +required
	Schedule string `json:"schedule"`

	// retention is the number of snapshots to keep for each selected PVC. Defaults to 7.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=7
	// +optional
	Retention *int32 `json:"retention,omitempty"`

	// suspend stops new backup runs when true. Defaults to false.
	// +kubebuilder:default=false
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// snapshotClassName is the VolumeSnapshotClass to use. When omitted, the controller uses the cluster default.
	// +optional
	SnapshotClassName *string `json:"snapshotClassName,omitempty"`

	// verifyRestore creates a RestoreVerification after a successful backup when true. Defaults to false.
	// +kubebuilder:default=false
	// +optional
	VerifyRestore *bool `json:"verifyRestore,omitempty"`

	// verification is the image and command used when verifyRestore is true.
	// +optional
	Verification *Verification `json:"verification,omitempty"`
}

const (
	// BackupPolicyConditionReady is True when the policy has been reconciled and is not failing.
	BackupPolicyConditionReady = "Ready"

	// BackupPolicyConditionDegraded is True when the latest backup or verification failed.
	BackupPolicyConditionDegraded = "Degraded"
)

// LastVerification is the latest restore-verification result mirrored onto the policy.
type LastVerification struct {
	// name is the RestoreVerification that produced this result.
	// +optional
	Name string `json:"name,omitempty"`

	// result is Succeeded or Failed.
	// +kubebuilder:validation:Enum=Succeeded;Failed
	// +optional
	Result string `json:"result,omitempty"`

	// completionTime is when that verification finished.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// message is a human-readable summary of the result.
	// +optional
	Message string `json:"message,omitempty"`
}

// BackupPolicyStatus defines the observed state of BackupPolicy.
type BackupPolicyStatus struct {
	// lastSuccessfulBackupTime is when the most recent backup run finished successfully.
	// +optional
	LastSuccessfulBackupTime *metav1.Time `json:"lastSuccessfulBackupTime,omitempty"`

	// lastVerification is the latest restore-verification result for this policy.
	// +optional
	LastVerification *LastVerification `json:"lastVerification,omitempty"`

	// conditions represent the current state of the BackupPolicy resource.
	// Ready is True when the policy is reconciled and not failing.
	// Degraded is True when the latest backup or verification failed.
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=".spec.schedule",description="Cron schedule for backup runs"
// +kubebuilder:printcolumn:name="Last Backup",type=date,JSONPath=".status.lastSuccessfulBackupTime",description="Time of the last successful backup"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Ready condition status"

// BackupPolicy is the Schema for the backup policies API
type BackupPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of BackupPolicy
	// +required
	Spec BackupPolicySpec `json:"spec"`

	// status defines the observed state of BackupPolicy
	// +optional
	Status BackupPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// BackupPolicyList contains a list of BackupPolicy
type BackupPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []BackupPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &BackupPolicy{}, &BackupPolicyList{})
		return nil
	})
}
