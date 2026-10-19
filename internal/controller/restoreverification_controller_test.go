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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

var _ = Describe("RestoreVerification Controller", func() {
	const (
		resourceName      = "test-resource"
		resourceNamespace = testNamespace
	)

	ctx := context.Background()
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: resourceNamespace,
	}

	BeforeEach(func() {
		By("creating the custom resource for the Kind RestoreVerification")
		err := k8sClient.Get(ctx, typeNamespacedName, &opsv1alpha1.RestoreVerification{})
		if err != nil && apierrors.IsNotFound(err) {
			resource := newRestoreVerification(resourceName, 0)
			timeout := int32(30)
			resource.Spec.Verification.TimeoutSeconds = &timeout
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, restoreSourcePVC()))).To(Succeed())
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, restoreSnapshot(resourceNamespace)))).To(Succeed())
	})

	AfterEach(func() {
		restoreName := restoreObjectName(resourceName)
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: restoreName, Namespace: resourceNamespace}}))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: restoreName, Namespace: resourceNamespace}}))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: restoreVolumeName, Namespace: resourceNamespace}}))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, restoreSnapshot(resourceNamespace)))).To(Succeed())

		resource := &opsv1alpha1.RestoreVerification{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		Expect(err).NotTo(HaveOccurred())

		By("Cleanup the specific resource instance RestoreVerification")
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("moves an empty phase to Pending and then restores a read-only volume", func() {
		now := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
		reconciler := &RestoreVerificationReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Now:    func() time.Time { return now },
		}

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(verificationPendingRequeue))

		pending := &opsv1alpha1.RestoreVerification{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, pending)).To(Succeed())
		expectVerificationPhase(pending, opsv1alpha1.RestoreVerificationPhasePending)
		Expect(pending.Status.StartTime).To(BeNil())
		Expect(pending.Status.ObservedGeneration).To(Equal(pending.Generation))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, restoreKey(), &corev1.PersistentVolumeClaim{}))).To(BeTrue())

		result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		running := &opsv1alpha1.RestoreVerification{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, running)).To(Succeed())
		expectVerificationPhase(running, opsv1alpha1.RestoreVerificationPhaseRunning)
		Expect(running.Status.StartTime).NotTo(BeNil())
		Expect(running.Status.StartTime.Time).To(BeTemporally("==", now))
		Expect(running.Status.CompletionTime).To(BeNil())
		started := running.ResourceVersion

		restored := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, restoreKey(), restored)).To(Succeed())
		expectRestorePVC(restored, running)
		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, restoreKey(), pod)).To(Succeed())
		expectRestorePod(pod, running)
		claimVersion := restored.ResourceVersion
		podVersion := pod.ResourceVersion

		result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		again := &opsv1alpha1.RestoreVerification{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, again)).To(Succeed())
		Expect(again.ResourceVersion).To(Equal(started))
		Expect(again.Status.StartTime.Time).To(BeTemporally("==", now))

		Expect(k8sClient.Get(ctx, restoreKey(), restored)).To(Succeed())
		Expect(k8sClient.Get(ctx, restoreKey(), pod)).To(Succeed())
		Expect(restored.ResourceVersion).To(Equal(claimVersion))
		Expect(pod.ResourceVersion).To(Equal(podVersion))
	})
})

