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
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

func TestSnapshotName(t *testing.T) {
	scheduled := time.Unix(10, 0).UTC()
	if got := snapshotName("daily", "data", scheduled); got != "daily-data-10" {
		t.Fatalf("snapshotName() = %s, want daily-data-10", got)
	}

	long := snapshotName(strings.Repeat("a", 200), strings.Repeat("b", 200), scheduled)
	if len(long) > 253 {
		t.Fatalf("snapshotName() length = %d, want at most 253", len(long))
	}
	if !strings.HasPrefix(long, "backup-") {
		t.Fatalf("snapshotName() = %s, want a backup- hash", long)
	}
}

var _ = Describe("BackupPolicy snapshots", func() {
	const (
		policyName      = "daily"
		policyNamespace = "default"
		policyUID       = types.UID("policy-uid")
	)

	sundayAfternoon := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
	mondayMidnight := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	mondayMorning := time.Date(2026, 10, 19, 0, 30, 0, 0, time.UTC)

	var (
		ctx       context.Context
		scheme    *runtime.Scheme
		policy    *opsv1alpha1.BackupPolicy
		key       types.NamespacedName
		class     string
		matching  *corev1.PersistentVolumeClaim
		other     *corev1.PersistentVolumeClaim
		elsewhere *corev1.PersistentVolumeClaim
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(opsv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		class = "csi-hostpath-snapclass"
		policy = newBackupPolicy(policyName, policyNamespace, 1)
		policy.UID = policyUID
		policy.CreationTimestamp = metav1.NewTime(sundayAfternoon)
		policy.Spec.SnapshotClassName = &class
		key = types.NamespacedName{Name: policyName, Namespace: policyNamespace}

		matching = pvc(policyNamespace, "data", "iris")
		other = pvc(policyNamespace, "other", "web")
		elsewhere = pvc("other", "data", "iris")
	})

	It("creates a VolumeSnapshot for each selected PVC when a run is due", func() {
		cl := snapshotClient(scheme, policy, matching, other, elsewhere)
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(23*time.Hour + 30*time.Minute))

		snapshots := listSnapshots(ctx, cl)
		Expect(snapshots).To(HaveLen(1))
		snapshot := &snapshots[0]
		Expect(snapshot.GetName()).To(Equal(fmt.Sprintf("daily-data-%d", mondayMidnight.Unix())))
		Expect(snapshot.GetNamespace()).To(Equal(policyNamespace))
		Expect(snapshot.GetLabels()).To(Equal(map[string]string{
			labelBackupPolicy: policyName,
			labelBackupPVC:    matching.Name,
			labelScheduledAt:  fmt.Sprintf("%d", mondayMidnight.Unix()),
		}))
		Expect(snapshot.GetAnnotations()).To(HaveKeyWithValue(annotationPVC, matching.Name))
		Expect(snapshot.GetAnnotations()).To(HaveKeyWithValue(annotationScheduledAt, mondayMidnight.Format(time.RFC3339)))

		refs := snapshot.GetOwnerReferences()
		Expect(refs).To(HaveLen(1))
		Expect(refs[0].APIVersion).To(Equal("ops.leandro.ie/v1alpha1"))
		Expect(refs[0].Kind).To(Equal("BackupPolicy"))
		Expect(refs[0].Name).To(Equal(policyName))
		Expect(refs[0].UID).To(Equal(policyUID))
		Expect(refs[0].Controller).NotTo(BeNil())
		Expect(*refs[0].Controller).To(BeTrue())

		pvcName, found, err := unstructured.NestedString(snapshot.Object, "spec", "source", "persistentVolumeClaimName")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(pvcName).To(Equal(matching.Name))
		className, found, err := unstructured.NestedString(snapshot.Object, "spec", "volumeSnapshotClassName")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(className).To(Equal(class))

		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.LastSuccessfulBackupTime).NotTo(BeNil())
		Expect(got.Status.LastSuccessfulBackupTime.Time).To(BeTemporally("==", mondayMorning))

		By("reconciling the same due slot again")
		cleared := got.DeepCopy()
		cleared.Status.LastSuccessfulBackupTime = nil
		Expect(cl.Status().Update(ctx, cleared)).To(Succeed())

		result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(23*time.Hour + 30*time.Minute))
		Expect(listSnapshots(ctx, cl)).To(HaveLen(1))
	})

	It("omits the snapshot class when the policy does not set one", func() {
		policy.Spec.SnapshotClassName = nil
		cl := snapshotClient(scheme, policy, matching)
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		snapshots := listSnapshots(ctx, cl)
		Expect(snapshots).To(HaveLen(1))
		_, found, err := unstructured.NestedString(snapshots[0].Object, "spec", "volumeSnapshotClassName")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
	})

	It("does not create snapshots while the policy is suspended", func() {
		suspend := true
		policy.Spec.Suspend = &suspend
		cl := snapshotClient(scheme, policy, matching)
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
		Expect(listSnapshots(ctx, cl)).To(BeEmpty())

		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.LastSuccessfulBackupTime).To(BeNil())
		expectCondition(got, opsv1alpha1.BackupPolicyConditionReady, metav1.ConditionTrue, backupPolicyReasonSuspended)
		expectCondition(got, opsv1alpha1.BackupPolicyConditionDegraded, metav1.ConditionFalse, backupPolicyReasonSuspended)
	})

	It("returns the schedule error and creates no snapshots", func() {
		policy.Spec.Schedule = "60 0 * * *"
		cl := snapshotClient(scheme, policy, matching)
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(ContainSubstring("parse schedule")))
		Expect(listSnapshots(ctx, cl)).To(BeEmpty())

		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectDegraded(got)
	})

	It("records a run when no PVC matches and does not create a snapshot", func() {
		cl := snapshotClient(scheme, policy, other)
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(23*time.Hour + 30*time.Minute))
		Expect(listSnapshots(ctx, cl)).To(BeEmpty())

		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.LastSuccessfulBackupTime).NotTo(BeNil())
	})

	It("returns the error when a VolumeSnapshot cannot be created", func() {
		cl := snapshotClient(scheme, policy, matching)
		cl = interceptor.NewClient(cl.(client.WithWatch), interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*unstructured.Unstructured); ok {
					return fmt.Errorf("snapshot create failed")
				}
				return c.Create(ctx, obj, opts...)
			},
		})
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(ContainSubstring("snapshot create failed")))

		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.LastSuccessfulBackupTime).To(BeNil())
		expectDegraded(got)
	})
})

