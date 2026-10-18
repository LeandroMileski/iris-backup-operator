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
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

// createVerifications creates one RestoreVerification per snapshotted PVC when verifyRestore is true.
// An object that already exists for that schedule slot is left in place.
func (r *BackupPolicyReconciler) createVerifications(ctx context.Context, policy *opsv1alpha1.BackupPolicy, scheduled time.Time) error {
	if policy.Spec.VerifyRestore == nil || !*policy.Spec.VerifyRestore {
		return nil
	}
	if policy.Spec.Verification == nil || policy.Spec.Verification.Image == "" || len(policy.Spec.Verification.Command) == 0 {
		err := fmt.Errorf("verification is required when verifyRestore is true")
		logf.FromContext(ctx).Error(err, "Failed to create RestoreVerification")
		return err
	}

	pvcs, err := r.selectedPVCs(ctx, policy)
	if err != nil {
		return err
	}
	for i := range pvcs {
		if err := r.createVerification(ctx, policy, &pvcs[i], scheduled); err != nil {
			return err
		}
	}
	return nil
}

func (r *BackupPolicyReconciler) selectedPVCs(ctx context.Context, policy *opsv1alpha1.BackupPolicy) ([]corev1.PersistentVolumeClaim, error) {
	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.TargetSelector)
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to parse BackupPolicy target selector")
		return nil, err
	}
	list := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, list, client.InNamespace(policy.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list PersistentVolumeClaims")
		return nil, err
	}
	items := append([]corev1.PersistentVolumeClaim(nil), list.Items...)
	slices.SortFunc(items, func(a, b corev1.PersistentVolumeClaim) int {
		return strings.Compare(a.Name, b.Name)
	})
	return items, nil
}

func (r *BackupPolicyReconciler) createVerification(ctx context.Context, policy *opsv1alpha1.BackupPolicy, pvc *corev1.PersistentVolumeClaim, scheduled time.Time) error {
	log := logf.FromContext(ctx)
	verification := newPolicyVerification(policy, pvc, scheduled)
	if err := controllerutil.SetControllerReference(policy, verification, r.Scheme); err != nil {
		log.Error(err, "Failed to set RestoreVerification owner reference", "verification", verification.Name)
		return err
	}

	existing := &opsv1alpha1.RestoreVerification{}
	err := r.Get(ctx, client.ObjectKeyFromObject(verification), existing)
	if err == nil {
		log.Info("RestoreVerification already exists", "verification", verification.Name, "pvc", pvc.Name)
		return nil
	}
	if !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to get RestoreVerification", "verification", verification.Name)
		return err
	}
	if err := r.Create(ctx, verification); err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Info("RestoreVerification already exists", "verification", verification.Name, "pvc", pvc.Name)
			return nil
		}
		log.Error(err, "Failed to create RestoreVerification", "verification", verification.Name, "pvc", pvc.Name)
		return err
	}
	log.Info("Created RestoreVerification", "verification", verification.Name, "pvc", pvc.Name)
	r.event(policy, corev1.EventTypeNormal, backupPolicyReasonVerificationCreated,
		fmt.Sprintf("Created RestoreVerification %s for PersistentVolumeClaim %s", verification.Name, pvc.Name))
	return nil
}

func newPolicyVerification(policy *opsv1alpha1.BackupPolicy, pvc *corev1.PersistentVolumeClaim, scheduled time.Time) *opsv1alpha1.RestoreVerification {
	scheduled = scheduled.UTC()
	labels := map[string]string{}
	setLabel(labels, labelBackupPolicy, policy.Name)
	setLabel(labels, labelBackupPVC, pvc.Name)
	return &opsv1alpha1.RestoreVerification{
		ObjectMeta: metav1.ObjectMeta{
			Name:      verificationObjectName(policy.Name, pvc.Name, scheduled),
			Namespace: policy.Namespace,
			Labels:    labels,
		},
		Spec: opsv1alpha1.RestoreVerificationSpec{
			SnapshotName:              snapshotName(policy.Name, pvc.Name, scheduled),
			PersistentVolumeClaimName: pvc.Name,
			Verification:              *policy.Spec.Verification.DeepCopy(),
		},
	}
}

func verificationObjectName(policyName, pvcName string, scheduled time.Time) string {
	name := fmt.Sprintf("verify-%s-%s-%d", policyName, pvcName, scheduled.UTC().Unix())
	if len(name) <= 253 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("verify-%x", sum[:20])
}

// mirrorVerification copies the newest finished RestoreVerification onto the policy.
// A newer verification that is still running does not replace a finished result.
func (r *BackupPolicyReconciler) mirrorVerification(ctx context.Context, policy *opsv1alpha1.BackupPolicy, progress *backupProgress) error {
	last, failed, err := r.latestVerification(ctx, policy)
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list RestoreVerifications")
		return err
	}
	progress.mirrored = true
	progress.lastVerification = last
	progress.verificationFailed = failed
	return nil
}

func (r *BackupPolicyReconciler) latestVerification(ctx context.Context, policy *opsv1alpha1.BackupPolicy) (*opsv1alpha1.LastVerification, bool, error) {
	list := &opsv1alpha1.RestoreVerificationList{}
	if err := r.List(ctx, list, client.InNamespace(policy.Namespace)); err != nil {
		return nil, false, err
	}
	var latest *opsv1alpha1.RestoreVerification
	for i := range list.Items {
		item := &list.Items[i]
		if !metav1.IsControlledBy(item, policy) {
			continue
		}
		if !verificationFinished(item) {
			continue
		}
		if latest == nil || verificationNewer(item, latest) {
			latest = item
		}
	}
	if latest == nil {
		return nil, false, nil
	}
	return &opsv1alpha1.LastVerification{
		Name:           latest.Name,
		Result:         latest.Status.Phase,
		CompletionTime: latest.Status.CompletionTime,
		Message:        latest.Status.Message,
	}, latest.Status.Phase == opsv1alpha1.RestoreVerificationPhaseFailed, nil
}

func verificationFinished(verification *opsv1alpha1.RestoreVerification) bool {
	return verification.Status.Phase == opsv1alpha1.RestoreVerificationPhaseSucceeded ||
		verification.Status.Phase == opsv1alpha1.RestoreVerificationPhaseFailed
}

func verificationNewer(candidate, current *opsv1alpha1.RestoreVerification) bool {
	candidateTime := verificationCompletion(candidate)
	currentTime := verificationCompletion(current)
	if candidateTime.Equal(currentTime) {
		return candidate.Name > current.Name
	}
	return candidateTime.After(currentTime)
}

func verificationCompletion(verification *opsv1alpha1.RestoreVerification) time.Time {
	if verification.Status.CompletionTime == nil {
		return time.Time{}
	}
	return verification.Status.CompletionTime.Time
}
