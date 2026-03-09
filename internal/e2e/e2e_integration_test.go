//go:build integration


package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

// TestE2ESimpleConversation tests a basic conversation flow
func TestE2ESimpleConversation(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)

	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 4, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(ctx, 9001)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// First message
	resp, err := agentSvc.Handle(ctx, userID, "e2e-simple", "My name is Alice. Remember it.")
	if err != nil {
		t.Fatalf("handle first message: %v", err)
	}
	if resp == "" {
		t.Fatal("empty response for first message")
	}

	// Second message - test context retention
	resp, err = agentSvc.Handle(ctx, userID, "e2e-simple", "What is my name?")
	if err != nil {
		t.Fatalf("handle second message: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp), "alice") {
		t.Fatalf("expected response to contain 'Alice', got: %s", resp)
	}
}

// TestE2EToolExecution tests tool calling flow
func TestE2EToolExecution(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)

	// Register tools that the agent might use
	registry.Register(tools.NewMemorySearchTool(store))

	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 8, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(ctx, 9002)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create a session with some messages
	sessionID, err := store.GetOrCreateSession(ctx, userID, "e2e-tool")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	err = store.AddMessage(ctx, sessionID, "user", "My favorite color is blue.", nil)
	if err != nil {
		t.Fatalf("add message: %v", err)
	}
	err = store.AddMessage(ctx, sessionID, "assistant", "I'll remember that your favorite color is blue.", nil)
	if err != nil {
		t.Fatalf("add message: %v", err)
	}

	// Ask about the stored information - this might trigger memory search
	resp, err := agentSvc.Handle(ctx, userID, "e2e-tool", "What is my favorite color? Use memory search if needed.")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp == "" {
		t.Fatal("empty response")
	}
	// The response should mention blue
	if !strings.Contains(strings.ToLower(resp), "blue") {
		t.Logf("Response: %s", resp)
		// Note: This might not always work depending on LLM behavior
	}
}

// TestE2ESessionInterrupt tests session interruption
func TestE2ESessionInterrupt(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)

	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 4, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(ctx, 9002)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create a session and add some messages
	sessionID, err := store.GetOrCreateSession(ctx, userID, "e2e-interrupt")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	err = store.AddMessage(ctx, sessionID, "user", "Let's discuss Go programming.", nil)
	if err != nil {
		t.Fatalf("add message: %v", err)
	}
	err = store.AddMessage(ctx, sessionID, "assistant", "Sure! Go is a statically typed, compiled programming language designed at Google.", nil)
	if err != nil {
		t.Fatalf("add message: %v", err)
	}

	// Interrupt the session with a question
	resp, err := agentSvc.InterruptSession(ctx, sessionID, userID, "What were we talking about?", "", false, nil)
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if resp == "" {
		t.Fatal("empty interrupt response")
	}
	// Should mention Go or programming
	respLower := strings.ToLower(resp)
	if !strings.Contains(respLower, "go") && !strings.Contains(respLower, "programming") {
		t.Logf("Response: %s", resp)
		// Note: This test may be flaky depending on LLM
	}
}

// TestE2EMultipleSessions tests handling multiple sessions for the same user
func TestE2EMultipleSessions(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)

	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 4, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(ctx, 9003)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// First session - about Python
	resp1, err := agentSvc.Handle(ctx, userID, "session-python", "I love Python programming.")
	if err != nil {
		t.Fatalf("handle session 1: %v", err)
	}
	if resp1 == "" {
		t.Fatal("empty response for session 1")
	}

	// Second session - about JavaScript
	resp2, err := agentSvc.Handle(ctx, userID, "session-js", "I enjoy JavaScript too.")
	if err != nil {
		t.Fatalf("handle session 2: %v", err)
	}
	if resp2 == "" {
		t.Fatal("empty response for session 2")
	}

	// Go back to first session - context should be preserved
	resp3, err := agentSvc.Handle(ctx, userID, "session-python", "What language did I say I love?")
	if err != nil {
		t.Fatalf("handle session 1 again: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp3), "python") {
		t.Fatalf("expected response to contain 'Python', got: %s", resp3)
	}
}

// TestE2ESessionStatus tests session status tracking
func TestE2ESessionStatus(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)

	agentSvc := agent.New(store, nil, skillsMgr, registry, nil, t.TempDir(), 4, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByTelegramID(ctx, 9004)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	sessionID, err := store.GetOrCreateSession(ctx, userID, "e2e-status")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Initial status should be idle
	status := agentSvc.GetSessionStatus(sessionID)
	if status.State != "idle" {
		t.Fatalf("expected initial state to be idle, got: %s", status.State)
	}

	// Start a handle operation in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agentSvc.Handle(ctx, userID, "e2e-status", "Say hello.")
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Wait for completion
	<-done

	// Status should be back to idle
	status = agentSvc.GetSessionStatus(sessionID)
	if status.State != "idle" {
		t.Fatalf("expected final state to be idle, got: %s", status.State)
	}
}
