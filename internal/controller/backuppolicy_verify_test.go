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
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

func TestVerificationObjectName(t *testing.T) {
	scheduled := time.Unix(10, 0).UTC()
	if got := verificationObjectName("daily", restoreVolumeName, scheduled); got != "verify-daily-data-10" {
		t.Fatalf("verificationObjectName() = %s, want verify-daily-data-10", got)
	}
	long := verificationObjectName(strings.Repeat("a", 200), strings.Repeat("b", 200), scheduled)
	if len(long) > 253 || !strings.HasPrefix(long, "verify-") {
		t.Fatalf("verificationObjectName() = %s", long)
	}
}

var _ = Describe("BackupPolicy verification", func() {
	const (
		policyName      = "daily"
		policyNamespace = testNamespace
		policyUID       = types.UID("policy-uid")
	)

	sundayAfternoon := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
	mondayMorning := time.Date(2026, 10, 19, 0, 30, 0, 0, time.UTC)
	tuesdayMorning := time.Date(2026, 10, 20, 0, 30, 0, 0, time.UTC)

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
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		policy = newBackupPolicy(policyName, 1)
		policy.UID = policyUID
		policy.CreationTimestamp = metav1.NewTime(sundayAfternoon)
		key = types.NamespacedName{Name: policyName, Namespace: policyNamespace}
	})

	It("creates a RestoreVerification for the snapshot when verifyRestore is true", func() {
		enableVerification(policy, []string{cmdFalse})
		cl := policyClient(scheme, policy, pvc(policyNamespace, restoreVolumeName, appIris), pvc(policyNamespace, "other", "web"))
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		got := listVerifications(ctx, cl)
		Expect(got).To(HaveLen(1))
		verification := got[0]
		Expect(verification.Name).To(Equal(verificationObjectName(policyName, restoreVolumeName, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))))
		Expect(verification.Spec.SnapshotName).To(Equal(snapshotName(policyName, restoreVolumeName, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))))
		Expect(verification.Spec.PersistentVolumeClaimName).To(Equal(restoreVolumeName))
		Expect(verification.Spec.Verification.Command).To(Equal([]string{cmdFalse}))
		Expect(metav1.IsControlledBy(&verification, policy)).To(BeTrue())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(listVerifications(ctx, cl)).To(HaveLen(1))
	})

	It("does not create a RestoreVerification when verifyRestore is unset", func() {
		cl := policyClient(scheme, policy, pvc(policyNamespace, restoreVolumeName, appIris))
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(listVerifications(ctx, cl)).To(BeEmpty())
	})

	It("returns an error when verifyRestore is true and verification is missing", func() {
		enabled := true
		policy.Spec.VerifyRestore = &enabled
		cl := policyClient(scheme, policy, pvc(policyNamespace, restoreVolumeName, appIris))
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(ContainSubstring("verification is required")))

		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.LastSuccessfulBackupTime).To(BeNil())
		expectDegraded(got)
	})

	It("marks the policy Degraded when the latest verification failed and recovers when a newer one succeeds", func() {
		enableVerification(policy, []string{cmdFalse})
		cl := policyClient(scheme, policy, pvc(policyNamespace, restoreVolumeName, appIris))
		now := mondayMorning
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return now }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		failed := listVerifications(ctx, cl)
		Expect(failed).To(HaveLen(1))
		finishOwnedVerification(ctx, cl, &failed[0], opsv1alpha1.RestoreVerificationPhaseFailed, mondayMorning, verificationExitMessage(1))

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectVerificationFailed(got, failed[0].Name, verificationExitMessage(1))

		Expect(cl.Get(ctx, key, policy)).To(Succeed())
		policy.Spec.Verification.Command = []string{cmdTrue}
		Expect(cl.Update(ctx, policy)).To(Succeed())
		now = tuesdayMorning
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		created := listVerifications(ctx, cl)
		Expect(created).To(HaveLen(2))
		var recovered *opsv1alpha1.RestoreVerification
		for i := range created {
			if created[i].Name != failed[0].Name {
				recovered = &created[i]
			}
		}
		Expect(recovered).NotTo(BeNil())
		Expect(recovered.Spec.Verification.Command).To(Equal([]string{cmdTrue}))

		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectVerificationFailed(got, failed[0].Name, verificationExitMessage(1))

		finishOwnedVerification(ctx, cl, recovered, opsv1alpha1.RestoreVerificationPhaseSucceeded, tuesdayMorning, verificationExitMessage(0))
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectReady(got)
		Expect(got.Status.LastVerification.Name).To(Equal(recovered.Name))
		Expect(got.Status.LastVerification.Result).To(Equal(opsv1alpha1.RestoreVerificationPhaseSucceeded))
		Expect(got.Status.LastVerification.Message).To(Equal(verificationExitMessage(0)))
	})

	It("keeps a failed result while a newer verification is still running", func() {
		enableVerification(policy, []string{cmdFalse})
		policy.Status.LastSuccessfulBackupTime = &metav1.Time{Time: mondayMorning}
		failedVerification := finishedPolicyVerification(policy, restoreVolumeName, mondayMorning, opsv1alpha1.RestoreVerificationPhaseFailed, verificationExitMessage(1))
		runningVerification := finishedPolicyVerification(policy, restoreVolumeName, tuesdayMorning, opsv1alpha1.RestoreVerificationPhaseRunning, "")
		runningVerification.Status.Phase = opsv1alpha1.RestoreVerificationPhaseRunning
		runningVerification.Status.CompletionTime = nil
		Expect(controllerutil.SetControllerReference(policy, failedVerification, scheme)).To(Succeed())
		Expect(controllerutil.SetControllerReference(policy, runningVerification, scheme)).To(Succeed())

		cl := policyClient(scheme, policy, failedVerification, runningVerification)
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning.Add(12 * time.Hour) }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectVerificationFailed(got, failedVerification.Name, verificationExitMessage(1))
	})

	It("ignores a verification owned by another policy", func() {
		other := newBackupPolicy("other", 1)
		other.UID = "other-uid"
		enableVerification(other, []string{cmdFalse})
		foreign := finishedPolicyVerification(other, restoreVolumeName, mondayMorning, opsv1alpha1.RestoreVerificationPhaseFailed, verificationExitMessage(1))
		Expect(controllerutil.SetControllerReference(other, foreign, scheme)).To(Succeed())
		cl := policyClient(scheme, policy, foreign)
		reconciler := newReconciler(cl)
		reconciler.Now = func() time.Time { return mondayMorning }

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		got := &opsv1alpha1.BackupPolicy{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		expectReady(got)
		Expect(got.Status.LastVerification).To(BeNil())
	})
})

