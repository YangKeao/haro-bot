//go:build integration

package memory_test

import (
	"context"
	"testing"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestMemoryStoreRoundTrip(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	store := memory.NewStore(gdb)
	ctx := context.Background()

	userID, err := store.GetOrCreateUserByTelegramID(ctx, 1001)
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
	msgs, _, err := store.LoadViewMessages(ctx, sessionID, 10)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
}

func TestLoadViewMessagesPreservesMetadata(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	store := memory.NewStore(gdb)
	ctx := context.Background()

	userID, err := store.GetOrCreateUserByTelegramID(ctx, 1002)
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
	msgs, _, err := store.LoadViewMessages(ctx, sessionID, 10)
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

func TestLoadViewMessagesSoftDeletesInvalidToolOutputs(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	store := memory.NewStore(gdb)
	ctx := context.Background()

	userID, err := store.GetOrCreateUserByTelegramID(ctx, 1003)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	sessionID, err := store.GetOrCreateSession(ctx, userID, "soft-delete")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	toolCalls := []llm.ToolCall{
		{ID: "call-ok", Type: "function", Function: llm.ToolCallFn{Name: "test", Arguments: `{}`}},
	}
	if err := store.AddMessage(ctx, sessionID, "assistant", "", &memory.MessageMetadata{ToolCalls: toolCalls}); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}
	if err := store.AddMessage(ctx, sessionID, "tool", "ok", &memory.MessageMetadata{ToolCallID: "call-ok"}); err != nil {
		t.Fatalf("add tool message: %v", err)
	}
	if err := store.AddMessage(ctx, sessionID, "tool", "bad-unknown", &memory.MessageMetadata{ToolCallID: "call-missing"}); err != nil {
		t.Fatalf("add invalid tool message: %v", err)
	}
	if err := store.AddMessage(ctx, sessionID, "tool", "bad-empty", &memory.MessageMetadata{}); err != nil {
		t.Fatalf("add invalid tool message: %v", err)
	}

	msgs, _, err := store.LoadViewMessages(ctx, sessionID, 10)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	for _, msg := range msgs {
		if msg.Content == "bad-unknown" || msg.Content == "bad-empty" {
			t.Fatalf("unexpected invalid tool output in results: %+v", msg)
		}
	}

	var deleted []dbmodel.Message
	if err := gdb.Where("session_id = ? AND deleted_at IS NOT NULL", sessionID).Find(&deleted).Error; err != nil {
		t.Fatalf("query deleted messages: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 soft deleted messages, got %d", len(deleted))
	}
}

func TestLoadViewMessagesUsesAnchor(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	store := memory.NewStore(gdb)
	ctx := context.Background()

	userID, err := store.GetOrCreateUserByTelegramID(ctx, 1004)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	sessionID, err := store.GetOrCreateSession(ctx, userID, "anchor")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AddMessage(ctx, sessionID, "user", "one", nil); err != nil {
		t.Fatalf("add message 1: %v", err)
	}
	if err := store.AddMessage(ctx, sessionID, "assistant", "two", nil); err != nil {
		t.Fatalf("add message 2: %v", err)
	}
	if err := store.AddMessage(ctx, sessionID, "user", "three", nil); err != nil {
		t.Fatalf("add message 3: %v", err)
	}

	var records []dbmodel.Message
	if err := gdb.Where("session_id = ?", sessionID).Order("id asc").Find(&records).Error; err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(records))
	}
	anchorID, err := store.AppendAnchor(ctx, sessionID, memory.Anchor{
		EntryID: records[1].ID,
		Summary: "state after two",
	})
	if err != nil {
		t.Fatalf("append anchor: %v", err)
	}
	if anchorID == 0 {
		t.Fatalf("expected anchor id to be set")
	}

	msgs, anchor, err := store.LoadViewMessages(ctx, sessionID, 10)
	if err != nil {
		t.Fatalf("load view: %v", err)
	}
	if anchor == nil || anchor.ID != anchorID {
		t.Fatalf("unexpected anchor: %+v", anchor)
	}
	if len(msgs) != 1 || msgs[0].Content != "three" {
		t.Fatalf("unexpected view messages: %+v", msgs)
	}
}
