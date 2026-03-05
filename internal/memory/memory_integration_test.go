//go:build integration

package memory_test

import (
	"context"
	"testing"

	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestMemoryStoreRoundTrip(t *testing.T) {
	gdb, cleanup := testutil.NewTestDB(t)
	t.Cleanup(cleanup)
	if err := db.ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
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
	if err := store.AddMessage(ctx, sessionID, "user", "hello", map[string]any{"k": "v"}); err != nil {
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
