//go:build integration

package fork_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/fork"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

func TestForkInterruptFlow(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	ctx := context.Background()
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 8, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	forkMgr := fork.NewManager(agentSvc, store)
	forkTool := fork.NewForkTool(forkMgr)
	interruptTool := fork.NewForkInterruptTool(forkMgr)
	registry.Register(forkTool)
	registry.Register(interruptTool)

	userID, err := store.GetOrCreateUserByTelegramID(ctx, 2001)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	parentID, err := store.GetOrCreateSession(ctx, userID, "parent")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	startOut, err := forkTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"input": "Reply with 'child done'.",
	}))
	if err != nil {
		t.Fatalf("fork start: %v", err)
	}
	var startResp struct {
		ChildSessionID int64 `json:"child_session_id"`
	}
	if err := json.Unmarshal([]byte(startOut), &startResp); err != nil {
		t.Fatalf("parse fork response: %v", err)
	}
	if startResp.ChildSessionID == 0 {
		t.Fatalf("missing child_session_id")
	}

	waitForStatus(t, forkMgr, parentID, startResp.ChildSessionID, "completed", 30*time.Second)

	resp, err := interruptTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"child_session_id": startResp.ChildSessionID,
		"message":          "INTERRUPT:status",
		"store_in_child":   false,
	}))
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if resp == "" {
		t.Fatalf("unexpected empty interrupt response")
	}

	msgs, _, err := store.LoadViewMessages(ctx, startResp.ChildSessionID, 50)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	for _, m := range msgs {
		if m.Content == "INTERRUPT:status" {
			t.Fatalf("interrupt was stored despite store_in_child=false")
		}
	}

	resp, err = interruptTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"child_session_id": startResp.ChildSessionID,
		"message":          "INTERRUPT:note",
		"store_in_child":   true,
	}))
	if err != nil {
		t.Fatalf("interrupt store: %v", err)
	}
	if resp == "" {
		t.Fatalf("unexpected empty interrupt response")
	}
	msgs, _, err = store.LoadViewMessages(ctx, startResp.ChildSessionID, 50)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	found := false
	for _, m := range msgs {
		if m.Content == "INTERRUPT:note" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("interrupt message not stored in child session")
	}
}

func TestForkStatusAndCancel(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	registry.Register(sleepTool{})
	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 8, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	forkMgr := fork.NewManager(agentSvc, store)
	forkTool := fork.NewForkTool(forkMgr)
	cancelTool := fork.NewForkCancelTool(forkMgr)
	statusTool := fork.NewForkStatusTool(forkMgr)

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(ctx, 2002)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	parentID, err := store.GetOrCreateSession(ctx, userID, "parent-2")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	startOut, err := forkTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"input": "Call sleep_tool with {\"ms\":1000} before replying. You must use the tool.",
	}))
	if err != nil {
		t.Fatalf("fork start: %v", err)
	}
	var startResp struct {
		ChildSessionID int64 `json:"child_session_id"`
	}
	if err := json.Unmarshal([]byte(startOut), &startResp); err != nil {
		t.Fatalf("parse fork response: %v", err)
	}
	if startResp.ChildSessionID == 0 {
		t.Fatalf("missing child_session_id")
	}

	statusOut, err := statusTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"child_session_id": startResp.ChildSessionID,
	}))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(statusOut, "running") {
		t.Fatalf("expected running status, got %q", statusOut)
	}

	time.Sleep(50 * time.Millisecond)
	if _, err := cancelTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"child_session_id": startResp.ChildSessionID,
	})); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	waitForTerminalStatus(t, forkMgr, parentID, startResp.ChildSessionID, "cancelled", 30*time.Second)
	// Ensure status doesn't flip after cancellation.
	time.Sleep(100 * time.Millisecond)
	statusOut, err = statusTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"child_session_id": startResp.ChildSessionID,
	}))
	if err != nil {
		t.Fatalf("status after cancel: %v", err)
	}
	if !strings.Contains(statusOut, "cancelled") {
		t.Fatalf("expected cancelled status, got %q", statusOut)
	}
}

