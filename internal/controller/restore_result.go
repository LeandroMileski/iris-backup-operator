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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

const (
	podReasonDeadlineExceeded = "DeadlineExceeded"

	verificationTimeoutMessage = "Verification command exceeded its timeout"
)

// observeVerificationPod reads the verification Pod exit code.
// done is false while the Pod is missing or still running.
func (r *RestoreVerificationReconciler) observeVerificationPod(ctx context.Context, verification *opsv1alpha1.RestoreVerification) (bool, bool, string, error) {
	pod := &corev1.Pod{}
	key := restoreObjectKey(verification)
	err := r.Get(ctx, key, pod)
	if apierrors.IsNotFound(err) {
		return false, false, "", nil
	}
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to get Pod", "name", key.Name)
		return false, false, "", err
	}
	done, succeeded, message := verificationPodResult(pod)
	return done, succeeded, message, nil
}

// verificationPodResult maps a Pod phase and container exit code onto a verification result.
// A Pod that is still pending or running is not done.
// Exit code 0 is success. Any other exit code is failure.
// A deadline is failure even when the container has no exit code.
func verificationPodResult(pod *corev1.Pod) (done, succeeded bool, message string) {
	switch pod.Status.Phase {
	case corev1.PodSucceeded, corev1.PodFailed:
	default:
		return false, false, ""
	}
	if deadlineExceeded(pod) {
		return true, false, verificationTimeoutMessage
	}
	code, found := containerExitCode(pod)
	if !found {
		if pod.Status.Phase == corev1.PodSucceeded {
			return true, true, verificationExitMessage(0)
		}
		reason := pod.Status.Reason
		if reason == "" {
			reason = string(pod.Status.Phase)
		}
		return true, false, "Verification Pod failed: " + reason
	}
	return true, code == 0, verificationExitMessage(code)
}

func verificationExitMessage(code int32) string {
	return fmt.Sprintf("Verification command exited %d", code)
}

func deadlineExceeded(pod *corev1.Pod) bool {
	if pod.Status.Reason == podReasonDeadlineExceeded {
		return true
	}
	_, reason, found := containerTermination(pod)
	return found && reason == podReasonDeadlineExceeded
}

func containerExitCode(pod *corev1.Pod) (int32, bool) {
	code, _, found := containerTermination(pod)
	return code, found
}

func containerTermination(pod *corev1.Pod) (int32, string, bool) {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != restoreContainerName || status.State.Terminated == nil {
			continue
		}
		return status.State.Terminated.ExitCode, status.State.Terminated.Reason, true
	}
	return 0, "", false
}

// cleanupRestore deletes the verification Pod and the restored PersistentVolumeClaim.
// A claim that is still present is retried. The source claim and the VolumeSnapshot are left in place.
func (r *RestoreVerificationReconciler) cleanupRestore(ctx context.Context, verification *opsv1alpha1.RestoreVerification) (ctrl.Result, error) {
	key := restoreObjectKey(verification)
	podGone, err := r.deleteIfPresent(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}, "Pod")
	if err != nil {
		return ctrl.Result{}, err
	}
	claimGone, err := r.deleteIfPresent(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}, "PersistentVolumeClaim")
	if err != nil {
		return ctrl.Result{}, err
	}
	if !podGone || !claimGone {
		logf.FromContext(ctx).Info("Waiting for restore resources to delete", "name", key.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func restoreObjectKey(verification *opsv1alpha1.RestoreVerification) client.ObjectKey {
	return client.ObjectKey{Namespace: verification.Namespace, Name: restoreObjectName(verification.Name)}
}

func (r *RestoreVerificationReconciler) deleteIfPresent(ctx context.Context, obj client.Object, kind string) (bool, error) {
	log := logf.FromContext(ctx)
	key := client.ObjectKeyFromObject(obj)
	current := obj.DeepCopyObject().(client.Object)
	err := r.Get(ctx, key, current)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		log.Error(err, "Failed to get "+kind, "name", key.Name)
		return false, err
	}
	if err := r.Delete(ctx, current); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		log.Error(err, "Failed to delete "+kind, "name", key.Name)
		return false, err
	}
	log.Info("Deleted "+kind, "name", key.Name)

	remaining := obj.DeepCopyObject().(client.Object)
	err = r.Get(ctx, key, remaining)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		log.Error(err, "Failed to get "+kind, "name", key.Name)
		return false, err
	}
	// A deletion timestamp means the object is terminating. The kubelet or the
	// garbage collector removes it after the grace period and finalizers finish.
	return !remaining.GetDeletionTimestamp().IsZero(), nil
}
