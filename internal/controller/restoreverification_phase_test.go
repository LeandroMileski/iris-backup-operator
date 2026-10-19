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

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

func TestAdvanceVerification(t *testing.T) {
	tests := []struct {
		name      string
		phase     string
		finished  bool
		succeeded bool
		want      string
		wantErr   bool
	}{
		{name: "empty becomes pending", want: opsv1alpha1.RestoreVerificationPhasePending},
		{name: "pending becomes running", phase: opsv1alpha1.RestoreVerificationPhasePending, want: opsv1alpha1.RestoreVerificationPhaseRunning},
		{name: "running waits", phase: opsv1alpha1.RestoreVerificationPhaseRunning, want: opsv1alpha1.RestoreVerificationPhaseRunning},
		{name: "running succeeds", phase: opsv1alpha1.RestoreVerificationPhaseRunning, finished: true, succeeded: true, want: opsv1alpha1.RestoreVerificationPhaseSucceeded},
		{name: "running fails", phase: opsv1alpha1.RestoreVerificationPhaseRunning, finished: true, want: opsv1alpha1.RestoreVerificationPhaseFailed},
		{name: "succeeded stays", phase: opsv1alpha1.RestoreVerificationPhaseSucceeded, want: opsv1alpha1.RestoreVerificationPhaseSucceeded},
		{name: "failed stays", phase: opsv1alpha1.RestoreVerificationPhaseFailed, want: opsv1alpha1.RestoreVerificationPhaseFailed},
		{name: "succeeded finish matches", phase: opsv1alpha1.RestoreVerificationPhaseSucceeded, finished: true, succeeded: true, want: opsv1alpha1.RestoreVerificationPhaseSucceeded},
		{name: "failed finish matches", phase: opsv1alpha1.RestoreVerificationPhaseFailed, finished: true, want: opsv1alpha1.RestoreVerificationPhaseFailed},
		{name: "succeeded cannot fail", phase: opsv1alpha1.RestoreVerificationPhaseSucceeded, finished: true, wantErr: true},
		{name: "failed cannot succeed", phase: opsv1alpha1.RestoreVerificationPhaseFailed, finished: true, succeeded: true, wantErr: true},
		{name: "pending cannot finish", phase: opsv1alpha1.RestoreVerificationPhasePending, finished: true, wantErr: true},
		{name: "empty cannot finish", finished: true, wantErr: true},
		{name: "unknown phase", phase: "Bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := advanceVerification(tt.phase, tt.finished, tt.succeeded)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("advance verification: %v", err)
			}
			if got != tt.want {
				t.Fatalf("phase = %q, want %q", got, tt.want)
			}
		})
	}
}
