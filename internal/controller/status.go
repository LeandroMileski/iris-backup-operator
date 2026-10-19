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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

// persistStatus writes Ready, Degraded, and lastSuccessfulBackupTime once.
// A conflict retries against a fresh object so the status subresource update is not lost.
func (r *BackupPolicyReconciler) persistStatus(ctx context.Context, policy *opsv1alpha1.BackupPolicy, progress backupProgress) error {
	log := logf.FromContext(ctx)

	original := policy.Status.DeepCopy()
	policy.Status.ObservedGeneration = policy.Generation
	if progress.recorded != nil {
		policy.Status.LastSuccessfulBackupTime = &metav1.Time{Time: progress.recorded.UTC()}
	}
	if progress.mirrored {
		policy.Status.LastVerification = progress.lastVerification
	}
	r.applyConditions(policy, progress)
	if equality.Semantic.DeepEqual(original, &policy.Status) {
		return nil
	}

	desired := policy.Status.DeepCopy()
	key := client.ObjectKeyFromObject(policy)
	var err error
	for range 3 {
		current := &opsv1alpha1.BackupPolicy{}
		if err = r.Get(ctx, key, current); err != nil {
			log.Error(err, "Failed to get BackupPolicy")
			return err
		}
		if equality.Semantic.DeepEqual(&current.Status, desired) {
			policy.SetResourceVersion(current.GetResourceVersion())
			return nil
		}
		current.Status = *desired
		err = r.Status().Update(ctx, current)
		if err == nil {
			policy.SetResourceVersion(current.GetResourceVersion())
			policy.Status = current.Status
			log.Info("Updated BackupPolicy status", "observedGeneration", policy.Status.ObservedGeneration)
			return nil
		}
		if !apierrors.IsConflict(err) {
			log.Error(err, "Failed to update BackupPolicy status")
			return err
		}
		log.Info("BackupPolicy status update conflicted, retrying")
	}
	log.Error(err, "Failed to update BackupPolicy status")
	return err
}

func (r *BackupPolicyReconciler) applyConditions(policy *opsv1alpha1.BackupPolicy, progress backupProgress) {
	ready := metav1.Condition{
		Type:               opsv1alpha1.BackupPolicyConditionReady,
		ObservedGeneration: policy.Generation,
	}
	degraded := metav1.Condition{
		Type:               opsv1alpha1.BackupPolicyConditionDegraded,
		ObservedGeneration: policy.Generation,
	}
	switch {
	case progress.failed:
		ready.Status = metav1.ConditionFalse
		ready.Reason = backupPolicyReasonBackupFailed
		ready.Message = progress.message
		degraded.Status = metav1.ConditionTrue
		degraded.Reason = backupPolicyReasonBackupFailed
		degraded.Message = progress.message
	case progress.verificationFailed:
		message := verificationFailureMessage(progress)
		ready.Status = metav1.ConditionFalse
		ready.Reason = backupPolicyReasonVerificationFailed
		ready.Message = message
		degraded.Status = metav1.ConditionTrue
		degraded.Reason = backupPolicyReasonVerificationFailed
		degraded.Message = message
	case progress.suspended:
		ready.Status = metav1.ConditionTrue
		ready.Reason = backupPolicyReasonSuspended
		ready.Message = backupPolicySuspendedMessage
		degraded.Status = metav1.ConditionFalse
		degraded.Reason = backupPolicyReasonSuspended
		degraded.Message = backupPolicySuspendedMessage
	default:
		ready.Status = metav1.ConditionTrue
		ready.Reason = backupPolicyReasonReconciled
		ready.Message = backupPolicyReconciledMessage
		degraded.Status = metav1.ConditionFalse
		degraded.Reason = backupPolicyReasonReconciled
		degraded.Message = backupPolicyReconciledMessage
	}
	meta.SetStatusCondition(&policy.Status.Conditions, ready)
	meta.SetStatusCondition(&policy.Status.Conditions, degraded)
}

func verificationFailureMessage(progress backupProgress) string {
	if progress.lastVerification != nil && progress.lastVerification.Message != "" {
		return progress.lastVerification.Message
	}
	return "Restore verification failed"
}

func (r *BackupPolicyReconciler) event(obj runtime.Object, eventType, reason, message string) {
	if r.Recorder == nil || obj == nil {
		return
	}
	r.Recorder.Eventf(obj, nil, eventType, reason, reason, message)
}
