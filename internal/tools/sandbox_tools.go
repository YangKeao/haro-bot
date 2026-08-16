package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/YangKeao/haro-bot/internal/sandbox"
)

type SandboxExecCommandTool struct {
	agentID int64
	service *sandbox.Service
}

type SandboxWriteStdinTool struct {
	agentID int64
	service *sandbox.Service
}

type SandboxListProcessesTool struct {
	agentID int64
	service *sandbox.Service
}

type SandboxStopProcessTool struct {
	agentID int64
	service *sandbox.Service
}

type SandboxTailProcessLogsTool struct {
	agentID int64
	service *sandbox.Service
}

func NewSandboxExecCommandTool(agentID int64, service *sandbox.Service) *SandboxExecCommandTool {
	return &SandboxExecCommandTool{agentID: agentID, service: service}
}

func NewSandboxWriteStdinTool(agentID int64, service *sandbox.Service) *SandboxWriteStdinTool {
	return &SandboxWriteStdinTool{agentID: agentID, service: service}
}

func NewSandboxListProcessesTool(agentID int64, service *sandbox.Service) *SandboxListProcessesTool {
	return &SandboxListProcessesTool{agentID: agentID, service: service}
}

func NewSandboxStopProcessTool(agentID int64, service *sandbox.Service) *SandboxStopProcessTool {
	return &SandboxStopProcessTool{agentID: agentID, service: service}
}

func NewSandboxTailProcessLogsTool(agentID int64, service *sandbox.Service) *SandboxTailProcessLogsTool {
	return &SandboxTailProcessLogsTool{agentID: agentID, service: service}
}

func (t *SandboxExecCommandTool) Name() string { return "exec_command" }
func (t *SandboxExecCommandTool) Description() string {
	return "Runs a command in the agent's isolated Sandbox. Processes persist until they exit or are explicitly stopped."
}
func (t *SandboxExecCommandTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"cmd":               map[string]any{"type": "string", "description": "Shell command to execute."},
		"workdir":           map[string]any{"type": "string", "description": "Working directory within /workspace."},
		"tty":               map[string]any{"type": "boolean", "description": "Allocate a PTY."},
		"background":        map[string]any{"type": "boolean", "description": "Return immediately and keep the process running."},
		"yield_time_ms":     map[string]any{"type": "number", "description": "How long to wait before returning a running process ID."},
		"max_output_tokens": map[string]any{"type": "number", "description": "Maximum approximate output tokens returned to the model."},
	}, "required": []string{"cmd"}, "additionalProperties": false}
}
func (t *SandboxExecCommandTool) Execute(ctx context.Context, tc ToolContext, raw json.RawMessage) (string, error) {
	if t == nil || t.service == nil {
		return "", errors.New("sandbox execution is not configured")
	}
	var input struct {
		Cmd             string `json:"cmd"`
		Workdir         string `json:"workdir"`
		TTY             bool   `json:"tty"`
		Background      bool   `json:"background"`
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
		Command: input.Cmd, Workdir: input.Workdir, TTY: input.TTY, Background: input.Background, YieldTimeMS: input.YieldTimeMS,
	})
	if err != nil {
		return "", err
	}
	return formatSandboxProcess(process, input.MaxOutputTokens), nil
}

func (t *SandboxWriteStdinTool) Name() string { return "write_stdin" }
func (t *SandboxWriteStdinTool) Description() string {
	return "Writes characters to a running Sandbox process and returns its latest output."
}
func (t *SandboxWriteStdinTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"process_id":        map[string]any{"type": "string", "description": "Opaque process ID returned by exec_command."},
		"chars":             map[string]any{"type": "string", "description": "Characters to write; may be empty to poll."},
		"yield_time_ms":     map[string]any{"type": "number"},
		"max_output_tokens": map[string]any{"type": "number"},
	}, "required": []string{"process_id"}, "additionalProperties": false}
}
func (t *SandboxWriteStdinTool) Execute(ctx context.Context, _ ToolContext, raw json.RawMessage) (string, error) {
	var input struct {
		ProcessID       string `json:"process_id"`
		Chars           string `json:"chars"`
		YieldTimeMS     int    `json:"yield_time_ms"`
		MaxOutputTokens *int   `json:"max_output_tokens"`
	}
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.ProcessID) == "" {
		return "", errors.New("process_id is required")
	}
	process, err := t.service.WriteProcessStdin(ctx, t.agentID, input.ProcessID, sandbox.StdinRequest{Chars: input.Chars, YieldTimeMS: input.YieldTimeMS})
	if err != nil {
		return "", err
	}
	return formatSandboxProcess(process, input.MaxOutputTokens), nil
}