var _ = Describe("BackupPolicy verification envtest", func() {
	const (
		policyName = "verify-daily"
		namespace  = testNamespace
		pvcName    = "verify-source"
	)

	var key types.NamespacedName

	BeforeEach(func() {
		key = types.NamespacedName{Name: policyName, Namespace: namespace}
		policy := newBackupPolicy(policyName, 0)
		policy.Spec.TargetSelector.MatchLabels = map[string]string{labelApp: policyName}
		enableVerification(policy, []string{cmdFalse})
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		claim := backupPVC(pvcName)
		claim.Labels = map[string]string{labelApp: policyName}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
	})

	AfterEach(func() {
		deletePolicyVerifications(namespace, policyName)
		deleteLabeledSnapshots(policyName)
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: namespace}}))).To(Succeed())
		finishPolicy(ctx, key)
	})

	It("records a failed verification as Degraded and a newer success as recovered", func() {
		created := &opsv1alpha1.BackupPolicy{}
		Expect(k8sClient.Get(ctx, key, created)).To(Succeed())
		now := created.CreationTimestamp.Add(48 * time.Hour)
		reconciler := &BackupPolicyReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Now:    func() time.Time { return now },
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		verifications := verificationsFor(ctx, k8sClient, namespace, policyName)
		Expect(verifications).To(HaveLen(1))
		Expect(verifications[0].Spec.Verification.Command).To(Equal([]string{cmdFalse}))
		finishOwnedVerification(ctx, k8sClient, &verifications[0], opsv1alpha1.RestoreVerificationPhaseFailed, now, verificationExitMessage(1))

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		got := &opsv1alpha1.BackupPolicy{}
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		expectVerificationFailed(got, verifications[0].Name, verificationExitMessage(1))

		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		got.Spec.Verification.Command = []string{cmdTrue}
		Expect(k8sClient.Update(ctx, got)).To(Succeed())
		now = now.Add(48 * time.Hour)
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		verifications = verificationsFor(ctx, k8sClient, namespace, policyName)
		Expect(verifications).To(HaveLen(2))
		var recovered *opsv1alpha1.RestoreVerification
		for i := range verifications {
			if verifications[i].Spec.Verification.Command[0] == cmdTrue {
				recovered = &verifications[i]
			}
		}
		Expect(recovered).NotTo(BeNil())
		finishOwnedVerification(ctx, k8sClient, recovered, opsv1alpha1.RestoreVerificationPhaseSucceeded, now, verificationExitMessage(0))

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		expectReady(got)
		Expect(got.Status.LastVerification.Result).To(Equal(opsv1alpha1.RestoreVerificationPhaseSucceeded))
		Expect(got.Status.LastVerification.Name).To(Equal(recovered.Name))
	})
})

