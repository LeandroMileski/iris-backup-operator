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
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

const (
	scheduleInvalidMessage       = "must be a five-field cron expression"
	scheduleNoNextMessage        = "must be a five-field cron expression with an upcoming time"
	retentionInvalidMessage      = "must be greater than or equal to 1"
	selectorEmptyMessage         = "must not be empty"
	verificationRequiredMessage  = "required when verifyRestore is true"
	verificationCommandEmptyItem = "must not be empty"
)

// backupPolicyCronParser accepts a five-field cron expression: minute, hour, day of month, month, day of week.
// Descriptors such as @daily are rejected so admission matches the reconciler.
var backupPolicyCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

var backupPolicyGroupKind = schema.GroupKind{Group: opsv1alpha1.GroupVersion.Group, Kind: "BackupPolicy"}

// validateBackupPolicy reports every spec problem that should keep a BackupPolicy out of the API.
func validateBackupPolicy(policy *opsv1alpha1.BackupPolicy) error {
	allErrs := validateBackupPolicySpec(&policy.Spec)
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(backupPolicyGroupKind, policy.Name, allErrs)
}

func validateBackupPolicySpec(spec *opsv1alpha1.BackupPolicySpec) field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, validateSchedule(spec.Schedule)...)
	allErrs = append(allErrs, validateRetention(spec.Retention)...)
	allErrs = append(allErrs, validateTargetSelector(&spec.TargetSelector)...)
	if spec.VerifyRestore != nil && *spec.VerifyRestore {
		allErrs = append(allErrs, validateVerification(spec.Verification)...)
	}
	return allErrs
}

func validateSchedule(schedule string) field.ErrorList {
	path := field.NewPath("spec", "schedule")
	parsed, err := backupPolicyCronParser.Parse(schedule)
	if err != nil {
		return field.ErrorList{field.Invalid(path, schedule, scheduleInvalidMessage+": "+err.Error())}
	}
	if parsed.Next(time.Now().UTC()).IsZero() {
		return field.ErrorList{field.Invalid(path, schedule, scheduleNoNextMessage)}
	}
	return nil
}

func validateRetention(retention *int32) field.ErrorList {
	path := field.NewPath("spec", "retention")
	if retention == nil {
		return field.ErrorList{field.Required(path, retentionInvalidMessage)}
	}
	if *retention < 1 {
		return field.ErrorList{field.Invalid(path, *retention, retentionInvalidMessage)}
	}
	return nil
}

func validateTargetSelector(selector *metav1.LabelSelector) field.ErrorList {
	path := field.NewPath("spec", "targetSelector")
	if len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0 {
		return field.ErrorList{field.Required(path, selectorEmptyMessage)}
	}
	if _, err := metav1.LabelSelectorAsSelector(selector); err != nil {
		return field.ErrorList{field.Invalid(path, selector, fmt.Sprintf("must be a valid label selector: %s", err.Error()))}
	}
	return nil
}

func validateVerification(verification *opsv1alpha1.Verification) field.ErrorList {
	path := field.NewPath("spec", "verification")
	if verification == nil {
		return field.ErrorList{field.Required(path, verificationRequiredMessage)}
	}

	var allErrs field.ErrorList
	if strings.TrimSpace(verification.Image) == "" {
		allErrs = append(allErrs, field.Required(path.Child("image"), verificationRequiredMessage))
	}
	if len(verification.Command) == 0 {
		allErrs = append(allErrs, field.Required(path.Child("command"), verificationRequiredMessage))
		return allErrs
	}
	for i, arg := range verification.Command {
		if strings.TrimSpace(arg) == "" {
			allErrs = append(allErrs, field.Invalid(path.Child("command").Index(i), arg, verificationCommandEmptyItem))
		}
	}
	return allErrs
}
