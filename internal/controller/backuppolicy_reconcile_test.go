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
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

var _ = Describe("BackupPolicy reconciler", func() {
	const (
		policyName      = "daily"
		policyNamespace = "default"
		generation      = int64(2)
	)

	var (
		ctx    context.Context
		scheme *runtime.Scheme
		policy *opsv1alpha1.BackupPolicy
		key    types.NamespacedName
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(opsv1alpha1.AddToScheme(scheme)).To(Succeed())
		policy = newBackupPolicy(policyName, policyNamespace, generation)
		key = types.NamespacedName{Name: policyName, Namespace: policyNamespace}
	})

	It("ignores a missing BackupPolicy", func() {
		reconciler := newReconciler(fake.NewClientBuilder().WithScheme(scheme).Build())

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
	})

	It("returns the error when the BackupPolicy cannot be fetched", func() {
		fetchErr := errors.New("api unavailable")
		cl := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return fetchErr
			},
		}).Build()

		_, err := newReconciler(cl).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(fetchErr))
	})

	It("sets observedGeneration and an initial Ready condition", func() {
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&opsv1alpha1.BackupPolicy{}).
			WithObjects(policy).
			Build()
		reconciler := newReconciler(cl)

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(9 * time.Hour))

		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectReady(got)

		By("reconciling again without a new generation")
		result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(9 * time.Hour))

		unchanged := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, unchanged)).To(Succeed())
		expectReady(unchanged)
		Expect(unchanged.ResourceVersion).To(Equal(got.ResourceVersion))

		By("recording a newer generation")
		unchanged.Generation = generation + 1
		Expect(cl.Update(ctx, unchanged)).To(Succeed())

		result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(9 * time.Hour))

		bumped := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, bumped)).To(Succeed())
		Expect(bumped.Generation).To(Equal(generation + 1))
		expectReady(bumped)
	})
})

func newReconciler(cl client.Client) *BackupPolicyReconciler {
	return &BackupPolicyReconciler{
		Client: cl,
		Scheme: cl.Scheme(),
		Now:    func() time.Time { return time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC) },
	}
}

func newBackupPolicy(name, namespace string, generation int64) *opsv1alpha1.BackupPolicy {
	return &opsv1alpha1.BackupPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: generation,
		},
		Spec: opsv1alpha1.BackupPolicySpec{
			TargetSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "iris"},
			},
			Schedule: "0 0 * * *",
		},
	}
}

func expectReady(policy *opsv1alpha1.BackupPolicy) {
	Expect(policy.Status.ObservedGeneration).To(Equal(policy.Generation))

	ready := meta.FindStatusCondition(policy.Status.Conditions, opsv1alpha1.BackupPolicyConditionReady)
	Expect(ready).NotTo(BeNil())
	Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	Expect(ready.Reason).To(Equal(backupPolicyReasonReconciled))
	Expect(ready.Message).To(Equal(backupPolicyReconciledMessage))
	Expect(ready.ObservedGeneration).To(Equal(policy.Generation))
	Expect(ready.LastTransitionTime.IsZero()).To(BeFalse())

	degraded := meta.FindStatusCondition(policy.Status.Conditions, opsv1alpha1.BackupPolicyConditionDegraded)
	Expect(degraded).NotTo(BeNil())
	Expect(degraded.Status).To(Equal(metav1.ConditionFalse))
	Expect(degraded.Reason).To(Equal(backupPolicyReasonReconciled))
	Expect(degraded.ObservedGeneration).To(Equal(policy.Generation))
}
