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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

const (
	restoreVerificationPendingMessage   = "RestoreVerification is pending"
	restoreVerificationRunningMessage   = "RestoreVerification is running"
	restoreVerificationSucceededMessage = "RestoreVerification succeeded"
	restoreVerificationFailedMessage    = "RestoreVerification failed"
)

// RestoreVerificationReconciler reconciles a RestoreVerification object
type RestoreVerificationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Now supplies the current time. Tests set it; a nil Now uses time.Now.
	Now func() time.Time
}

// +kubebuilder:rbac:groups=ops.leandro.ie,resources=restoreverifications,verbs=get;list;watch
// +kubebuilder:rbac:groups=ops.leandro.ie,resources=restoreverifications/status,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get

// Reconcile moves a RestoreVerification one phase at a time.
// An empty phase becomes Pending, and Pending becomes Running.
// Running creates a PersistentVolumeClaim from the snapshot and a Pod that mounts it read-only.
// A finished Pod records Succeeded or Failed from its exit code, then the Pod and claim are deleted.
// Succeeded and Failed stay as they are.
// A missing object is ignored so delete events do not return an error.
func (r *RestoreVerificationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	verification := &opsv1alpha1.RestoreVerification{}
	if err := r.Get(ctx, req.NamespacedName, verification); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("RestoreVerification not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get RestoreVerification")
		return ctrl.Result{}, err
	}

	if !verification.DeletionTimestamp.IsZero() {
		log.Info("RestoreVerification is deleting")
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling RestoreVerification", "phase", verification.Status.Phase)

	next, err := advanceVerification(verification.Status.Phase, false, false)
	if err != nil {
		log.Error(err, "Failed to advance RestoreVerification")
		return ctrl.Result{}, err
	}
	if err := r.persistVerification(ctx, verification, next, ""); err != nil {
		return ctrl.Result{}, err
	}
	if next == opsv1alpha1.RestoreVerificationPhasePending {
		return ctrl.Result{Requeue: true}, nil
	}
	if next == opsv1alpha1.RestoreVerificationPhaseRunning {
		if err := r.ensureRestore(ctx, verification); err != nil {
			log.Error(err, "Failed to create restore resources")
			return ctrl.Result{}, err
		}
		done, succeeded, message, err := r.observeVerificationPod(ctx, verification)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done {
			return ctrl.Result{}, nil
		}
		next, err = advanceVerification(verification.Status.Phase, true, succeeded)
		if err != nil {
			log.Error(err, "Failed to advance RestoreVerification")
			return ctrl.Result{}, err
		}
		if err := r.persistVerification(ctx, verification, next, message); err != nil {
			return ctrl.Result{}, err
		}
	}
	if next == opsv1alpha1.RestoreVerificationPhaseSucceeded || next == opsv1alpha1.RestoreVerificationPhaseFailed {
		return r.cleanupRestore(ctx, verification)
	}
	return ctrl.Result{}, nil
}

// persistVerification writes the phase, times, and conditions once.
// A conflict retries against a fresh object so the status subresource update is not lost.
func (r *RestoreVerificationReconciler) persistVerification(ctx context.Context, verification *opsv1alpha1.RestoreVerification, phase, message string) error {
	log := logf.FromContext(ctx)

	original := verification.Status.DeepCopy()
	applyVerificationStatus(verification, phase, message, r.now())
	if equality.Semantic.DeepEqual(original, &verification.Status) {
		return nil
	}

	desired := verification.Status.DeepCopy()
	key := client.ObjectKeyFromObject(verification)
	var err error
	for range 3 {
		current := &opsv1alpha1.RestoreVerification{}
		if err = r.Get(ctx, key, current); err != nil {
			log.Error(err, "Failed to get RestoreVerification")
			return err
		}
		if equality.Semantic.DeepEqual(&current.Status, desired) {
			verification.SetResourceVersion(current.GetResourceVersion())
			return nil
		}
		current.Status = *desired
		err = r.Status().Update(ctx, current)
		if err == nil {
			verification.SetResourceVersion(current.GetResourceVersion())
			verification.Status = current.Status
			log.Info("Updated RestoreVerification status", "phase", verification.Status.Phase)
			return nil
		}
		if !apierrors.IsConflict(err) {
			log.Error(err, "Failed to update RestoreVerification status")
			return err
		}
		log.Info("RestoreVerification status update conflicted, retrying")
	}
	log.Error(err, "Failed to update RestoreVerification status")
	return err
}

func applyVerificationStatus(verification *opsv1alpha1.RestoreVerification, phase, message string, now time.Time) {
	status := &verification.Status
	status.ObservedGeneration = verification.Generation
	if status.Phase != phase {
		status.Phase = phase
		if message == "" {
			message = defaultVerificationMessage(phase)
		}
		status.Message = message
		switch phase {
		case opsv1alpha1.RestoreVerificationPhaseRunning:
			status.StartTime = &metav1.Time{Time: now.UTC()}
		case opsv1alpha1.RestoreVerificationPhaseSucceeded, opsv1alpha1.RestoreVerificationPhaseFailed:
			status.CompletionTime = &metav1.Time{Time: now.UTC()}
		}
	}
	applyVerificationConditions(verification)
}

func defaultVerificationMessage(phase string) string {
	switch phase {
	case opsv1alpha1.RestoreVerificationPhasePending:
		return restoreVerificationPendingMessage
	case opsv1alpha1.RestoreVerificationPhaseRunning:
		return restoreVerificationRunningMessage
	case opsv1alpha1.RestoreVerificationPhaseSucceeded:
		return restoreVerificationSucceededMessage
	case opsv1alpha1.RestoreVerificationPhaseFailed:
		return restoreVerificationFailedMessage
	default:
		return ""
	}
}

func applyVerificationConditions(verification *opsv1alpha1.RestoreVerification) {
	phase := verification.Status.Phase
	ready := metav1.Condition{
		Type:               opsv1alpha1.RestoreVerificationConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             phase,
		Message:            verification.Status.Message,
		ObservedGeneration: verification.Generation,
	}
	degraded := metav1.Condition{
		Type:               opsv1alpha1.RestoreVerificationConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             phase,
		Message:            verification.Status.Message,
		ObservedGeneration: verification.Generation,
	}
	if phase == opsv1alpha1.RestoreVerificationPhaseSucceeded {
		ready.Status = metav1.ConditionTrue
	}
	if phase == opsv1alpha1.RestoreVerificationPhaseFailed {
		degraded.Status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&verification.Status.Conditions, ready)
	meta.SetStatusCondition(&verification.Status.Conditions, degraded)
}

func (r *RestoreVerificationReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// SetupWithManager sets up the controller with the Manager.
func (r *RestoreVerificationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.RestoreVerification{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Pod{}).
		Named("restoreverification").
		Complete(r)
}
