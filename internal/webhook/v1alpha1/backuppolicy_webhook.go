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

package v1alpha1

import (
	"context"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var backuppolicylog = logf.Log.WithName("backuppolicy-resource")

// SetupBackupPolicyWebhookWithManager registers the webhook for BackupPolicy in the manager.
func SetupBackupPolicyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &opsv1alpha1.BackupPolicy{}).
		WithDefaulter(&BackupPolicyDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-ops-leandro-ie-v1alpha1-backuppolicy,mutating=true,failurePolicy=fail,sideEffects=None,groups=ops.leandro.ie,resources=backuppolicies,verbs=create;update,versions=v1alpha1,name=mbackuppolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// BackupPolicyDefaulter struct is responsible for setting default values on the custom resource of the
// Kind BackupPolicy when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type BackupPolicyDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

// Default implements admission.Defaulter so a webhook will be registered for the Kind BackupPolicy.
func (d *BackupPolicyDefaulter) Default(_ context.Context, obj *opsv1alpha1.BackupPolicy) error {
	backuppolicylog.Info("Defaulting for BackupPolicy", "name", obj.GetName())
	applyBackupPolicyDefaults(obj)
	return nil
}

// applyBackupPolicyDefaults fills schedule, retention, and suspend when they are empty.
// A value that is already set is left unchanged.
func applyBackupPolicyDefaults(policy *opsv1alpha1.BackupPolicy) {
	if strings.TrimSpace(policy.Spec.Schedule) == "" {
		policy.Spec.Schedule = opsv1alpha1.DefaultBackupSchedule
	}
	if policy.Spec.Retention == nil {
		retention := opsv1alpha1.DefaultBackupRetention
		policy.Spec.Retention = &retention
	}
	if policy.Spec.Suspend == nil {
		suspend := false
		policy.Spec.Suspend = &suspend
	}
}
