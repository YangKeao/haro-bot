package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/sandbox"
)

func TestFormatSandboxProcessUsesUnreadInteractionOutput(t *testing.T) {
	process := sandbox.Process{
		ID: "session-1", Status: sandbox.RunRunning, DurationMillis: 12_500,
		Output: "old output\nnew output\n", InteractionOutput: "new output\n", InteractionOutputAvailable: true,
	}
	encoded, err := formatSandboxProcess(process, nil)
	if err != nil {
		t.Fatal(err)
	}
	var result sandboxProcessResult
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "session-1" || result.Output != "new output\n" || result.ExitCode != nil || result.WallTimeSeconds != 12.5 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSandboxToolSchemasExposeOnlyCodexProcessControls(t *testing.T) {
	execProperties := NewSandboxExecCommandTool(1, nil).Parameters()["properties"].(map[string]any)
	if _, exists := execProperties["background"]; exists {
		t.Fatal("exec_command must not expose background")
	}
	stdinProperties := NewSandboxWriteStdinTool(1, nil).Parameters()["properties"].(map[string]any)
	if _, exists := stdinProperties["session_id"]; !exists {
		t.Fatal("write_stdin must expose session_id")
	}
	if _, exists := stdinProperties["process_id"]; exists {
		t.Fatal("write_stdin must not expose the historical process_id alias")
	}
}

func TestFormatSandboxProcessReturnsTerminalExitWithoutSession(t *testing.T) {
	exitCode := 0
	process := sandbox.Process{ID: "session-1", Status: sandbox.RunExited, ExitCode: &exitCode, InteractionOutputAvailable: true}
	encoded, err := formatSandboxProcess(process, nil)
	if err != nil {
		t.Fatal(err)
	}
	var result sandboxProcessResult
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "" || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestTruncateProcessOutputKeepsHeadAndTail(t *testing.T) {
	value := strings.Repeat("a", 30) + strings.Repeat("z", 30)
	got, truncated := truncateProcessOutput(value, 10)
	if !truncated || len(got) > 40 || !strings.Contains(got, "output omitted") || !strings.HasPrefix(got, "a") || !strings.HasSuffix(got, "z") {
		t.Fatalf("unexpected truncation: %q", got)
	}
}