func enableVerification(policy *opsv1alpha1.BackupPolicy, command []string) {
	enabled := true
	policy.Spec.VerifyRestore = &enabled
	policy.Spec.Verification = &opsv1alpha1.Verification{
		Image:   "busybox:1.36",
		Command: command,
	}
}

func policyClient(scheme *runtime.Scheme, objects ...client.Object) client.WithWatch {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&opsv1alpha1.BackupPolicy{}, &opsv1alpha1.RestoreVerification{}).
		WithObjects(objects...).
		Build()
}

func listVerifications(ctx context.Context, cl client.Client) []opsv1alpha1.RestoreVerification {
	list := &opsv1alpha1.RestoreVerificationList{}
	Expect(cl.List(ctx, list)).To(Succeed())
	return list.Items
}

func finishOwnedVerification(ctx context.Context, cl client.Client, verification *opsv1alpha1.RestoreVerification, phase string, finished time.Time, message string) {
	current := &opsv1alpha1.RestoreVerification{}
	Expect(cl.Get(ctx, client.ObjectKeyFromObject(verification), current)).To(Succeed())
	current.Status.Phase = phase
	current.Status.Message = message
	current.Status.CompletionTime = &metav1.Time{Time: finished.UTC()}
	Expect(cl.Status().Update(ctx, current)).To(Succeed())
}

func finishedPolicyVerification(policy *opsv1alpha1.BackupPolicy, pvcName string, finished time.Time, phase, message string) *opsv1alpha1.RestoreVerification {
	verification := newPolicyVerification(policy, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: policy.Namespace}}, finished)
	verification.Status.Phase = phase
	verification.Status.Message = message
	verification.Status.CompletionTime = &metav1.Time{Time: finished.UTC()}
	return verification
}

func expectVerificationFailed(policy *opsv1alpha1.BackupPolicy, name, message string) {
	expectCondition(policy, opsv1alpha1.BackupPolicyConditionReady, metav1.ConditionFalse, backupPolicyReasonVerificationFailed)
	expectCondition(policy, opsv1alpha1.BackupPolicyConditionDegraded, metav1.ConditionTrue, backupPolicyReasonVerificationFailed)
	Expect(policy.Status.LastVerification).NotTo(BeNil())
	Expect(policy.Status.LastVerification.Name).To(Equal(name))
	Expect(policy.Status.LastVerification.Result).To(Equal(opsv1alpha1.RestoreVerificationPhaseFailed))
	Expect(policy.Status.LastVerification.Message).To(Equal(message))
}

func verificationsFor(ctx context.Context, cl client.Client, namespace, policyName string) []opsv1alpha1.RestoreVerification {
	list := &opsv1alpha1.RestoreVerificationList{}
	Expect(cl.List(ctx, list, client.InNamespace(namespace), client.MatchingLabels{labelBackupPolicy: policyName})).To(Succeed())
	return list.Items
}

func deletePolicyVerifications(namespace, policyName string) {
	list := &opsv1alpha1.RestoreVerificationList{}
	Expect(k8sClient.List(ctx, list, client.InNamespace(namespace))).To(Succeed())
	for i := range list.Items {
		if list.Items[i].Labels[labelBackupPolicy] != policyName {
			continue
		}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &list.Items[i]))).To(Succeed())
	}
}
