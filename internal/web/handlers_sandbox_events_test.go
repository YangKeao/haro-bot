package web

import (
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
)

func TestSandboxFingerprintIgnoresObservationClockButTracksState(t *testing.T) {
	first := sandbox.Profile{ID: 1, RuntimeStatus: "Starting", RuntimeDetails: &sandbox.RuntimeDetails{State: "Starting", ObservedAt: time.Now()}}
	second := first
	details := *first.RuntimeDetails
	details.ObservedAt = details.ObservedAt.Add(time.Minute)
	second.RuntimeDetails = &details
	if sandboxFingerprints([]sandbox.Profile{first})[1] != sandboxFingerprints([]sandbox.Profile{second})[1] {
		t.Fatal("observation timestamp caused a spurious status event")
	}
	second.RuntimeStatus = "Ready"
	second.RuntimeDetails.State = "Ready"
	if sandboxFingerprints([]sandbox.Profile{first})[1] == sandboxFingerprints([]sandbox.Profile{second})[1] {
		t.Fatal("runtime state change did not produce a new fingerprint")
	}
}

func TestTerminalOutputSinceHandlesTruncatedRuntimeLog(t *testing.T) {
	chunk, next := terminalOutputSince(sandbox.Process{Output: "def", OutputOffset: 3}, 1)
	if chunk != "def" || next != 6 {
		t.Fatalf("chunk = %q, next = %d", chunk, next)
	}
	chunk, next = terminalOutputSince(sandbox.Process{Output: "abcdef", OutputOffset: 0}, 3)
	if chunk != "def" || next != 6 {
		t.Fatalf("chunk = %q, next = %d", chunk, next)
	}
}
