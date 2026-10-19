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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

var _ = Describe("BackupPolicy status", func() {
	It("retries a conflicting status update", func() {
		scheme := runtime.NewScheme()
		Expect(opsv1alpha1.AddToScheme(scheme)).To(Succeed())
		policy := newBackupPolicy("conflict", 4)
		key := types.NamespacedName{Name: policy.Name, Namespace: policy.Namespace}
		base := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&opsv1alpha1.BackupPolicy{}).
			WithObjects(policy).
			Build()

		var calls int
		cl := interceptor.NewClient(base, interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if subResourceName == "status" {
					calls++
					if calls == 1 {
						return apierrors.NewConflict(schema.GroupResource{Group: "ops.leandro.ie", Resource: "backuppolicies"}, obj.GetName(), fmt.Errorf("conflict"))
					}
				}
				return c.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		})

		_, err := newReconciler(cl).Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(2))

		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(context.Background(), key, got)).To(Succeed())
		expectReady(got)
	})

	Describe("envtest paths", func() {
		const namespace = testNamespace

		AfterEach(func() {
			deleteLabeledSnapshots("status-create")
			deleteLabeledSnapshots("status-suspend")
			finishPolicy(ctx, types.NamespacedName{Name: "status-create", Namespace: namespace})
			finishPolicy(ctx, types.NamespacedName{Name: "status-suspend", Namespace: namespace})
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "status-create-data", Namespace: namespace}}))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "status-suspend-data", Namespace: namespace}}))).To(Succeed())
		})

		It("creates a VolumeSnapshot and records a successful backup", func() {
			policy := createStatusPolicy("status-create", false)
			Expect(k8sClient.Create(ctx, backupPVC("status-create-data"))).To(Succeed())

			events := events.NewFakeRecorder(10)
			now := policy.CreationTimestamp.Add(48 * time.Hour)
			_, err := (&BackupPolicyReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events,
				Now:      func() time.Time { return now },
			}).Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: policy.Name, Namespace: policy.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			got := &opsv1alpha1.BackupPolicy{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), got)).To(Succeed())
			expectReady(got)
			Expect(got.Status.LastSuccessfulBackupTime).NotTo(BeNil())
			Expect(got.Status.LastSuccessfulBackupTime.Time).To(BeTemporally("==", now))
			Expect(ownedSnapshotCount(got)).To(Equal(1))
			Expect(readEvents(events)).To(ContainElement(ContainSubstring("SnapshotCreated")))
		})

		It("does not create a snapshot while the policy is suspended", func() {
			policy := createStatusPolicy("status-suspend", true)
			Expect(k8sClient.Create(ctx, backupPVC("status-suspend-data"))).To(Succeed())

			events := events.NewFakeRecorder(10)
			now := policy.CreationTimestamp.Add(48 * time.Hour)
			result, err := (&BackupPolicyReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events,
				Now:      func() time.Time { return now },
			}).Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: policy.Name, Namespace: policy.Namespace}})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			got := &opsv1alpha1.BackupPolicy{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), got)).To(Succeed())
			expectCondition(got, opsv1alpha1.BackupPolicyConditionReady, metav1.ConditionTrue, backupPolicyReasonSuspended)
			expectCondition(got, opsv1alpha1.BackupPolicyConditionDegraded, metav1.ConditionFalse, backupPolicyReasonSuspended)
			Expect(got.Status.LastSuccessfulBackupTime).To(BeNil())
			Expect(ownedSnapshotCount(got)).To(Equal(0))
			Expect(readEvents(events)).NotTo(ContainElement(ContainSubstring("SnapshotCreated")))
		})
	})
})

func createStatusPolicy(name string, suspend bool) *opsv1alpha1.BackupPolicy {
	policy := newBackupPolicy(name, 1)
	if suspend {
		policy.Spec.Suspend = &suspend
	}
	Expect(k8sClient.Create(ctx, policy)).To(Succeed())
	created := &opsv1alpha1.BackupPolicy{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, created)).To(Succeed())
	return created
}

func backupPVC(name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    map[string]string{labelApp: appIris},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
}

func ownedSnapshotCount(policy *opsv1alpha1.BackupPolicy) int {
	owned, err := (&BackupPolicyReconciler{Client: k8sClient}).listOwnedSnapshots(ctx, policy)
	Expect(err).NotTo(HaveOccurred())
	return len(owned)
}

func deleteLabeledSnapshots(policyName string) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(volumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"))
	Expect(k8sClient.List(ctx, list, client.InNamespace(testNamespace), client.MatchingLabels{labelBackupPolicy: policyName})).To(Succeed())
	for i := range list.Items {
		list.Items[i].SetGroupVersionKind(volumeSnapshotGVK)
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &list.Items[i]))).To(Succeed())
	}
}

func readEvents(recorder *events.FakeRecorder) []string {
	var got []string
	for {
		select {
		case event := <-recorder.Events:
			got = append(got, event)
		default:
			return got
		}
	}
}
