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
	"strconv"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

func TestSnapshotsToDelete(t *testing.T) {
	same := metav1.NewTime(time.Unix(50, 0))
	older := metav1.NewTime(time.Unix(10, 0))
	newer := metav1.NewTime(time.Unix(80, 0))

	tests := []struct {
		name  string
		limit int
		snaps []unstructured.Unstructured
		want  []string
	}{
		{
			name:  "within retention",
			limit: 3,
			snaps: []unstructured.Unstructured{agedSnapshot("a", same, 1), agedSnapshot("b", same, 2)},
		},
		{
			name:  "equal creation time uses the scheduled instant",
			limit: 1,
			snaps: []unstructured.Unstructured{
				agedSnapshot("c", same, 30),
				agedSnapshot("a", same, 10),
				agedSnapshot("b", same, 20),
			},
			want: []string{"a", "b"},
		},
		{
			name:  "creation time is older than a later scheduled instant",
			limit: 1,
			snaps: []unstructured.Unstructured{
				agedSnapshot("fresh-clock", newer, 1),
				agedSnapshot("old-clock", older, 99),
			},
			want: []string{"old-clock"},
		},
		{
			name:  "equal age falls back to name",
			limit: 1,
			snaps: []unstructured.Unstructured{
				agedSnapshot("zeta", same, 5),
				agedSnapshot("alpha", same, 5),
			},
			want: []string{"alpha"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshotsToDelete(tt.snaps, tt.limit)
			names := make([]string, 0, len(got))
			for _, snapshot := range got {
				names = append(names, snapshot.GetName())
			}
			if len(names) != len(tt.want) {
				t.Fatalf("snapshotsToDelete() = %v, want %v", names, tt.want)
			}
			for i := range names {
				if names[i] != tt.want[i] {
					t.Fatalf("snapshotsToDelete() = %v, want %v", names, tt.want)
				}
			}
		})
	}
}

func agedSnapshot(name string, created metav1.Time, scheduled int64) unstructured.Unstructured {
	snapshot := unstructured.Unstructured{}
	snapshot.SetName(name)
	snapshot.SetCreationTimestamp(created)
	snapshot.SetLabels(map[string]string{labelScheduledAt: strconv.FormatInt(scheduled, 10)})
	return snapshot
}

var _ = Describe("BackupPolicy retention", func() {
	const (
		policyName      = "retention-policy"
		policyNamespace = "default"
		dataPVC         = "data"
		logsPVC         = "logs"
	)

	var (
		policyKey types.NamespacedName
		policy    *opsv1alpha1.BackupPolicy
	)

	BeforeEach(func() {
		retention := int32(2)
		policy = newBackupPolicy(policyName, policyNamespace, 1)
		policy.Spec.Retention = &retention
		policy.Spec.Schedule = "0 0 * * *"
		policyKey = types.NamespacedName{Name: policyName, Namespace: policyNamespace}
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())

		created := &opsv1alpha1.BackupPolicy{}
		Expect(k8sClient.Get(ctx, policyKey, created)).To(Succeed())
		policy = created

		for _, scheduled := range []int64{1000, 2000, 3000} {
			createOwnedSnapshot(policy, dataPVC, time.Unix(scheduled, 0))
		}
		createOwnedSnapshot(policy, logsPVC, time.Unix(1000, 0))
		createForeignSnapshot(policy, dataPVC, time.Unix(500, 0))
	})

	AfterEach(func() {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(volumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"))
		Expect(k8sClient.List(ctx, list, client.InNamespace(policyNamespace))).To(Succeed())
		for i := range list.Items {
			list.Items[i].SetGroupVersionKind(volumeSnapshotGVK)
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &list.Items[i]))).To(Succeed())
		}
		current := &opsv1alpha1.BackupPolicy{}
		err := k8sClient.Get(ctx, policyKey, current)
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Delete(ctx, current)).To(Succeed())
	})

	It("deletes owned snapshots beyond retention, oldest first, for each PVC", func() {
		finished := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
		policy.Status.LastSuccessfulBackupTime = &metav1.Time{Time: finished}
		Expect(k8sClient.Status().Update(ctx, policy)).To(Succeed())

		result, err := (&BackupPolicyReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Now:    func() time.Time { return finished.Add(time.Minute) },
		}).Reconcile(ctx, reconcile.Request{NamespacedName: policyKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		Expect(snapshotExists(policyNamespace, snapshotName(policyName, dataPVC, time.Unix(1000, 0)))).To(BeFalse())
		Expect(snapshotExists(policyNamespace, snapshotName(policyName, dataPVC, time.Unix(2000, 0)))).To(BeTrue())
		Expect(snapshotExists(policyNamespace, snapshotName(policyName, dataPVC, time.Unix(3000, 0)))).To(BeTrue())
		Expect(snapshotExists(policyNamespace, snapshotName(policyName, logsPVC, time.Unix(1000, 0)))).To(BeTrue())
		Expect(snapshotExists(policyNamespace, snapshotName(policyName, dataPVC, time.Unix(500, 0)))).To(BeTrue())
	})
})

func createOwnedSnapshot(policy *opsv1alpha1.BackupPolicy, pvcName string, scheduled time.Time) {
	snapshot, err := newVolumeSnapshot(policy, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: policy.Namespace},
	}, scheduled)
	Expect(err).NotTo(HaveOccurred())
	Expect(controllerutil.SetControllerReference(policy, snapshot, k8sClient.Scheme())).To(Succeed())
	Expect(k8sClient.Create(ctx, snapshot)).To(Succeed())
}

func createForeignSnapshot(policy *opsv1alpha1.BackupPolicy, pvcName string, scheduled time.Time) {
	snapshot, err := newVolumeSnapshot(policy, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: policy.Namespace},
	}, scheduled)
	Expect(err).NotTo(HaveOccurred())
	controller := false
	snapshot.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "ops.leandro.ie/v1alpha1",
		Kind:       "BackupPolicy",
		Name:       "someone-else",
		UID:        "other-uid",
		Controller: &controller,
	}})
	Expect(k8sClient.Create(ctx, snapshot)).To(Succeed())
}

func snapshotExists(namespace, name string) bool {
	snapshot := &unstructured.Unstructured{}
	snapshot.SetGroupVersionKind(volumeSnapshotGVK)
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, snapshot)
	if err == nil {
		return true
	}
	if apierrors.IsNotFound(err) {
		return false
	}
	Expect(err).NotTo(HaveOccurred())
	return false
}
