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
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

const (
	labelRestoreVerification = "ops.leandro.ie/restore-verification"

	restoreVolumeName          = "data"
	restoreVolumeMountPath     = "/data"
	restoreContainerName       = "verify"
	defaultVerificationTimeout = int64(600)

	// verificationRunAsUser is nobody. The image user is not trusted.
	verificationRunAsUser   int64 = 65534
	verificationCPULimit          = "100m"
	verificationMemoryLimit       = "64Mi"
)

// ensureRestore creates the PersistentVolumeClaim restored from the snapshot and the verification Pod.
// Both objects are owned by the RestoreVerification. A second reconcile leaves an existing object in place.
func (r *RestoreVerificationReconciler) ensureRestore(ctx context.Context, verification *opsv1alpha1.RestoreVerification) error {
	if err := r.getSnapshot(ctx, verification); err != nil {
		return err
	}
	source, err := r.getSourceClaim(ctx, verification)
	if err != nil {
		return err
	}

	name := restoreObjectName(verification.Name)
	pvc, err := newRestorePVC(verification, source, name)
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to build restore PersistentVolumeClaim", "pvc", source.Name)
		return err
	}
	if err := controllerutil.SetControllerReference(verification, pvc, r.Scheme); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to set PersistentVolumeClaim owner reference", "pvc", pvc.Name)
		return err
	}
	if err := r.createIfAbsent(ctx, pvc, "PersistentVolumeClaim"); err != nil {
		return err
	}
	if err := r.ensureVerificationNetworkPolicy(ctx, verification.Namespace); err != nil {
		return err
	}

	pod := newVerificationPod(verification, name)
	if err := controllerutil.SetControllerReference(verification, pod, r.Scheme); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to set Pod owner reference", "pod", pod.Name)
		return err
	}
	return r.createIfAbsent(ctx, pod, "Pod")
}

func (r *RestoreVerificationReconciler) getSnapshot(ctx context.Context, verification *opsv1alpha1.RestoreVerification) error {
	snapshot := &unstructured.Unstructured{}
	snapshot.SetGroupVersionKind(volumeSnapshotGVK)
	err := r.Get(ctx, client.ObjectKey{Namespace: verification.Namespace, Name: verification.Spec.SnapshotName}, snapshot)
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) {
		logf.FromContext(ctx).Error(err, "VolumeSnapshot not found", "snapshot", verification.Spec.SnapshotName)
		return fmt.Errorf("VolumeSnapshot %s not found", verification.Spec.SnapshotName)
	}
	logf.FromContext(ctx).Error(err, "Failed to get VolumeSnapshot", "snapshot", verification.Spec.SnapshotName)
	return err
}

func (r *RestoreVerificationReconciler) getSourceClaim(ctx context.Context, verification *opsv1alpha1.RestoreVerification) (*corev1.PersistentVolumeClaim, error) {
	source := &corev1.PersistentVolumeClaim{}
	key := client.ObjectKey{Namespace: verification.Namespace, Name: verification.Spec.PersistentVolumeClaimName}
	err := r.Get(ctx, key, source)
	if err == nil {
		return source, nil
	}
	if apierrors.IsNotFound(err) {
		logf.FromContext(ctx).Error(err, "PersistentVolumeClaim not found", "pvc", verification.Spec.PersistentVolumeClaimName)
		return nil, fmt.Errorf("PersistentVolumeClaim %s not found", verification.Spec.PersistentVolumeClaimName)
	}
	logf.FromContext(ctx).Error(err, "Failed to get PersistentVolumeClaim", "pvc", verification.Spec.PersistentVolumeClaimName)
	return nil, err
}

func newRestorePVC(verification *opsv1alpha1.RestoreVerification, source *corev1.PersistentVolumeClaim, name string) (*corev1.PersistentVolumeClaim, error) {
	if len(source.Spec.AccessModes) == 0 {
		return nil, fmt.Errorf("PersistentVolumeClaim %s has no access mode", source.Name)
	}
	storage, ok := source.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok || storage.IsZero() {
		return nil, fmt.Errorf("PersistentVolumeClaim %s has no storage request", source.Name)
	}

	apiGroup := volumeSnapshotGVK.Group
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: verification.Namespace,
			Labels:    restoreLabels(verification.Name),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: append([]corev1.PersistentVolumeAccessMode(nil), source.Spec.AccessModes...),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: storage},
			},
			VolumeMode: source.Spec.VolumeMode,
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     volumeSnapshotGVK.Kind,
				Name:     verification.Spec.SnapshotName,
			},
		},
	}
	if source.Spec.StorageClassName != nil {
		class := *source.Spec.StorageClassName
		pvc.Spec.StorageClassName = &class
	}
	return pvc, nil
}

func newVerificationPod(verification *opsv1alpha1.RestoreVerification, claimName string) *corev1.Pod {
	timeout := verificationTimeout(verification)
	runAsUser := verificationRunAsUser
	runAsNonRoot := true
	readOnlyRoot := true
	allowPrivilegeEscalation := false
	automountToken := false
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: verification.Namespace,
			Labels:    restoreLabels(verification.Name),
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:        &timeout,
			AutomountServiceAccountToken: &automountToken,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRoot,
				RunAsUser:    &runAsUser,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{{
				Name:    restoreContainerName,
				Image:   verification.Spec.Verification.Image,
				Command: append([]string(nil), verification.Spec.Verification.Command...),
				SecurityContext: &corev1.SecurityContext{
					RunAsNonRoot:             &runAsNonRoot,
					RunAsUser:                &runAsUser,
					ReadOnlyRootFilesystem:   &readOnlyRoot,
					AllowPrivilegeEscalation: &allowPrivilegeEscalation,
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeRuntimeDefault,
					},
				},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(verificationCPULimit),
						corev1.ResourceMemory: resource.MustParse(verificationMemoryLimit),
					},
				},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      restoreVolumeName,
					MountPath: restoreVolumeMountPath,
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: restoreVolumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: claimName,
						ReadOnly:  true,
					},
				},
			}},
		},
	}
}

func verificationTimeout(verification *opsv1alpha1.RestoreVerification) int64 {
	if verification.Spec.Verification.TimeoutSeconds != nil && *verification.Spec.Verification.TimeoutSeconds > 0 {
		return int64(*verification.Spec.Verification.TimeoutSeconds)
	}
	return defaultVerificationTimeout
}

func restoreLabels(verificationName string) map[string]string {
	labels := map[string]string{}
	setLabel(labels, labelRestoreVerification, verificationName)
	return labels
}

func restoreObjectName(verificationName string) string {
	name := "verify-" + verificationName
	if len(name) <= 253 {
		return name
	}
	sum := sha256.Sum256([]byte(verificationName))
	return fmt.Sprintf("verify-%x", sum[:20])
}

func (r *RestoreVerificationReconciler) createIfAbsent(ctx context.Context, obj client.Object, kind string) error {
	log := logf.FromContext(ctx)
	existing := obj.DeepCopyObject().(client.Object)
	err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing)
	if err == nil {
		log.Info(kind+" already exists", "name", obj.GetName())
		return nil
	}
	if !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to get "+kind, "name", obj.GetName())
		return err
	}
	if err := r.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Info(kind+" already exists", "name", obj.GetName())
			return nil
		}
		log.Error(err, "Failed to create "+kind, "name", obj.GetName())
		return err
	}
	log.Info("Created "+kind, "name", obj.GetName())
	return nil
}