func (t *SandboxListProcessesTool) Name() string { return "list_processes" }
func (t *SandboxListProcessesTool) Description() string {
	return "Lists processes started by all agents sharing this Sandbox."
}
func (t *SandboxListProcessesTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
}
func (t *SandboxListProcessesTool) Execute(ctx context.Context, _ ToolContext, raw json.RawMessage) (string, error) {
	if err := decodeToolArgs(raw, &struct{}{}); err != nil {
		return "", err
	}
	processes, err := t.service.ListProcessesForAgent(ctx, t.agentID)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]any{"processes": processes})
	return string(data), err
}

func (t *SandboxStopProcessTool) Name() string { return "stop_process" }
func (t *SandboxStopProcessTool) Description() string {
	return "Sends TERM or KILL to a process in the shared Sandbox."
}
func (t *SandboxStopProcessTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"process_id": map[string]any{"type": "string"},
		"signal":     map[string]any{"type": "string", "enum": []string{"TERM", "KILL"}},
	}, "required": []string{"process_id"}, "additionalProperties": false}
}
func (t *SandboxStopProcessTool) Execute(ctx context.Context, _ ToolContext, raw json.RawMessage) (string, error) {
	var input struct {
		ProcessID string `json:"process_id"`
		Signal    string `json:"signal"`
	}
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	if input.Signal == "" {
		input.Signal = "TERM"
	}
	process, err := t.service.SignalProcess(ctx, t.agentID, input.ProcessID, input.Signal)
	if err != nil {
		return "", err
	}
	return formatSandboxProcess(process, nil), nil
}

func (t *SandboxTailProcessLogsTool) Name() string { return "tail_process_logs" }
func (t *SandboxTailProcessLogsTool) Description() string {
	return "Returns the current bounded log buffer and status for a Sandbox process."
}
func (t *SandboxTailProcessLogsTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"process_id":        map[string]any{"type": "string"},
		"max_output_tokens": map[string]any{"type": "number"},
	}, "required": []string{"process_id"}, "additionalProperties": false}
}
func (t *SandboxTailProcessLogsTool) Execute(ctx context.Context, _ ToolContext, raw json.RawMessage) (string, error) {
	var input struct {
		ProcessID       string `json:"process_id"`
		MaxOutputTokens *int   `json:"max_output_tokens"`
	}
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	process, err := t.service.GetProcess(ctx, t.agentID, input.ProcessID)
	if err != nil {
		return "", err
	}
	return formatSandboxProcess(process, input.MaxOutputTokens), nil
}

func decodeToolArgs(raw json.RawMessage, output any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(output)
}

func formatSandboxProcess(process sandbox.Process, maxTokens *int) string {
	output := process.Output
	truncated := process.OutputTruncated
	if maxTokens != nil && *maxTokens > 0 {
		limit := *maxTokens * 4
		if len(output) > limit {
			output = output[len(output)-limit:]
			truncated = true
		}
	}
	lines := []string{
		fmt.Sprintf("Process ID: %s", process.ID),
		fmt.Sprintf("Status: %s", process.Status),
		fmt.Sprintf("Wall time: %.4f seconds", float64(process.DurationMillis)/1000),
	}
	if process.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("Process exited with code %d", *process.ExitCode))
	}
	if truncated {
		lines = append(lines, "Output is truncated to the most recent data.")
	}
	lines = append(lines, "Output:", output)
	return strings.Join(lines, "\n")
}