func TestForkInheritRecent(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 8, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})
	forkMgr := fork.NewManager(agentSvc, store)
	forkTool := fork.NewForkTool(forkMgr)

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(ctx, 2003)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	parentID, err := store.GetOrCreateSession(ctx, userID, "parent-3")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AddMessage(ctx, parentID, "user", "parent-msg-1", nil); err != nil {
		t.Fatalf("add parent msg: %v", err)
	}
	if err := store.AddMessage(ctx, parentID, "assistant", "parent-msg-2", nil); err != nil {
		t.Fatalf("add parent msg: %v", err)
	}

	startOut, err := forkTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"input":          "child task",
		"inherit_recent": 2,
	}))
	if err != nil {
		t.Fatalf("fork start: %v", err)
	}
	var startResp struct {
		ChildSessionID int64 `json:"child_session_id"`
	}
	if err := json.Unmarshal([]byte(startOut), &startResp); err != nil {
		t.Fatalf("parse fork response: %v", err)
	}
	if startResp.ChildSessionID == 0 {
		t.Fatalf("missing child_session_id")
	}
	waitForStatus(t, forkMgr, parentID, startResp.ChildSessionID, "completed", 30*time.Second)

	msgs, _, err := store.LoadViewMessages(ctx, startResp.ChildSessionID, 10)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	found1 := false
	found2 := false
	for _, m := range msgs {
		if m.Content == "parent-msg-1" {
			found1 = true
		}
		if m.Content == "parent-msg-2" {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Fatalf("expected inherited messages, got: %+v", msgs)
	}
}

func TestForkContextCancel(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	registry.Register(sleepTool{})
	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 8, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	forkMgr := fork.NewManager(agentSvc, store)

	baseCtx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(baseCtx, 2004)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	parentID, err := store.GetOrCreateSession(baseCtx, userID, "parent-4")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	ctx, cancel := context.WithCancel(baseCtx)
	childID, err := forkMgr.Start(ctx, parentID, userID, "Call sleep_tool with {\"ms\":1000} before replying. You must use the tool.", "", 0)
	if err != nil {
		t.Fatalf("fork start: %v", err)
	}
	cancel()

	waitForTerminalStatus(t, forkMgr, parentID, childID, "cancelled", 30*time.Second)
}

func TestForkCleanupCompletedRun(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 8, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	forkMgr := fork.NewManagerWithOptions(agentSvc, store, fork.ManagerOptions{
		CleanupAfter: 50 * time.Millisecond,
	})
	forkTool := fork.NewForkTool(forkMgr)

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(ctx, 2005)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	parentID, err := store.GetOrCreateSession(ctx, userID, "parent-5")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	startOut, err := forkTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"input": "Reply with 'done'.",
	}))
	if err != nil {
		t.Fatalf("fork start: %v", err)
	}
	var startResp struct {
		ChildSessionID int64 `json:"child_session_id"`
	}
	if err := json.Unmarshal([]byte(startOut), &startResp); err != nil {
		t.Fatalf("parse fork response: %v", err)
	}
	if startResp.ChildSessionID == 0 {
		t.Fatalf("missing child_session_id")
	}
	waitForTerminalStatus(t, forkMgr, parentID, startResp.ChildSessionID, "completed", 30*time.Second)

	deadline := time.After(60 * time.Second)
	for {
		if _, err := forkMgr.Status(parentID, startResp.ChildSessionID); err != nil {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected status lookup to fail after cleanup")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func waitForStatus(t *testing.T, mgr *fork.Manager, parentID, childID int64, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		status, err := mgr.Status(parentID, childID)
		if err == nil && status == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for status %q (last: %q, err: %v)", want, status, err)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func waitForTerminalStatus(t *testing.T, mgr *fork.Manager, parentID, childID int64, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		status, err := mgr.Status(parentID, childID)
		if err == nil && status != "running" {
			if status != want {
				t.Fatalf("expected status %q, got %q", want, status)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for status %q (last: %q, err: %v)", want, status, err)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	return b
}

type sleepTool struct{}

func (sleepTool) Name() string        { return "sleep_tool" }
func (sleepTool) Description() string { return "sleep for the given duration in milliseconds" }
func (sleepTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ms": map[string]any{"type": "integer"},
		},
		"required": []string{"ms"},
	}
}

func (sleepTool) Execute(ctx context.Context, _ tools.ToolContext, args json.RawMessage) (string, error) {
	var payload struct {
		MS int `json:"ms"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if payload.MS <= 0 {
		payload.MS = 200
	}
	timer := time.NewTimer(time.Duration(payload.MS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "slept", nil
	}
}
