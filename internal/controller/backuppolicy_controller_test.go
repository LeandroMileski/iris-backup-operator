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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

var _ = Describe("BackupPolicy Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = testNamespace
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		backuppolicy := &opsv1alpha1.BackupPolicy{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind BackupPolicy")
			err := k8sClient.Get(ctx, typeNamespacedName, backuppolicy)
			if err != nil && errors.IsNotFound(err) {
				resource := &opsv1alpha1.BackupPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: opsv1alpha1.BackupPolicySpec{
						TargetSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{labelApp: appIris},
						},
						Schedule: testSchedule,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance BackupPolicy")
			finishPolicy(ctx, typeNamespacedName)
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &BackupPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Now:    func() time.Time { return time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC) },
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &opsv1alpha1.BackupPolicy{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			expectReady(updated)
		})
	})
})
