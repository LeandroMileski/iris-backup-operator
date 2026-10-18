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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

func TestVerificationPodResult(t *testing.T) {
	succeeded := verificationPod(corev1.PodSucceeded, 0, "Completed")
	failed := verificationPod(corev1.PodFailed, 2, "Error")
	deadline := verificationPod(corev1.PodFailed, 0, podReasonDeadlineExceeded)
	deadline.Status.Reason = podReasonDeadlineExceeded
	evicted := &corev1.Pod{}
	evicted.Status.Phase = corev1.PodFailed
	evicted.Status.Reason = "Evicted"
	running := &corev1.Pod{}
	running.Status.Phase = corev1.PodRunning

	tests := []struct {
		name      string
		pod       *corev1.Pod
		done      bool
		succeeded bool
		message   string
	}{
		{name: "pending", pod: &corev1.Pod{}},
		{name: "running", pod: running},
		{name: "exit 0", pod: succeeded, done: true, succeeded: true, message: "Verification command exited 0"},
		{name: "exit 2", pod: failed, done: true, message: "Verification command exited 2"},
		{name: "deadline", pod: deadline, done: true, message: verificationTimeoutMessage},
		{name: "evicted", pod: evicted, done: true, message: "Verification Pod failed: Evicted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, ok, message := verificationPodResult(tt.pod)
			if done != tt.done || ok != tt.succeeded || message != tt.message {
				t.Fatalf("verificationPodResult() = (%v, %v, %q), want (%v, %v, %q)", done, ok, message, tt.done, tt.succeeded, tt.message)
			}
		})
	}
}

var _ = Describe("RestoreVerification result", func() {
	const (
		resourceName      = "test-result"
		resourceNamespace = "default"
	)

	ctx := context.Background()
	key := types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}
	now := time.Date(2026, 10, 18, 17, 0, 0, 0, time.UTC)

	BeforeEach(func() {
		err := k8sClient.Get(ctx, key, &opsv1alpha1.RestoreVerification{})
		if err != nil && apierrors.IsNotFound(err) {
			resource := newRestoreVerification(resourceName, resourceNamespace, 0)
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
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: resourceNamespace}}))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, restoreSnapshot(resourceNamespace)))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &opsv1alpha1.RestoreVerification{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: resourceNamespace}}))).To(Succeed())
	})

	It("records exit code 0 and deletes the Pod and restored PersistentVolumeClaim", func() {
		reconciler := &RestoreVerificationReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Now:    func() time.Time { return now },
		}
		Expect(reconcileUntilRunning(ctx, reconciler, key)).To(Succeed())
		Expect(setPodResult(ctx, k8sClient, restoreObjectName(resourceName), corev1.PodSucceeded, 0, "Completed")).To(Succeed())

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
		expectFinishedVerification(ctx, key, opsv1alpha1.RestoreVerificationPhaseSucceeded, verificationExitMessage(0), now)
		expectRestoreObjectsGone(ctx, resourceName)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "data", Namespace: resourceNamespace}, &corev1.PersistentVolumeClaim{})).To(Succeed())
	})

	It("records a non-zero exit code and still deletes the Pod and restored PersistentVolumeClaim", func() {
		reconciler := &RestoreVerificationReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Now:    func() time.Time { return now },
		}
		Expect(reconcileUntilRunning(ctx, reconciler, key)).To(Succeed())
		Expect(setPodResult(ctx, k8sClient, restoreObjectName(resourceName), corev1.PodFailed, 2, "Error")).To(Succeed())

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		expectFinishedVerification(ctx, key, opsv1alpha1.RestoreVerificationPhaseFailed, verificationExitMessage(2), now)
		expectRestoreObjectsGone(ctx, resourceName)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "data", Namespace: resourceNamespace}, &corev1.PersistentVolumeClaim{})).To(Succeed())
	})
})

