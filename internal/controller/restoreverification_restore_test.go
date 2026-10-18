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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

func TestRestoreObjectName(t *testing.T) {
	if got := restoreObjectName("data"); got != "verify-data" {
		t.Fatalf("restoreObjectName() = %s, want verify-data", got)
	}

	long := restoreObjectName(strings.Repeat("a", 250))
	if len(long) > 253 {
		t.Fatalf("restoreObjectName() length = %d, want at most 253", len(long))
	}
	if !strings.HasPrefix(long, "verify-") {
		t.Fatalf("restoreObjectName() = %s, want a verify- hash", long)
	}
}

var _ = Describe("RestoreVerification restore", func() {
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
		now = time.Date(2026, 10, 18, 16, 45, 0, 0, time.UTC)
	})

	It("returns an error when the VolumeSnapshot does not exist", func() {
		cl := restoreClient(scheme, verification, restoreSourcePVC())

		_, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(ContainSubstring("VolumeSnapshot data-snapshot not found")))
		Expect(apierrors.IsNotFound(cl.Get(ctx, types.NamespacedName{Name: restoreObjectName(verification.Name), Namespace: verification.Namespace}, &corev1.PersistentVolumeClaim{}))).To(BeTrue())
	})

	It("returns an error when the source PersistentVolumeClaim does not exist", func() {
		cl := restoreClient(scheme, verification, restoreSnapshot(verification.Namespace))

		_, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(ContainSubstring("PersistentVolumeClaim data not found")))
		Expect(apierrors.IsNotFound(cl.Get(ctx, types.NamespacedName{Name: restoreObjectName(verification.Name), Namespace: verification.Namespace}, &corev1.Pod{}))).To(BeTrue())
	})

	It("returns an error when the source claim has no storage request", func() {
		source := restoreSourcePVC()
		source.Spec.Resources = corev1.VolumeResourceRequirements{}
		cl := restoreClient(scheme, verification, source, restoreSnapshot(verification.Namespace))

		_, err := newVerificationReconciler(cl, now).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(ContainSubstring("has no storage request")))
	})

	It("creates one read-only Pod and keeps the default timeout", func() {
		cl := restoreClient(scheme, verification, restoreSourcePVC(), restoreSnapshot(verification.Namespace))
		reconciler := newVerificationReconciler(cl, now)

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: restoreObjectName(verification.Name), Namespace: verification.Namespace}, pod)).To(Succeed())
		Expect(pod.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
		Expect(*pod.Spec.ActiveDeadlineSeconds).To(Equal(defaultVerificationTimeout))
		created := pod.ResourceVersion

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(cl.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
		Expect(pod.ResourceVersion).To(Equal(created))
	})
})

func restoreKey() types.NamespacedName {
	return types.NamespacedName{Name: restoreObjectName("test-resource"), Namespace: "default"}
}

func restoreSourcePVC() *corev1.PersistentVolumeClaim {
	class := "standard"
	claim := backupPVC("data")
	claim.Labels = map[string]string{"app": "restore-source"}
	claim.Spec.StorageClassName = &class
	return claim
}

func restoreSnapshot(namespace string) *unstructured.Unstructured {
	snapshot := &unstructured.Unstructured{}
	snapshot.SetGroupVersionKind(volumeSnapshotGVK)
	snapshot.SetName("data-snapshot")
	snapshot.SetNamespace(namespace)
	return snapshot
}

func restoreClient(scheme *runtime.Scheme, objects ...client.Object) client.WithWatch {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&opsv1alpha1.RestoreVerification{}).
		WithObjects(objects...).
		Build()
}

