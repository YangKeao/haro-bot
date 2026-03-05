//go:build integration

package fork_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/fork"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

func TestForkInterruptFlow(t *testing.T) {
	gdb, cleanup := testutil.NewTestDB(t)
	t.Cleanup(cleanup)
	if err := db.ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	llmServer := newFakeLLMServer(t)
	t.Cleanup(llmServer.Close)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	registry.Register(tools.NewActivateSkillTool(skillsMgr))
	agentSvc := agent.New(store, skillsMgr, registry, t.TempDir(), 8, llm.NewClient(llmServer.URL, ""), "fake-model", "openai")

	forkMgr := fork.NewManager(agentSvc, store)
	forkTool := fork.NewForkTool(forkMgr)
	interruptTool := fork.NewForkInterruptTool(forkMgr)
	registry.Register(forkTool)
	registry.Register(interruptTool)

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "u-1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	parentID, err := store.GetOrCreateSession(ctx, userID, "parent")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	startOut, err := forkTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"input": "child task",
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

	waitForStatus(t, forkMgr, parentID, startResp.ChildSessionID, "completed", 2*time.Second)

	resp, err := interruptTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"child_session_id": startResp.ChildSessionID,
		"message":          "INTERRUPT:status",
		"store_in_child":   false,
	}))
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if resp != "echo:INTERRUPT:status" {
		t.Fatalf("unexpected interrupt response: %q", resp)
	}

	msgs, err := store.LoadRecentMessages(ctx, startResp.ChildSessionID, 50)
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
	if resp != "echo:INTERRUPT:note" {
		t.Fatalf("unexpected interrupt response: %q", resp)
	}
	msgs, err = store.LoadRecentMessages(ctx, startResp.ChildSessionID, 50)
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
	gdb, cleanup := testutil.NewTestDB(t)
	t.Cleanup(cleanup)
	if err := db.ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	llmServer := newBlockingLLMServer(t)
	t.Cleanup(llmServer.Close)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	registry.Register(tools.NewActivateSkillTool(skillsMgr))
	agentSvc := agent.New(store, skillsMgr, registry, t.TempDir(), 8, llm.NewClient(llmServer.URL, ""), "fake-model", "openai")

	forkMgr := fork.NewManager(agentSvc, store)
	forkTool := fork.NewForkTool(forkMgr)
	cancelTool := fork.NewForkCancelTool(forkMgr)
	statusTool := fork.NewForkStatusTool(forkMgr)

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "u-2")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	parentID, err := store.GetOrCreateSession(ctx, userID, "parent-2")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	startOut, err := forkTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"input": "child task that blocks",
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

	if _, err := cancelTool.Execute(ctx, tools.ToolContext{SessionID: parentID, UserID: userID}, mustJSON(t, map[string]any{
		"child_session_id": startResp.ChildSessionID,
	})); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	waitForStatus(t, forkMgr, parentID, startResp.ChildSessionID, "cancelled", 2*time.Second)
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
	gdb, cleanup := testutil.NewTestDB(t)
	t.Cleanup(cleanup)
	if err := db.ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	llmServer := newFakeLLMServer(t)
	t.Cleanup(llmServer.Close)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	registry.Register(tools.NewActivateSkillTool(skillsMgr))
	agentSvc := agent.New(store, skillsMgr, registry, t.TempDir(), 8, llm.NewClient(llmServer.URL, ""), "fake-model", "openai")
	forkMgr := fork.NewManager(agentSvc, store)
	forkTool := fork.NewForkTool(forkMgr)

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "u-3")
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
	waitForStatus(t, forkMgr, parentID, startResp.ChildSessionID, "completed", 2*time.Second)

	msgs, err := store.LoadRecentMessages(ctx, startResp.ChildSessionID, 10)
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

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	return b
}

func newFakeLLMServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		lastUser := ""
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				lastUser = req.Messages[i].Content
				break
			}
		}
		if strings.HasPrefix(lastUser, "INTERRUPT:") && len(req.Tools) > 0 {
			http.Error(w, "tools not allowed in interrupt", http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "echo:" + lastUser,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newBlockingLLMServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Simulate a long-running model call.
		time.Sleep(5 * time.Second)
		resp := map[string]any{
			"id":      "chatcmpl-block",
			"created": time.Now().Unix(),
			"model":   "fake-model",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "done",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}
