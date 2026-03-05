package fork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/YangKeao/haro-bot/internal/tools"
)

type ForkTool struct {
	manager *Manager
}

type forkArgs struct {
	Input         string `json:"input"`
	Model         string `json:"model"`
	Goal          string `json:"goal"`
	InheritRecent int    `json:"inherit_recent"`
}

type forkResult struct {
	ChildSessionID int64  `json:"child_session_id"`
	Status         string `json:"status"`
}

func NewForkTool(manager *Manager) *ForkTool {
	return &ForkTool{manager: manager}
}

func (t *ForkTool) Name() string { return "fork" }
func (t *ForkTool) Description() string {
	return "Start a child session to execute a sub-task immediately."
}
func (t *ForkTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "Initial instruction for the child session",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override",
			},
			"goal": map[string]any{
				"type":        "string",
				"description": "Optional goal/context for tracking",
			},
			"inherit_recent": map[string]any{
				"type":        "integer",
				"description": "Number of recent messages to inherit from parent session (default 8)",
			},
		},
		"required": []string{"input"},
	}
}

func (t *ForkTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
	if t.manager == nil {
		return "", errors.New("fork manager not configured")
	}
	var payload forkArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if payload.Input == "" {
		return "", errors.New("fork input required")
	}
	childID, err := t.manager.Start(ctx, tc.SessionID, tc.UserID, payload.Input, payload.Model, payload.InheritRecent)
	if err != nil {
		return "", err
	}
	resp := forkResult{ChildSessionID: childID, Status: "running"}
	b, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type ForkInterruptTool struct {
	manager *Manager
}

type forkInterruptArgs struct {
	ChildSessionID int64  `json:"child_session_id"`
	Message        string `json:"message"`
	StoreInChild   bool   `json:"store_in_child"`
	Model          string `json:"model"`
}

func NewForkInterruptTool(manager *Manager) *ForkInterruptTool {
	return &ForkInterruptTool{manager: manager}
}

func (t *ForkInterruptTool) Name() string { return "fork_interrupt" }
func (t *ForkInterruptTool) Description() string {
	return "Interrupt a child session to get a response from its current context; optionally store the interrupt in the child history."
}
func (t *ForkInterruptTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"child_session_id": map[string]any{
				"type":        "integer",
				"description": "Child session id",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Interrupt message",
			},
			"store_in_child": map[string]any{
				"type":        "boolean",
				"description": "Store interrupt and response in child context if true",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override",
			},
		},
		"required": []string{"child_session_id", "message"},
	}
}

func (t *ForkInterruptTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
	if t.manager == nil {
		return "", errors.New("fork manager not configured")
	}
	var payload forkInterruptArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if payload.ChildSessionID <= 0 {
		return "", errors.New("child_session_id required")
	}
	if payload.Message == "" {
		return "", errors.New("message required")
	}
	resp, err := t.manager.Interrupt(ctx, tc.SessionID, payload.ChildSessionID, payload.Message, payload.StoreInChild, payload.Model)
	if err != nil {
		return "", err
	}
	return resp, nil
}

type ForkCancelTool struct {
	manager *Manager
}

type forkCancelArgs struct {
	ChildSessionID int64 `json:"child_session_id"`
}

func NewForkCancelTool(manager *Manager) *ForkCancelTool {
	return &ForkCancelTool{manager: manager}
}

func (t *ForkCancelTool) Name() string { return "fork_cancel" }
func (t *ForkCancelTool) Description() string {
	return "Cancel a child session started via fork."
}
func (t *ForkCancelTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"child_session_id": map[string]any{
				"type":        "integer",
				"description": "Child session id",
			},
		},
		"required": []string{"child_session_id"},
	}
}

func (t *ForkCancelTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
	if t.manager == nil {
		return "", errors.New("fork manager not configured")
	}
	var payload forkCancelArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if payload.ChildSessionID <= 0 {
		return "", errors.New("child_session_id required")
	}
	if err := t.manager.Cancel(tc.SessionID, payload.ChildSessionID); err != nil {
		return "", err
	}
	return "ok", nil
}

type ForkStatusTool struct {
	manager *Manager
}

type forkStatusArgs struct {
	ChildSessionID int64 `json:"child_session_id"`
}

func NewForkStatusTool(manager *Manager) *ForkStatusTool {
	return &ForkStatusTool{manager: manager}
}

func (t *ForkStatusTool) Name() string { return "fork_status" }
func (t *ForkStatusTool) Description() string {
	return "Get the status of a child session started via fork."
}
func (t *ForkStatusTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"child_session_id": map[string]any{
				"type":        "integer",
				"description": "Child session id",
			},
		},
		"required": []string{"child_session_id"},
	}
}

func (t *ForkStatusTool) Execute(ctx context.Context, tc tools.ToolContext, args json.RawMessage) (string, error) {
	if t.manager == nil {
		return "", errors.New("fork manager not configured")
	}
	var payload forkStatusArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if payload.ChildSessionID <= 0 {
		return "", errors.New("child_session_id required")
	}
	status, err := t.manager.Status(tc.SessionID, payload.ChildSessionID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("{\"status\":\"%s\"}", status), nil
}
