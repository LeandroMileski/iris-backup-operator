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
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

func TestValidateBackupPolicy(t *testing.T) {
	retentionZero := int32(0)
	retentionNegative := int32(-1)
	retentionOne := int32(1)

	tests := []struct {
		name    string
		mutate  func(*opsv1alpha1.BackupPolicy)
		wantErr []string
	}{
		{name: "valid policy"},
		{
			name: "selector with only a match expression",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				policy.Spec.TargetSelector = metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key:      labelApp,
						Operator: metav1.LabelSelectorOpExists,
					}},
				}
			},
		},
		{
			name: "verification omitted when verifyRestore is unset",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				policy.Spec.VerifyRestore = nil
				policy.Spec.Verification = nil
			},
		},
		{
			name: "retention of 1",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				policy.Spec.Retention = &retentionOne
			},
		},
		{
			name: "verifyRestore with image and command",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				enableVerify(policy)
				policy.Spec.Verification = &opsv1alpha1.Verification{
					Image:   imageBusybox,
					Command: []string{cmdEcho, "ok"},
				}
			},
		},
		{
			name:    "empty schedule",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "non-cron text",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = scheduleNotCron },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "minute out of range",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = scheduleOutOfRange },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage, "60"},
		},
		{
			name:    "hour out of range",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "0 24 * * *" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "day of month out of range",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "0 0 32 * *" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "month out of range",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "0 0 * 13 *" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "day of week out of range",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "0 0 * * 8" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "too few fields",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "* * * *" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "too many fields",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "* * * * * *" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "zero step",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "*/0 * * * *" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "descriptor",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "@daily" },
			wantErr: []string{fieldSchedule, scheduleInvalidMessage},
		},
		{
			name:    "impossible day",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Schedule = "0 0 31 2 *" },
			wantErr: []string{fieldSchedule, "must be a five-field cron expression with an upcoming time"},
		},
		{
			name:    "missing retention",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Retention = nil },
			wantErr: []string{fieldRetention, retentionInvalidMessage},
		},
		{
			name:    "retention of 0",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Retention = &retentionZero },
			wantErr: []string{fieldRetention, retentionInvalidMessage},
		},
		{
			name:    "negative retention",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.Retention = &retentionNegative },
			wantErr: []string{fieldRetention, retentionInvalidMessage},
		},
		{
			name:    "empty selector",
			mutate:  func(policy *opsv1alpha1.BackupPolicy) { policy.Spec.TargetSelector = metav1.LabelSelector{} },
			wantErr: []string{fieldTargetSelector, "must not be empty"},
		},
		{
			name: "selector that cannot be parsed",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				policy.Spec.TargetSelector = metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key:      labelApp,
						Operator: metav1.LabelSelectorOpIn,
					}},
				}
			},
			wantErr: []string{fieldTargetSelector, "must be a valid label selector"},
		},
		{
			name: "verifyRestore without verification",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				enableVerify(policy)
			},
			wantErr: []string{"spec.verification", verificationRequiredMessage},
		},
		{
			name: "verifyRestore without a command",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				enableVerify(policy)
				policy.Spec.Verification = &opsv1alpha1.Verification{Image: imageBusybox}
			},
			wantErr: []string{"spec.verification.command", verificationRequiredMessage},
		},
		{
			name: "verifyRestore with a blank image and command",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				enableVerify(policy)
				policy.Spec.Verification = &opsv1alpha1.Verification{Image: "  ", Command: []string{" "}}
			},
			wantErr: []string{
				"spec.verification.image",
				"spec.verification.command[0]",
				verificationRequiredMessage,
				"must not be empty",
			},
		},
		{
			name: "every problem in one response",
			mutate: func(policy *opsv1alpha1.BackupPolicy) {
				policy.Spec.Schedule = scheduleOutOfRange
				policy.Spec.TargetSelector = metav1.LabelSelector{}
				policy.Spec.Retention = &retentionZero
				enableVerify(policy)
			},
			wantErr: []string{fieldSchedule, fieldRetention, fieldTargetSelector, "spec.verification"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := validBackupPolicy("daily")
			if tt.mutate != nil {
				tt.mutate(policy)
			}

			err := validateBackupPolicy(policy)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("validateBackupPolicy() unexpected error: %v", err)
				}
				return
			}
			expectInvalidPolicy(t, err, tt.wantErr)
		})
	}
}

