//go:build integration

package memory_test

import (
	"context"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestMemoryStoreRoundTrip(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	store := memory.NewStore(gdb)
	ctx := context.Background()

	userID, err := store.GetOrCreateUserByExternalID(ctx, "user-1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	sessionID, err := store.GetOrCreateSession(ctx, userID, "test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AddMessage(ctx, sessionID, "user", "hello", &memory.MessageMetadata{Status: "ok"}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	msgs, err := store.LoadRecentMessages(ctx, sessionID, 10)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
}

func TestLoadRecentMessagesPreservesMetadata(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	store := memory.NewStore(gdb)
	ctx := context.Background()

	userID, err := store.GetOrCreateUserByExternalID(ctx, "user-meta")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	sessionID, err := store.GetOrCreateSession(ctx, userID, "meta-test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	toolCalls := []llm.ToolCall{
		{ID: "call-1", Type: "function", Function: llm.ToolCallFn{Name: "test", Arguments: `{"x":1}`}},
	}
	meta := &memory.MessageMetadata{
		ToolCallID: "call-1",
		ToolCalls:  toolCalls,
	}
	if err := store.AddMessage(ctx, sessionID, "tool", "ok", meta); err != nil {
		t.Fatalf("add message: %v", err)
	}
	msgs, err := store.LoadRecentMessages(ctx, sessionID, 10)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Metadata == nil {
		t.Fatalf("expected metadata to be preserved")
	}
	if msgs[0].Metadata.ToolCallID != "call-1" {
		t.Fatalf("expected tool_call_id to roundtrip, got %q", msgs[0].Metadata.ToolCallID)
	}
}
