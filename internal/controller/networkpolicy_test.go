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
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestVerificationNetworkPolicyDeniesAllTraffic(t *testing.T) {
	policy := newVerificationNetworkPolicy("app")
	if policy.Namespace != "app" || policy.Name != verificationNetworkPolicyName {
		t.Fatalf("policy = %s/%s, want app/%s", policy.Namespace, policy.Name, verificationNetworkPolicyName)
	}
	if len(policy.Spec.Ingress) != 0 || len(policy.Spec.Egress) != 0 {
		t.Fatalf("ingress = %d, egress = %d, want no allow rules", len(policy.Spec.Ingress), len(policy.Spec.Egress))
	}
	if len(policy.Spec.PolicyTypes) != 2 ||
		policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress ||
		policy.Spec.PolicyTypes[1] != networkingv1.PolicyTypeEgress {
		t.Fatalf("policyTypes = %v, want Ingress and Egress", policy.Spec.PolicyTypes)
	}

	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
	if err != nil {
		t.Fatalf("LabelSelectorAsSelector() unexpected error: %v", err)
	}
	verificationPod := labels.Set(restoreLabels("check-data"))
	if !selector.Matches(verificationPod) {
		t.Fatal("verification Pod was not selected")
	}
	other := labels.Set(map[string]string{"app": "web"})
	if selector.Matches(other) {
		t.Fatal("unrelated Pod was selected")
	}
	if selector.Empty() {
		t.Fatal("selector is empty and would match every Pod")
	}
}