func TestApplyBackupPolicyDefaults(t *testing.T) {
	retention := int32(3)
	suspend := true

	tests := []struct {
		name          string
		spec          opsv1alpha1.BackupPolicySpec
		wantSchedule  string
		wantRetention int32
		wantSuspend   bool
	}{
		{
			name:          "empty schedule retention and suspend",
			spec:          opsv1alpha1.BackupPolicySpec{Schedule: "  "},
			wantSchedule:  opsv1alpha1.DefaultBackupSchedule,
			wantRetention: opsv1alpha1.DefaultBackupRetention,
			wantSuspend:   false,
		},
		{
			name: "values already set",
			spec: opsv1alpha1.BackupPolicySpec{
				Schedule:  scheduleOneAM,
				Retention: &retention,
				Suspend:   &suspend,
			},
			wantSchedule:  scheduleOneAM,
			wantRetention: retention,
			wantSuspend:   suspend,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &opsv1alpha1.BackupPolicy{Spec: tt.spec}
			applyBackupPolicyDefaults(policy)

			if policy.Spec.Schedule != tt.wantSchedule {
				t.Fatalf("schedule = %q, want %q", policy.Spec.Schedule, tt.wantSchedule)
			}
			if policy.Spec.Retention == nil || *policy.Spec.Retention != tt.wantRetention {
				t.Fatalf("retention = %v, want %d", policy.Spec.Retention, tt.wantRetention)
			}
			if policy.Spec.Suspend == nil || *policy.Spec.Suspend != tt.wantSuspend {
				t.Fatalf("suspend = %v, want %t", policy.Spec.Suspend, tt.wantSuspend)
			}
		})
	}
}

func TestDefaultedPolicyIsValid(t *testing.T) {
	policy := &opsv1alpha1.BackupPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "daily"},
		Spec: opsv1alpha1.BackupPolicySpec{
			TargetSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelApp: appIris},
			},
		},
	}

	applyBackupPolicyDefaults(policy)
	if policy.Spec.Schedule != opsv1alpha1.DefaultBackupSchedule {
		t.Fatalf("schedule = %q, want %q", policy.Spec.Schedule, opsv1alpha1.DefaultBackupSchedule)
	}
	if policy.Spec.Retention == nil || *policy.Spec.Retention != opsv1alpha1.DefaultBackupRetention {
		t.Fatalf("retention = %v, want %d", policy.Spec.Retention, opsv1alpha1.DefaultBackupRetention)
	}
	if policy.Spec.Suspend == nil || *policy.Spec.Suspend {
		t.Fatalf("suspend = %v, want false", policy.Spec.Suspend)
	}
	if err := validateBackupPolicy(policy); err != nil {
		t.Fatalf("validateBackupPolicy() after defaults: %v", err)
	}

	policy.Spec.Schedule = scheduleNotCron
	applyBackupPolicyDefaults(policy)
	if policy.Spec.Schedule != scheduleNotCron {
		t.Fatalf("schedule = %q, want not-a-cron", policy.Spec.Schedule)
	}
	expectInvalidPolicy(t, validateBackupPolicy(policy), []string{fieldSchedule, scheduleInvalidMessage})
}

func expectInvalidPolicy(t *testing.T, err error, fragments []string) {
	t.Helper()
	if err == nil {
		t.Fatal("validateBackupPolicy() error = nil, want an error")
	}

	var statusErr *apierrors.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("validateBackupPolicy() error type = %T, want StatusError", err)
	}
	if statusErr.ErrStatus.Reason != metav1.StatusReasonInvalid {
		t.Fatalf("reason = %s, want %s", statusErr.ErrStatus.Reason, metav1.StatusReasonInvalid)
	}

	message := err.Error()
	for _, fragment := range fragments {
		if !strings.Contains(message, fragment) {
			t.Fatalf("validateBackupPolicy() = %q, want substring %q", message, fragment)
		}
	}
}