func expectRestorePVC(claim *corev1.PersistentVolumeClaim, verification *opsv1alpha1.RestoreVerification) {
	Expect(claim.Spec.DataSource).NotTo(BeNil())
	Expect(claim.Spec.DataSource.Name).To(Equal(verification.Spec.SnapshotName))
	Expect(claim.Spec.DataSource.Kind).To(Equal(volumeSnapshotGVK.Kind))
	Expect(claim.Spec.DataSource.APIGroup).NotTo(BeNil())
	Expect(*claim.Spec.DataSource.APIGroup).To(Equal(volumeSnapshotGVK.Group))
	Expect(claim.Spec.AccessModes).To(Equal([]corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}))
	Expect(claim.Spec.Resources.Requests[corev1.ResourceStorage].Equal(resource.MustParse("1Gi"))).To(BeTrue())
	Expect(claim.Spec.StorageClassName).NotTo(BeNil())
	Expect(*claim.Spec.StorageClassName).To(Equal("standard"))
	Expect(claim.Labels).To(HaveKeyWithValue(labelRestoreVerification, verification.Name))
	Expect(metav1.IsControlledBy(claim, verification)).To(BeTrue())
}

func expectRestorePod(pod *corev1.Pod, verification *opsv1alpha1.RestoreVerification) {
	Expect(pod.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
	Expect(pod.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
	Expect(*pod.Spec.ActiveDeadlineSeconds).To(Equal(verificationTimeout(verification)))
	Expect(pod.Spec.Containers).To(HaveLen(1))
	container := pod.Spec.Containers[0]
	Expect(container.Name).To(Equal(restoreContainerName))
	Expect(container.Image).To(Equal(verification.Spec.Verification.Image))
	Expect(container.Command).To(Equal(verification.Spec.Verification.Command))
	Expect(container.VolumeMounts).To(ConsistOf(corev1.VolumeMount{
		Name:      restoreVolumeName,
		MountPath: restoreVolumeMountPath,
		ReadOnly:  true,
	}))
	Expect(pod.Spec.AutomountServiceAccountToken).NotTo(BeNil())
	Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())
	expectNonRoot(pod.Spec.SecurityContext)
	Expect(pod.Spec.SecurityContext.SeccompProfile).NotTo(BeNil())
	Expect(pod.Spec.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
	Expect(container.SecurityContext).NotTo(BeNil())
	expectNonRootContainer(container.SecurityContext)
	Expect(container.SecurityContext.ReadOnlyRootFilesystem).NotTo(BeNil())
	Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
	Expect(container.SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
	Expect(*container.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
	Expect(container.SecurityContext.Capabilities).NotTo(BeNil())
	Expect(container.SecurityContext.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
	Expect(container.SecurityContext.SeccompProfile).NotTo(BeNil())
	Expect(container.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
	Expect(container.Resources.Limits[corev1.ResourceCPU].Equal(resource.MustParse(verificationCPULimit))).To(BeTrue())
	Expect(container.Resources.Limits[corev1.ResourceMemory].Equal(resource.MustParse(verificationMemoryLimit))).To(BeTrue())
	Expect(pod.Spec.Volumes).To(ConsistOf(corev1.Volume{
		Name: restoreVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pod.Name,
				ReadOnly:  true,
			},
		},
	}))
	Expect(pod.Labels).To(HaveKeyWithValue(labelRestoreVerification, verification.Name))
	Expect(metav1.IsControlledBy(pod, verification)).To(BeTrue())
}

func expectNonRoot(security *corev1.PodSecurityContext) {
	Expect(security).NotTo(BeNil())
	Expect(security.RunAsNonRoot).NotTo(BeNil())
	Expect(*security.RunAsNonRoot).To(BeTrue())
	Expect(security.RunAsUser).NotTo(BeNil())
	Expect(*security.RunAsUser).To(Equal(verificationRunAsUser))
}

func expectNonRootContainer(security *corev1.SecurityContext) {
	Expect(security.RunAsNonRoot).NotTo(BeNil())
	Expect(*security.RunAsNonRoot).To(BeTrue())
	Expect(security.RunAsUser).NotTo(BeNil())
	Expect(*security.RunAsUser).To(Equal(verificationRunAsUser))
}
