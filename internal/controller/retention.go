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
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

const defaultRetention = 7

// pruneSnapshots deletes owned VolumeSnapshots that exceed retention, oldest first, for each PVC.
func (r *BackupPolicyReconciler) pruneSnapshots(ctx context.Context, policy *opsv1alpha1.BackupPolicy) error {
	log := logf.FromContext(ctx)

	limit, err := retentionLimit(policy)
	if err != nil {
		log.Error(err, "Failed to read BackupPolicy retention")
		return err
	}

	owned, err := r.listOwnedSnapshots(ctx, policy)
	if err != nil {
		return err
	}

	grouped := map[string][]unstructured.Unstructured{}
	for _, snapshot := range owned {
		pvcName := snapshotPVC(&snapshot)
		grouped[pvcName] = append(grouped[pvcName], snapshot)
	}

	pvcs := make([]string, 0, len(grouped))
	for pvcName := range grouped {
		pvcs = append(pvcs, pvcName)
	}
	slices.Sort(pvcs)

	for _, pvcName := range pvcs {
		for _, snapshot := range snapshotsToDelete(grouped[pvcName], limit) {
			snapshot.SetGroupVersionKind(volumeSnapshotGVK)
			if err := r.Delete(ctx, &snapshot); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				log.Error(err, "Failed to delete VolumeSnapshot", "snapshot", snapshot.GetName(), "pvc", pvcName)
				return err
			}
			log.Info("Deleted VolumeSnapshot", "snapshot", snapshot.GetName(), "pvc", pvcName)
		}
	}
	return nil
}

func (r *BackupPolicyReconciler) listOwnedSnapshots(ctx context.Context, policy *opsv1alpha1.BackupPolicy) ([]unstructured.Unstructured, error) {
	log := logf.FromContext(ctx)

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(volumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"))
	if err := r.List(ctx, list, client.InNamespace(policy.Namespace)); err != nil {
		log.Error(err, "Failed to list VolumeSnapshots")
		return nil, err
	}

	owned := make([]unstructured.Unstructured, 0, len(list.Items))
	for _, snapshot := range list.Items {
		if ownedBy(policy, &snapshot) {
			owned = append(owned, snapshot)
		}
	}
	return owned, nil
}

func retentionLimit(policy *opsv1alpha1.BackupPolicy) (int, error) {
	if policy.Spec.Retention == nil {
		return defaultRetention, nil
	}
	if *policy.Spec.Retention < 1 {
		return 0, fmt.Errorf("retention must be at least 1")
	}
	return int(*policy.Spec.Retention), nil
}

// snapshotsToDelete returns the snapshots that exceed limit, oldest first.
// Age is creation time, then the scheduled instant, then name.
func snapshotsToDelete(snapshots []unstructured.Unstructured, limit int) []unstructured.Unstructured {
	if limit < 1 || len(snapshots) <= limit {
		return nil
	}
	ordered := slices.Clone(snapshots)
	slices.SortFunc(ordered, compareSnapshotAge)
	return ordered[:len(ordered)-limit]
}

func compareSnapshotAge(a, b unstructured.Unstructured) int {
	if c := a.GetCreationTimestamp().Compare(b.GetCreationTimestamp().Time); c != 0 {
		return c
	}
	if c := cmp.Compare(scheduledUnix(&a), scheduledUnix(&b)); c != 0 {
		return c
	}
	return cmp.Compare(a.GetName(), b.GetName())
}

func scheduledUnix(snapshot *unstructured.Unstructured) int64 {
	parsed, err := strconv.ParseInt(snapshot.GetLabels()[labelScheduledAt], 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func ownedBy(policy *opsv1alpha1.BackupPolicy, snapshot client.Object) bool {
	for _, ref := range snapshot.GetOwnerReferences() {
		if ref.UID == policy.UID && ref.Kind == "BackupPolicy" {
			return true
		}
	}
	return false
}

func snapshotPVC(snapshot *unstructured.Unstructured) string {
	if name := snapshot.GetAnnotations()[annotationPVC]; name != "" {
		return name
	}
	if name := snapshot.GetLabels()[labelBackupPVC]; name != "" {
		return name
	}
	name, _, _ := unstructured.NestedString(snapshot.Object, "spec", "source", "persistentVolumeClaimName")
	return name
}
