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
	backupPolicyReasonReconciled  = "Reconciled"
	backupPolicyReconciledMessage = "BackupPolicy is reconciled"
)

// BackupPolicyReconciler reconciles a BackupPolicy object
type BackupPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ops.leandro.ie,resources=backuppolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.leandro.ie,resources=backuppolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ops.leandro.ie,resources=backuppolicies/finalizers,verbs=update

// Reconcile fetches the BackupPolicy and records that this generation was observed.
// A missing policy is ignored so delete events do not return an error.
// The initial Ready condition is True because the policy has been reconciled and nothing has failed yet.
func (r *BackupPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	policy := &opsv1alpha1.BackupPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("BackupPolicy not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get BackupPolicy")
		return ctrl.Result{}, err
	}

	log.Info("Reconciling BackupPolicy")

	generationChanged := policy.Status.ObservedGeneration != policy.Generation
	if generationChanged {
		policy.Status.ObservedGeneration = policy.Generation
	}

	conditionChanged := meta.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{
		Type:               opsv1alpha1.BackupPolicyConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             backupPolicyReasonReconciled,
		Message:            backupPolicyReconciledMessage,
		ObservedGeneration: policy.Generation,
	})
	if !generationChanged && !conditionChanged {
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, policy); err != nil {
		log.Error(err, "Failed to update BackupPolicy status")
		return ctrl.Result{}, err
	}

	log.Info("Updated BackupPolicy status", "observedGeneration", policy.Generation)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.BackupPolicy{}).
		Named("backuppolicy").
		Complete(r)
}