var _ = Describe("RestoreVerification reconciler", func() {
	const generation = int64(3)

	var (
		ctx          context.Context
		scheme       *runtime.Scheme
		verification *opsv1alpha1.RestoreVerification
		key          types.NamespacedName
		now          time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(opsv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(networkingv1.AddToScheme(scheme)).To(Succeed())
		verification = newRestoreVerification("check-data", generation)
		key = types.NamespacedName{Name: verification.Name, Namespace: verification.Namespace}
		now = time.Date(2026, 10, 18, 16, 30, 0, 0, time.UTC)
	})

	It("ignores a missing RestoreVerification", func() {
		result, err := newVerificationReconciler(fake.NewClientBuilder().WithScheme(scheme).Build(), now).
			Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
	})

	It("returns the error when the RestoreVerification cannot be fetched", func() {
		fetchErr := errors.New("api unavailable")
		cl := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return fetchErr
			},
		}).Build()

		_, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(fetchErr))
	})

	It("returns the error for an unknown phase", func() {
		verification.Status.Phase = "Bogus"
		cl := verificationClient(scheme, verification)

		_, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())

		got := &opsv1alpha1.RestoreVerification{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal("Bogus"))
	})

	It("leaves Succeeded and Failed unchanged", func() {
		finished := metav1.NewTime(now.Add(-time.Hour))
		for _, phase := range []string{
			opsv1alpha1.RestoreVerificationPhaseSucceeded,
			opsv1alpha1.RestoreVerificationPhaseFailed,
		} {
			object := newRestoreVerification(phase, generation)
			object.Status.Phase = phase
			object.Status.Message = finishedMessage
			object.Status.CompletionTime = &finished
			applyVerificationStatus(object, phase, "", finished.Time)
			cl := verificationClient(scheme, object)
			objectKey := types.NamespacedName{Name: object.Name, Namespace: object.Namespace}

			before := &opsv1alpha1.RestoreVerification{}
			Expect(cl.Get(ctx, objectKey, before)).To(Succeed())

			result, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: objectKey})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			got := &opsv1alpha1.RestoreVerification{}
			Expect(cl.Get(ctx, objectKey, got)).To(Succeed())
			Expect(got.ResourceVersion).To(Equal(before.ResourceVersion))
			Expect(got.Status.Phase).To(Equal(phase))
			Expect(got.Status.Message).To(Equal(finishedMessage))
			Expect(got.Status.CompletionTime.Time).To(BeTemporally("==", finished.Time))
		}
	})

	It("records Succeeded when a running verification finishes", func() {
		verification.Status.Phase = opsv1alpha1.RestoreVerificationPhaseRunning
		cl := verificationClient(scheme, verification)

		Expect(finishVerification(ctx, newVerificationReconciler(cl, now), key, true)).To(Succeed())

		got := &opsv1alpha1.RestoreVerification{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectVerificationPhase(got, opsv1alpha1.RestoreVerificationPhaseSucceeded)
		Expect(got.Status.CompletionTime).NotTo(BeNil())
		Expect(got.Status.CompletionTime.Time).To(BeTemporally("==", now))
	})

	It("records Failed when a running verification finishes unsuccessfully", func() {
		verification.Status.Phase = opsv1alpha1.RestoreVerificationPhaseRunning
		cl := verificationClient(scheme, verification)

		Expect(finishVerification(ctx, newVerificationReconciler(cl, now), key, false)).To(Succeed())

		got := &opsv1alpha1.RestoreVerification{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectVerificationPhase(got, opsv1alpha1.RestoreVerificationPhaseFailed)
		Expect(got.Status.CompletionTime.Time).To(BeTemporally("==", now))
	})

	It("retries a conflicting status update", func() {
		base := verificationClient(scheme, verification)
		var calls int
		cl := interceptor.NewClient(base, interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if subResourceName == "status" {
					calls++
					if calls == 1 {
						return apierrors.NewConflict(schema.GroupResource{Group: "ops.leandro.ie", Resource: "restoreverifications"}, obj.GetName(), fmt.Errorf("conflict"))
					}
				}
				return c.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		})

		_, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(2))

		got := &opsv1alpha1.RestoreVerification{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectVerificationPhase(got, opsv1alpha1.RestoreVerificationPhasePending)
	})
})

func newRestoreVerification(name string, generation int64) *opsv1alpha1.RestoreVerification {
	return &opsv1alpha1.RestoreVerification{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  testNamespace,
			Generation: generation,
		},
		Spec: opsv1alpha1.RestoreVerificationSpec{
			SnapshotName:              "data-snapshot",
			PersistentVolumeClaimName: restoreVolumeName,
			Verification: opsv1alpha1.Verification{
				Image:   "busybox:1.36",
				Command: []string{"test", "-f", "/data/ok"},
			},
		},
	}
}

func newVerificationReconciler(cl client.Client, now time.Time) *RestoreVerificationReconciler {
	return &RestoreVerificationReconciler{
		Client: cl,
		Scheme: cl.Scheme(),
		Now:    func() time.Time { return now },
	}
}

func verificationClient(scheme *runtime.Scheme, verification *opsv1alpha1.RestoreVerification) client.WithWatch {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&opsv1alpha1.RestoreVerification{}).
		WithObjects(verification).
		Build()
}

// finishVerification applies the terminal step the pod result will use.
func finishVerification(ctx context.Context, reconciler *RestoreVerificationReconciler, key types.NamespacedName, succeeded bool) error {
	verification := &opsv1alpha1.RestoreVerification{}
	if err := reconciler.Get(ctx, key, verification); err != nil {
		return err
	}
	next, err := advanceVerification(verification.Status.Phase, true, succeeded)
	if err != nil {
		return err
	}
	return reconciler.persistVerification(ctx, verification, next, "")
}

func expectVerificationPhase(verification *opsv1alpha1.RestoreVerification, phase string) {
	Expect(verification.Status.Phase).To(Equal(phase))
	ready := metav1.ConditionFalse
	degraded := metav1.ConditionFalse
	switch phase {
	case opsv1alpha1.RestoreVerificationPhaseSucceeded:
		ready = metav1.ConditionTrue
	case opsv1alpha1.RestoreVerificationPhaseFailed:
		degraded = metav1.ConditionTrue
	}
	expectVerificationCondition(verification, opsv1alpha1.RestoreVerificationConditionReady, ready, phase)
	expectVerificationCondition(verification, opsv1alpha1.RestoreVerificationConditionDegraded, degraded, phase)
}

func expectVerificationCondition(verification *opsv1alpha1.RestoreVerification, conditionType string, status metav1.ConditionStatus, reason string) {
	condition := meta.FindStatusCondition(verification.Status.Conditions, conditionType)
	Expect(condition).NotTo(BeNil())
	Expect(condition.Status).To(Equal(status))
	Expect(condition.Reason).To(Equal(reason))
	Expect(condition.ObservedGeneration).To(Equal(verification.Generation))
}