func expectCondition(policy *opsv1alpha1.BackupPolicy, conditionType string, status metav1.ConditionStatus, reason string) {
	condition := meta.FindStatusCondition(policy.Status.Conditions, conditionType)
	Expect(condition).NotTo(BeNil())
	Expect(condition.Status).To(Equal(status))
	Expect(condition.Reason).To(Equal(reason))
	Expect(condition.ObservedGeneration).To(Equal(policy.Generation))
}

func expectDegraded(policy *opsv1alpha1.BackupPolicy) {
	expectCondition(policy, opsv1alpha1.BackupPolicyConditionReady, metav1.ConditionFalse, backupPolicyReasonBackupFailed)
	expectCondition(policy, opsv1alpha1.BackupPolicyConditionDegraded, metav1.ConditionTrue, backupPolicyReasonBackupFailed)
}

func snapshotClient(scheme *runtime.Scheme, objects ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&opsv1alpha1.BackupPolicy{}).
		WithObjects(objects...).
		Build()
}

func pvc(namespace, name, app string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": app},
		},
	}
}

func listSnapshots(ctx context.Context, cl client.Client) []unstructured.Unstructured {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   volumeSnapshotGVK.Group,
		Version: volumeSnapshotGVK.Version,
		Kind:    "VolumeSnapshotList",
	})
	Expect(cl.List(ctx, list)).To(Succeed())
	return list.Items
}
