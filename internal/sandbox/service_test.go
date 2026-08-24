package sandbox

import "testing"

func TestOperationCompletedRequiresReplacementPodForRestart(t *testing.T) {
	profile := Profile{Operation: OperationRestart, OperationPreviousPodUID: "old"}
	if operationCompleted(profile, RuntimeDetails{State: "Ready", Pod: &PodRuntimeStatus{UID: "old"}}) {
		t.Fatal("restart completed while the old Pod was still running")
	}
	if !operationCompleted(profile, RuntimeDetails{State: "Ready", Pod: &PodRuntimeStatus{UID: "new"}}) {
		t.Fatal("restart did not complete after the replacement Pod became Ready")
	}
}
