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
	"fmt"

	opsv1alpha1 "github.com/LeandroMileski/iris-backup-operator/api/v1alpha1"
)

// advanceVerification moves a RestoreVerification one step.
// An empty phase becomes Pending.
// Pending becomes Running.
// Running stays Running until finished is true, then it becomes Succeeded or Failed.
// Succeeded and Failed stay as they are. Finishing a terminal phase again is accepted
// only when the result matches the phase already recorded.
func advanceVerification(phase string, finished, succeeded bool) (string, error) {
	switch phase {
	case "":
		if finished {
			return "", fmt.Errorf("RestoreVerification has not started")
		}
		return opsv1alpha1.RestoreVerificationPhasePending, nil
	case opsv1alpha1.RestoreVerificationPhasePending:
		if finished {
			return "", fmt.Errorf("RestoreVerification phase %q cannot finish", phase)
		}
		return opsv1alpha1.RestoreVerificationPhaseRunning, nil
	case opsv1alpha1.RestoreVerificationPhaseRunning:
		if !finished {
			return phase, nil
		}
		if succeeded {
			return opsv1alpha1.RestoreVerificationPhaseSucceeded, nil
		}
		return opsv1alpha1.RestoreVerificationPhaseFailed, nil
	case opsv1alpha1.RestoreVerificationPhaseSucceeded, opsv1alpha1.RestoreVerificationPhaseFailed:
		if finished && phase != finishedPhase(succeeded) {
			return "", fmt.Errorf("RestoreVerification is already %s", phase)
		}
		return phase, nil
	default:
		return "", fmt.Errorf("unknown RestoreVerification phase %q", phase)
	}
}

func finishedPhase(succeeded bool) string {
	if succeeded {
		return opsv1alpha1.RestoreVerificationPhaseSucceeded
	}
	return opsv1alpha1.RestoreVerificationPhaseFailed
}
