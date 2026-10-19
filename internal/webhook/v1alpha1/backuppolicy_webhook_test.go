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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

var _ = Describe("BackupPolicy Webhook", func() {
	var (
		obj       *opsv1alpha1.BackupPolicy
		oldObj    *opsv1alpha1.BackupPolicy
		defaulter BackupPolicyDefaulter
		validator BackupPolicyValidator
	)

	BeforeEach(func() {
		obj = &opsv1alpha1.BackupPolicy{}
		oldObj = &opsv1alpha1.BackupPolicy{}
		defaulter = BackupPolicyDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
		validator = BackupPolicyValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating BackupPolicy under Defaulting Webhook", func() {
		It("fills an empty schedule, retention, and suspend", func() {
			obj.Spec.Schedule = "  "
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			expectBackupPolicyDefaults(obj)
		})

		It("leaves a schedule, retention, and suspend that are already set", func() {
			retention := int32(3)
			suspend := true
			obj.Spec.Schedule = scheduleOneAM
			obj.Spec.Retention = &retention
			obj.Spec.Suspend = &suspend

			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.Schedule).To(Equal(scheduleOneAM))
			Expect(*obj.Spec.Retention).To(Equal(retention))
			Expect(*obj.Spec.Suspend).To(Equal(suspend))
		})

		It("stores the defaults when the API server admits a policy", func() {
			policy := &opsv1alpha1.BackupPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-defaults",
					Namespace: "default",
				},
				Spec: opsv1alpha1.BackupPolicySpec{
					TargetSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{labelApp: appIris},
					},
				},
			}
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())

			got := &opsv1alpha1.BackupPolicy{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), got)).To(Succeed())
			expectBackupPolicyDefaults(got)

			Expect(k8sClient.Delete(ctx, got)).To(Succeed())
		})
	})

	Context("When creating or updating BackupPolicy under Validating Webhook", func() {
		It("admits a valid policy on create and update", func() {
			policy := validBackupPolicy("valid")
			_, err := validator.ValidateCreate(ctx, policy)
			Expect(err).To(Succeed())

			updated := validBackupPolicy("valid")
			updated.Spec.Schedule = scheduleOneAM
			_, err = validator.ValidateUpdate(ctx, policy, updated)
			Expect(err).To(Succeed())
		})

		It("rejects an update that breaks the schedule", func() {
			policy := validBackupPolicy("update")
			updated := validBackupPolicy("update")
			updated.Spec.Schedule = scheduleOutOfRange
			_, err := validator.ValidateUpdate(ctx, policy, updated)
			expectDenied(err, fieldSchedule, "five-field cron")
		})

		It("allows deletion of an invalid policy", func() {
			policy := validBackupPolicy("delete")
			policy.Spec.Schedule = scheduleNotCron
			_, err := validator.ValidateDelete(ctx, policy)
			Expect(err).To(Succeed())
		})

		It("rejects an invalid cron expression at the API server", func() {
			policy := validBackupPolicy("webhook-invalid-cron")
			policy.Spec.Schedule = scheduleOutOfRange
			expectDenied(k8sClient.Create(ctx, policy), fieldSchedule, "five-field cron")
		})

		It("rejects an empty selector at the API server", func() {
			policy := validBackupPolicy("webhook-empty-selector")
			policy.Spec.TargetSelector = metav1.LabelSelector{}
			expectDenied(k8sClient.Create(ctx, policy), fieldTargetSelector, "must not be empty")
		})

		It("rejects verifyRestore without verification at the API server", func() {
			policy := validBackupPolicy("webhook-verify-required")
			enableVerify(policy)
			expectDenied(k8sClient.Create(ctx, policy), "spec.verification", verificationRequiredMessage)
		})

		It("rejects retention below 1 at the API server", func() {
			policy := validBackupPolicy("webhook-retention-zero")
			retention := int32(0)
			policy.Spec.Retention = &retention
			expectDenied(k8sClient.Create(ctx, policy), "retention", "1")
		})

		It("stores a valid policy and rejects an update that breaks it", func() {
			policy := validBackupPolicy("webhook-update")
			enableVerify(policy)
			policy.Spec.Verification = &opsv1alpha1.Verification{
				Image:   imageBusybox,
				Command: []string{cmdEcho, "ok"},
			}
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, policy)).To(Succeed())
			})

			policy.Spec.Schedule = scheduleOutOfRange
			expectDenied(k8sClient.Update(ctx, policy), fieldSchedule, "five-field cron")

			got := &opsv1alpha1.BackupPolicy{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), got)).To(Succeed())
			Expect(got.Spec.Schedule).To(Equal("0 0 * * *"))
			Expect(got.Spec.Verification.Command).To(Equal([]string{cmdEcho, "ok"}))
		})
	})

})

func validBackupPolicy(name string) *opsv1alpha1.BackupPolicy {
	retention := int32(7)
	verifyRestore := false
	return &opsv1alpha1.BackupPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: opsv1alpha1.BackupPolicySpec{
			TargetSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelApp: appIris},
			},
			Schedule:      "0 0 * * *",
			Retention:     &retention,
			VerifyRestore: &verifyRestore,
		},
	}
}

func enableVerify(policy *opsv1alpha1.BackupPolicy) {
	verifyRestore := true
	policy.Spec.VerifyRestore = &verifyRestore
}

func expectDenied(err error, fragments ...string) {
	Expect(err).To(HaveOccurred())
	for _, fragment := range fragments {
		Expect(err.Error()).To(ContainSubstring(fragment))
	}
}

func expectBackupPolicyDefaults(policy *opsv1alpha1.BackupPolicy) {
	Expect(policy.Spec.Schedule).To(Equal(opsv1alpha1.DefaultBackupSchedule))
	Expect(policy.Spec.Retention).NotTo(BeNil())
	Expect(*policy.Spec.Retention).To(Equal(opsv1alpha1.DefaultBackupRetention))
	Expect(policy.Spec.Suspend).NotTo(BeNil())
	Expect(*policy.Spec.Suspend).To(BeFalse())
}