var _ = Describe("RestoreVerification result reconciler", func() {
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
		verification = newRestoreVerification("check-data", "default", 1)
		verification.Status.Phase = opsv1alpha1.RestoreVerificationPhaseRunning
		key = types.NamespacedName{Name: verification.Name, Namespace: verification.Namespace}
		now = time.Date(2026, 10, 18, 17, 15, 0, 0, time.UTC)
	})

	It("keeps a running Pod", func() {
		pod := verificationPod(corev1.PodRunning, 0, "")
		pod.Name = restoreObjectName(verification.Name)
		pod.Namespace = verification.Namespace
		cl := restoreClient(scheme, verification, restoreSourcePVC(), restoreSnapshot(verification.Namespace), pod, namedRestorePVC(verification))

		_, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		got := &opsv1alpha1.RestoreVerification{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectVerificationPhase(got, opsv1alpha1.RestoreVerificationPhaseRunning)
		Expect(cl.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).To(Succeed())
		Expect(cl.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.PersistentVolumeClaim{})).To(Succeed())
	})

	It("retries cleanup while the restored PersistentVolumeClaim still exists", func() {
		pod := finishedVerificationPod(verification, corev1.PodSucceeded, 0, "Completed")
		base := restoreClient(scheme, verification, restoreSourcePVC(), restoreSnapshot(verification.Namespace), pod, namedRestorePVC(verification))
		cl := interceptor.NewClient(base, interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return nil
			},
		})

		result, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(5 * time.Second))

		got := &opsv1alpha1.RestoreVerification{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectVerificationPhase(got, opsv1alpha1.RestoreVerificationPhaseSucceeded)
		Expect(got.Status.Message).To(Equal(verificationExitMessage(0)))
		Expect(cl.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).To(Succeed())
	})

	It("deletes a leftover Pod after the result is already recorded", func() {
		verification.Status.Phase = opsv1alpha1.RestoreVerificationPhaseFailed
		verification.Status.Message = "already finished"
		finished := metav1.NewTime(now.Add(-time.Minute))
		verification.Status.CompletionTime = &finished
		applyVerificationStatus(verification, opsv1alpha1.RestoreVerificationPhaseFailed, "", finished.Time)
		verification.Status.Message = "already finished"
		pod := finishedVerificationPod(verification, corev1.PodFailed, 2, "Error")
		cl := restoreClient(scheme, verification, pod, namedRestorePVC(verification))

		result, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		got := &opsv1alpha1.RestoreVerification{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(opsv1alpha1.RestoreVerificationPhaseFailed))
		Expect(got.Status.Message).To(Equal("already finished"))
		Expect(got.Status.CompletionTime.Time).To(BeTemporally("==", finished.Time))
		Expect(apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.PersistentVolumeClaim{}))).To(BeTrue())
	})
})

func verificationPod(phase corev1.PodPhase, exitCode int32, reason string) *corev1.Pod {
	pod := &corev1.Pod{}
	pod.Status.Phase = phase
	if phase == corev1.PodPending || phase == corev1.PodRunning || phase == "" {
		return pod
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: restoreContainerName,
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: exitCode, Reason: reason},
		},
	}}
	return pod
}

func finishedVerificationPod(verification *opsv1alpha1.RestoreVerification, phase corev1.PodPhase, exitCode int32, reason string) *corev1.Pod {
	pod := verificationPod(phase, exitCode, reason)
	pod.Name = restoreObjectName(verification.Name)
	pod.Namespace = verification.Namespace
	return pod
}

func namedRestorePVC(verification *opsv1alpha1.RestoreVerification) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name:      restoreObjectName(verification.Name),
		Namespace: verification.Namespace,
	}}
}

func reconcileUntilRunning(ctx context.Context, reconciler *RestoreVerificationReconciler, key types.NamespacedName) error {
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		return err
	}
	_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
	return err
}

func setPodResult(ctx context.Context, cl client.Client, name string, phase corev1.PodPhase, exitCode int32, reason string) error {
	pod := &corev1.Pod{}
	if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, pod); err != nil {
		return err
	}
	pod.Status.Phase = phase
	if reason == podReasonDeadlineExceeded {
		pod.Status.Reason = reason
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: restoreContainerName,
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: exitCode, Reason: reason},
		},
	}}
	return cl.Status().Update(ctx, pod)
}

func expectFinishedVerification(ctx context.Context, key types.NamespacedName, phase, message string, now time.Time) {
	got := &opsv1alpha1.RestoreVerification{}
	Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
	expectVerificationPhase(got, phase)
	Expect(got.Status.Message).To(Equal(message))
	Expect(got.Status.CompletionTime).NotTo(BeNil())
	Expect(got.Status.CompletionTime.Time).To(BeTemporally("==", now))
	Expect(got.Status.StartTime).NotTo(BeNil())
}

func expectRestoreObjectsGone(ctx context.Context, verificationName string) {
	key := types.NamespacedName{Name: restoreObjectName(verificationName), Namespace: "default"}
	expectDeleted(ctx, key, &corev1.Pod{})
	expectDeleted(ctx, key, &corev1.PersistentVolumeClaim{})
	snapshot := restoreSnapshot("default")
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(snapshot), snapshot)).To(Succeed())
}

func expectDeleted(ctx context.Context, key types.NamespacedName, obj client.Object) {
	err := k8sClient.Get(ctx, key, obj)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(obj.GetDeletionTimestamp().IsZero()).To(BeFalse())
}
