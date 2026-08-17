package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/google/uuid"
)

type SandboxExecCommandTool struct {
	agentID int64
	service *sandbox.Service
}

type SandboxWriteStdinTool struct {
	agentID int64
	service *sandbox.Service
}

func NewSandboxExecCommandTool(agentID int64, service *sandbox.Service) *SandboxExecCommandTool {
	return &SandboxExecCommandTool{agentID: agentID, service: service}
}

func NewSandboxWriteStdinTool(agentID int64, service *sandbox.Service) *SandboxWriteStdinTool {
	return &SandboxWriteStdinTool{agentID: agentID, service: service}
}

func (t *SandboxExecCommandTool) Name() string { return "exec_command" }
func (t *SandboxExecCommandTool) Description() string {
	return "Runs a shell command in the agent's isolated sandbox. Waits up to yield_time_ms (10 seconds by default) and returns a session_id when the command is still running. Use write_stdin with that session_id to send input or wait for more output."
}
func (t *SandboxExecCommandTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"cmd":               map[string]any{"type": "string", "description": "Shell command to execute."},
		"workdir":           map[string]any{"type": "string", "description": "Working directory within /workspace."},
		"shell":             map[string]any{"type": "string", "description": "Shell executable. Defaults to /bin/sh."},
		"login":             map[string]any{"type": "boolean", "description": "Run the shell as a login shell. Defaults to true."},
		"tty":               map[string]any{"type": "boolean", "description": "Allocate a PTY for an interactive command."},
		"yield_time_ms":     map[string]any{"type": "integer", "minimum": sandbox.MinYieldTimeMS, "maximum": sandbox.MaxYieldTimeMS, "description": "How long to wait for output or completion. Defaults to 10000 ms."},
		"max_output_tokens": map[string]any{"type": "integer", "minimum": 1, "description": "Approximate output token budget. Defaults to 10000 tokens."},
	}, "required": []string{"cmd"}, "additionalProperties": false}
}
func (t *SandboxExecCommandTool) Execute(ctx context.Context, tc ToolContext, raw json.RawMessage) (string, error) {
	if t == nil || t.service == nil {
		return "", errors.New("sandbox execution is not configured")
	}
	var input struct {
		Cmd             string `json:"cmd"`
		Workdir         string `json:"workdir"`
		Shell           string `json:"shell"`
		Login           *bool  `json:"login"`
		TTY             bool   `json:"tty"`
		YieldTimeMS     int    `json:"yield_time_ms"`
		MaxOutputTokens *int   `json:"max_output_tokens"`
	}
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Cmd) == "" {
		return "", errors.New("cmd must not be empty")
	}
	process, err := t.service.StartProcess(ctx, t.agentID, tc.SessionID, sandbox.ExecRequest{
		Command: input.Cmd, Workdir: input.Workdir, Shell: input.Shell, Login: input.Login,
		TTY: input.TTY, YieldTimeMS: sandbox.ExecYieldTimeMS(input.YieldTimeMS),
	})
	if err != nil {
		return "", err
	}
	return formatSandboxProcess(process, input.MaxOutputTokens)
}

