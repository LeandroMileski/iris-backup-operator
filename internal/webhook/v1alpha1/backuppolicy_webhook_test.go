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
	)

	BeforeEach(func() {
		obj = &opsv1alpha1.BackupPolicy{}
		oldObj = &opsv1alpha1.BackupPolicy{}
		defaulter = BackupPolicyDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
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
			obj.Spec.Schedule = "0 1 * * *"
			obj.Spec.Retention = &retention
			obj.Spec.Suspend = &suspend

			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.Schedule).To(Equal("0 1 * * *"))
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
						MatchLabels: map[string]string{"app": "iris"},
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

})

func expectBackupPolicyDefaults(policy *opsv1alpha1.BackupPolicy) {
	Expect(policy.Spec.Schedule).To(Equal(opsv1alpha1.DefaultBackupSchedule))
	Expect(policy.Spec.Retention).NotTo(BeNil())
	Expect(*policy.Spec.Retention).To(Equal(opsv1alpha1.DefaultBackupRetention))
	Expect(policy.Spec.Suspend).NotTo(BeNil())
	Expect(*policy.Spec.Suspend).To(BeFalse())
}
