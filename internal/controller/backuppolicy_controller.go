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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

const (
	backupPolicyFinalizer                 = "ops.leandro.ie/backup-policy"
	backupPolicyReasonReconciled          = "Reconciled"
	backupPolicyReconciledMessage         = "BackupPolicy is reconciled"
	backupPolicyReasonSuspended           = "Suspended"
	backupPolicySuspendedMessage          = "BackupPolicy is suspended"
	backupPolicyReasonBackupFailed        = "BackupFailed"
	backupPolicyReasonVerificationFailed  = "VerificationFailed"
	backupPolicyReasonSnapshotCreated     = "SnapshotCreated"
	backupPolicyReasonSnapshotPruned      = "SnapshotPruned"
	backupPolicyReasonVerificationCreated = "VerificationCreated"
)

// BackupPolicyReconciler reconciles a BackupPolicy object
type BackupPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// Now supplies the current time. Tests set it; a nil Now uses time.Now.
	Now func() time.Time
}

// +kubebuilder:rbac:groups=ops.leandro.ie,resources=backuppolicies,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=ops.leandro.ie,resources=backuppolicies/status,verbs=update
// +kubebuilder:rbac:groups=ops.leandro.ie,resources=backuppolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=list
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;create;delete
// +kubebuilder:rbac:groups=ops.leandro.ie,resources=restoreverifications,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile fetches the BackupPolicy, records status, and creates VolumeSnapshots when a run is due.
// It deletes owned snapshots that exceed retention, oldest first, for each PVC.
// A missing policy is ignored so delete events do not return an error.
// The result asks to reconcile again at the next scheduled time.
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

	if !policy.DeletionTimestamp.IsZero() {
		return r.finalizeBackupPolicy(ctx, policy)
	}
	if err := r.ensureFinalizer(ctx, policy); err != nil {
		return ctrl.Result{}, err
	}

	progress, err := r.runBackup(ctx, policy)
	if syncErr := r.mirrorVerification(ctx, policy, &progress); syncErr != nil && err == nil {
		err = syncErr
	}
	if err != nil {
		progress.failed = true
		progress.message = err.Error()
	}
	if statusErr := r.persistStatus(ctx, policy, progress); statusErr != nil {
		return ctrl.Result{}, statusErr
	}
	if err != nil {
		r.event(policy, corev1.EventTypeWarning, backupPolicyReasonBackupFailed, "Backup failed: "+err.Error())
		return ctrl.Result{}, err
	}
	return progress.result, nil
}

// runBackup creates snapshots when a run is due and prunes owned snapshots past retention.
type backupProgress struct {
	result             ctrl.Result
	suspended          bool
	failed             bool
	verificationFailed bool
	message            string
	recorded           *time.Time
	mirrored           bool
	lastVerification   *opsv1alpha1.LastVerification
}

func (r *BackupPolicyReconciler) runBackup(ctx context.Context, policy *opsv1alpha1.BackupPolicy) (backupProgress, error) {
	log := logf.FromContext(ctx)

	if backupSuspended(policy) {
		log.Info("BackupPolicy is suspended")
		if err := r.pruneSnapshots(ctx, policy); err != nil {
			return backupProgress{suspended: true}, err
		}
		return backupProgress{suspended: true}, nil
	}

	now := r.now()
	next, err := nextRun(policy.Spec.Schedule, referenceTime(policy), now)
	if err != nil {
		log.Error(err, "Failed to determine next BackupPolicy run")
		return backupProgress{}, err
	}
	if next.After(now) {
		delay := next.Sub(now)
		log.V(1).Info("Backup run is not due", "requeueAfter", delay)
		if err := r.pruneSnapshots(ctx, policy); err != nil {
			return backupProgress{}, err
		}
		return backupProgress{result: ctrl.Result{RequeueAfter: delay}}, nil
	}

	if err := r.createSnapshots(ctx, policy, next); err != nil {
		return backupProgress{}, err
	}
	if err := r.pruneSnapshots(ctx, policy); err != nil {
		return backupProgress{}, err
	}
	if err := r.createVerifications(ctx, policy, next); err != nil {
		return backupProgress{}, err
	}
	recorded := now.UTC()

	following, err := nextRun(policy.Spec.Schedule, now, now)
	if err != nil {
		log.Error(err, "Failed to determine next BackupPolicy run")
		return backupProgress{}, err
	}
	return backupProgress{
		result:   ctrl.Result{RequeueAfter: following.Sub(now)},
		recorded: &recorded,
	}, nil
}

func (r *BackupPolicyReconciler) ensureFinalizer(ctx context.Context, policy *opsv1alpha1.BackupPolicy) error {
	if controllerutil.ContainsFinalizer(policy, backupPolicyFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(policy, backupPolicyFinalizer)
	if err := r.Update(ctx, policy); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to add BackupPolicy finalizer")
		return err
	}
	logf.FromContext(ctx).Info("Added BackupPolicy finalizer")
	return nil
}

// finalizeBackupPolicy deletes every owned VolumeSnapshot, then removes the finalizer.
// Snapshots that still exist keep the finalizer in place so deletion is retried.
func (r *BackupPolicyReconciler) finalizeBackupPolicy(ctx context.Context, policy *opsv1alpha1.BackupPolicy) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(policy, backupPolicyFinalizer) {
		return ctrl.Result{}, nil
	}

	owned, err := r.listOwnedSnapshots(ctx, policy)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range owned {
		if _, err := r.deleteSnapshot(ctx, &owned[i], snapshotPVC(&owned[i])); err != nil {
			return ctrl.Result{}, err
		}
	}

	remaining, err := r.listOwnedSnapshots(ctx, policy)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(remaining) > 0 {
		log.Info("Waiting for VolumeSnapshots to delete", "count", len(remaining))
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	controllerutil.RemoveFinalizer(policy, backupPolicyFinalizer)
	if err := r.Update(ctx, policy); err != nil {
		log.Error(err, "Failed to remove BackupPolicy finalizer")
		return ctrl.Result{}, err
	}
	log.Info("Removed BackupPolicy finalizer")
	return ctrl.Result{}, nil
}

func (r *BackupPolicyReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func backupSuspended(policy *opsv1alpha1.BackupPolicy) bool {
	return policy.Spec.Suspend != nil && *policy.Spec.Suspend
}

// referenceTime is the instant nextRun searches forward from.
// A recorded backup uses its finish time. The first run starts just before creation
// so a schedule instant equal to the creation time can still be due.
func referenceTime(policy *opsv1alpha1.BackupPolicy) time.Time {
	if policy.Status.LastSuccessfulBackupTime != nil && !policy.Status.LastSuccessfulBackupTime.IsZero() {
		return policy.Status.LastSuccessfulBackupTime.Time
	}
	if !policy.CreationTimestamp.IsZero() {
		return policy.CreationTimestamp.Add(-time.Nanosecond)
	}
	return time.Time{}
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("backuppolicy")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.BackupPolicy{}).
		Owns(&opsv1alpha1.RestoreVerification{}).
		Named("backuppolicy").
		Complete(r)
}