func (t *SandboxWriteStdinTool) Name() string { return "write_stdin" }
func (t *SandboxWriteStdinTool) Description() string {
	return "Writes characters to an existing exec_command session and returns only new output. Pass an empty chars string to wait for more output. Empty polls wait at least 5 seconds; non-empty writes wait 250 ms by default."
}
func (t *SandboxWriteStdinTool) Parameters() map[string]any {
	maxYield := sandbox.DefaultBackgroundTerminalMaxTimeoutMS
	if t != nil && t.service != nil && t.service.Config().BackgroundTerminalMaxTimeoutMS > 0 {
		maxYield = t.service.Config().BackgroundTerminalMaxTimeoutMS
	}
	if maxYield < sandbox.MinEmptyWriteYieldTimeMS {
		maxYield = sandbox.MinEmptyWriteYieldTimeMS
	}
	return map[string]any{"type": "object", "properties": map[string]any{
		"session_id":        map[string]any{"type": "string", "description": "Opaque session ID returned by exec_command."},
		"chars":             map[string]any{"type": "string", "description": "Characters to write. Use an empty string to wait for output."},
		"yield_time_ms":     map[string]any{"type": "integer", "minimum": sandbox.MinYieldTimeMS, "maximum": maxYield, "description": "How long to wait for output or completion. Empty polls are clamped to at least 5000 ms; non-empty writes are capped at 30000 ms."},
		"max_output_tokens": map[string]any{"type": "integer", "minimum": 1, "description": "Approximate output token budget. Defaults to 10000 tokens."},
	}, "required": []string{"session_id"}, "additionalProperties": false}
}
func (t *SandboxWriteStdinTool) Execute(ctx context.Context, _ ToolContext, raw json.RawMessage) (string, error) {
	if t == nil || t.service == nil {
		return "", errors.New("sandbox execution is not configured")
	}
	var input struct {
		SessionID       string `json:"session_id"`
		Chars           string `json:"chars"`
		YieldTimeMS     int    `json:"yield_time_ms"`
		MaxOutputTokens *int   `json:"max_output_tokens"`
	}
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return "", errors.New("session_id is required")
	}
	process, err := t.service.WriteProcessStdin(ctx, t.agentID, input.SessionID, sandbox.StdinRequest{
		Chars: input.Chars, YieldTimeMS: input.YieldTimeMS,
	})
	if err != nil {
		return "", err
	}
	return formatSandboxProcess(process, input.MaxOutputTokens)
}

func decodeToolArgs(raw json.RawMessage, output any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(output)
}

type sandboxProcessResult struct {
	ChunkID            string  `json:"chunk_id"`
	WallTimeSeconds    float64 `json:"wall_time_seconds"`
	Output             string  `json:"output"`
	SessionID          string  `json:"session_id,omitempty"`
	ExitCode           *int    `json:"exit_code,omitempty"`
	OriginalTokenCount *int    `json:"original_token_count,omitempty"`
}

func formatSandboxProcess(process sandbox.Process, maxTokens *int) (string, error) {
	output := process.Output
	truncated := process.OutputTruncated
	if process.InteractionOutputAvailable {
		output = process.InteractionOutput
		truncated = process.InteractionOutputTruncated
	}

	budget := sandbox.DefaultMaxOutputTokens
	if maxTokens != nil && *maxTokens > 0 {
		budget = *maxTokens
	}
	originalTokens := approximateTokens(output)
	if process.InteractionOutputAvailable && process.InteractionOriginalBytes > len(output) {
		originalTokens = (process.InteractionOriginalBytes + 3) / 4
	}
	output, budgetTruncated := truncateProcessOutput(output, budget)
	truncated = truncated || budgetTruncated

	result := sandboxProcessResult{
		ChunkID:         strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		WallTimeSeconds: float64(process.DurationMillis) / 1000,
		Output:          output,
	}
	if process.Status == sandbox.RunStarting || process.Status == sandbox.RunRunning {
		result.SessionID = process.ID
	} else {
		result.ExitCode = process.ExitCode
	}
	if truncated {
		result.OriginalTokenCount = &originalTokens
	}
	data, err := json.Marshal(result)
	return string(data), err
}

func truncateProcessOutput(output string, maxTokens int) (string, bool) {
	limit := maxTokens * 4
	if limit <= 0 || len(output) <= limit {
		return output, false
	}
	const marker = "\n... output omitted ...\n"
	available := limit - len(marker)
	if available <= 0 {
		return output[len(output)-limit:], true
	}
	head := available / 2
	tail := available - head
	return fmt.Sprintf("%s%s%s", output[:head], marker, output[len(output)-tail:]), true
}

func approximateTokens(output string) int {
	if output == "" {
		return 0
	}
	return (len(output) + 3) / 4
}
