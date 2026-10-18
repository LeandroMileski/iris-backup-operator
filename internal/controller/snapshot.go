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
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

const (
	labelBackupPolicy = "ops.leandro.ie/backup-policy"
	labelBackupPVC    = "ops.leandro.ie/pvc"
	labelScheduledAt  = "ops.leandro.ie/scheduled-at"

	annotationPVC         = "ops.leandro.ie/pvc-name"
	annotationScheduledAt = "ops.leandro.ie/scheduled-at"
)

var volumeSnapshotGVK = schema.GroupVersionKind{
	Group:   "snapshot.storage.k8s.io",
	Version: "v1",
	Kind:    "VolumeSnapshot",
}

// createSnapshots creates one VolumeSnapshot for each PVC selected by the policy.
// scheduled is the cron instant being backed up. A snapshot that already exists for that instant is left in place.
func (r *BackupPolicyReconciler) createSnapshots(ctx context.Context, policy *opsv1alpha1.BackupPolicy, scheduled time.Time) error {
	log := logf.FromContext(ctx)

	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.TargetSelector)
	if err != nil {
		log.Error(err, "Failed to parse BackupPolicy target selector")
		return err
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, pvcs,
		client.InNamespace(policy.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		log.Error(err, "Failed to list PersistentVolumeClaims")
		return err
	}

	items := append([]corev1.PersistentVolumeClaim(nil), pvcs.Items...)
	slices.SortFunc(items, func(a, b corev1.PersistentVolumeClaim) int {
		return strings.Compare(a.Name, b.Name)
	})
	log.V(1).Info("Listed PersistentVolumeClaims", "count", len(items))

	for i := range items {
		if err := r.createSnapshot(ctx, policy, &items[i], scheduled); err != nil {
			return err
		}
	}
	return nil
}

func (r *BackupPolicyReconciler) createSnapshot(ctx context.Context, policy *opsv1alpha1.BackupPolicy, pvc *corev1.PersistentVolumeClaim, scheduled time.Time) error {
	log := logf.FromContext(ctx)

	snapshot, err := newVolumeSnapshot(policy, pvc, scheduled)
	if err != nil {
		log.Error(err, "Failed to build VolumeSnapshot", "pvc", pvc.Name)
		return err
	}
	if err := controllerutil.SetControllerReference(policy, snapshot, r.Scheme); err != nil {
		log.Error(err, "Failed to set VolumeSnapshot owner reference", "snapshot", snapshot.GetName())
		return err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(volumeSnapshotGVK)
	err = r.Get(ctx, client.ObjectKeyFromObject(snapshot), existing)
	if err == nil {
		log.Info("VolumeSnapshot already exists", "snapshot", snapshot.GetName(), "pvc", pvc.Name)
		return nil
	}
	if !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to get VolumeSnapshot", "snapshot", snapshot.GetName())
		return err
	}

	if err := r.Create(ctx, snapshot); err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Info("VolumeSnapshot already exists", "snapshot", snapshot.GetName(), "pvc", pvc.Name)
			return nil
		}
		log.Error(err, "Failed to create VolumeSnapshot", "snapshot", snapshot.GetName(), "pvc", pvc.Name)
		return err
	}
	log.Info("Created VolumeSnapshot", "snapshot", snapshot.GetName(), "pvc", pvc.Name)
	r.event(policy, corev1.EventTypeNormal, backupPolicyReasonSnapshotCreated,
		fmt.Sprintf("Created VolumeSnapshot %s for PersistentVolumeClaim %s", snapshot.GetName(), pvc.Name))
	return nil
}

func newVolumeSnapshot(policy *opsv1alpha1.BackupPolicy, pvc *corev1.PersistentVolumeClaim, scheduled time.Time) (*unstructured.Unstructured, error) {
	scheduled = scheduled.UTC()
	snapshot := &unstructured.Unstructured{}
	snapshot.SetGroupVersionKind(volumeSnapshotGVK)
	snapshot.SetNamespace(policy.Namespace)
	snapshot.SetName(snapshotName(policy.Name, pvc.Name, scheduled))
	snapshot.SetLabels(snapshotLabels(policy.Name, pvc.Name, scheduled))
	snapshot.SetAnnotations(map[string]string{
		annotationPVC:         pvc.Name,
		annotationScheduledAt: scheduled.Format(time.RFC3339),
	})

	spec := map[string]any{
		"source": map[string]any{
			"persistentVolumeClaimName": pvc.Name,
		},
	}
	if policy.Spec.SnapshotClassName != nil && *policy.Spec.SnapshotClassName != "" {
		spec["volumeSnapshotClassName"] = *policy.Spec.SnapshotClassName
	}
	if err := unstructured.SetNestedMap(snapshot.Object, spec, "spec"); err != nil {
		return nil, fmt.Errorf("set VolumeSnapshot spec: %w", err)
	}
	return snapshot, nil
}

func snapshotLabels(policyName, pvcName string, scheduled time.Time) map[string]string {
	labels := map[string]string{
		labelScheduledAt: strconv.FormatInt(scheduled.UTC().Unix(), 10),
	}
	setLabel(labels, labelBackupPolicy, policyName)
	setLabel(labels, labelBackupPVC, pvcName)
	return labels
}

func setLabel(labels map[string]string, key, value string) {
	if value != "" && len(validation.IsValidLabelValue(value)) == 0 {
		labels[key] = value
	}
}

func snapshotName(policyName, pvcName string, scheduled time.Time) string {
	name := fmt.Sprintf("%s-%s-%d", policyName, pvcName, scheduled.UTC().Unix())
	if len(name) <= 253 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("backup-%x", sum[:20])
}
