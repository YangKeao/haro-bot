package agent

import (
	"testing"
	"time"
)

func TestSessionStateManagerUpdatesStartTimePerState(t *testing.T) {
	mgr := newSessionStateManager()
	sessionID := int64(42)

	mgr.SetState(sessionID, StateWaitingForLLM)
	first := mgr.GetStatus(sessionID)
	if first.State != StateWaitingForLLM {
		t.Fatalf("expected %s, got %s", StateWaitingForLLM, first.State)
	}
	if first.StartTime.IsZero() {
		t.Fatal("expected first start time to be set")
	}

	time.Sleep(5 * time.Millisecond)
	mgr.SetToolRunning(sessionID, "read_file")
	second := mgr.GetStatus(sessionID)
	if second.State != StateRunningTools {
		t.Fatalf("expected %s, got %s", StateRunningTools, second.State)
	}
	if !second.StartTime.After(first.StartTime) {
		t.Fatalf("expected tool state start time %v to be after %v", second.StartTime, first.StartTime)
	}
}
