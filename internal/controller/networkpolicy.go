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

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const verificationNetworkPolicyName = "iris-backup-verification-deny"

// ensureVerificationNetworkPolicy installs a namespace policy that selects verification Pods and allows no traffic.
// One policy covers every verification in the namespace. A policy that has been changed is restored.
func (r *RestoreVerificationReconciler) ensureVerificationNetworkPolicy(ctx context.Context, namespace string) error {
	log := logf.FromContext(ctx)
	desired := newVerificationNetworkPolicy(namespace)
	existing := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			if apierrors.IsAlreadyExists(err) {
				log.Info("NetworkPolicy already exists", "name", desired.Name, "namespace", namespace)
				return nil
			}
			log.Error(err, "Failed to create NetworkPolicy", "name", desired.Name, "namespace", namespace)
			return err
		}
		log.Info("Created NetworkPolicy", "name", desired.Name, "namespace", namespace)
		return nil
	}
	if err != nil {
		log.Error(err, "Failed to get NetworkPolicy", "name", desired.Name, "namespace", namespace)
		return err
	}
	if equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		return nil
	}
	existing.Spec = desired.Spec
	if err := r.Update(ctx, existing); err != nil {
		log.Error(err, "Failed to update NetworkPolicy", "name", desired.Name, "namespace", namespace)
		return err
	}
	log.Info("Updated NetworkPolicy", "name", desired.Name, "namespace", namespace)
	return nil
}

// newVerificationNetworkPolicy denies every ingress and egress packet for verification Pods.
// Naming Ingress or Egress in policyTypes with no rules allows no traffic of that kind.
func newVerificationNetworkPolicy(namespace string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      verificationNetworkPolicyName,
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      labelRestoreVerification,
					Operator: metav1.LabelSelectorOpExists,
				}},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
}
