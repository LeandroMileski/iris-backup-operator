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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

var _ = Describe("BackupPolicy finalizer", func() {
	const (
		policyName      = "finalizer-policy"
		policyNamespace = testNamespace
		pvcName         = restoreVolumeName
	)

	var policyKey types.NamespacedName

	BeforeEach(func() {
		policy := newBackupPolicy(policyName, 1)
		policyKey = types.NamespacedName{Name: policyName, Namespace: policyNamespace}
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())
	})

	AfterEach(func() {
		deleteSnapshotIfPresent(policyNamespace, snapshotName(policyName, pvcName, time.Unix(1000, 0)))
		deleteSnapshotIfPresent(policyNamespace, snapshotName(policyName, pvcName, time.Unix(500, 0)))
		finishPolicy(ctx, policyKey)
	})

	It("deletes owned snapshots and then removes the finalizer", func() {
		reconciler := &BackupPolicyReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Now:    func() time.Time { return time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC) },
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: policyKey})
		Expect(err).NotTo(HaveOccurred())

		policy := &opsv1alpha1.BackupPolicy{}
		Expect(k8sClient.Get(ctx, policyKey, policy)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(policy, backupPolicyFinalizer)).To(BeTrue())

		createOwnedSnapshot(policy, pvcName, time.Unix(1000, 0))
		createForeignSnapshot(policy, pvcName, time.Unix(500, 0))

		Expect(k8sClient.Delete(ctx, policy)).To(Succeed())
		Expect(k8sClient.Get(ctx, policyKey, policy)).To(Succeed())
		Expect(policy.DeletionTimestamp.IsZero()).To(BeFalse())

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: policyKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(snapshotExists(snapshotName(policyName, pvcName, time.Unix(1000, 0)))).To(BeFalse())
		Expect(snapshotExists(snapshotName(policyName, pvcName, time.Unix(500, 0)))).To(BeTrue())

		err = k8sClient.Get(ctx, policyKey, &opsv1alpha1.BackupPolicy{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})

func finishPolicy(ctx context.Context, key types.NamespacedName) {
	current := &opsv1alpha1.BackupPolicy{}
	err := k8sClient.Get(ctx, key, current)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	if controllerutil.ContainsFinalizer(current, backupPolicyFinalizer) {
		controllerutil.RemoveFinalizer(current, backupPolicyFinalizer)
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
	}
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, current))).To(Succeed())
}

func deleteSnapshotIfPresent(namespace, name string) {
	snapshot := &unstructured.Unstructured{}
	snapshot.SetNamespace(namespace)
	snapshot.SetName(name)
	snapshot.SetGroupVersionKind(volumeSnapshotGVK)
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, snapshot))).To(Succeed())
}
