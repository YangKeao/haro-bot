//go:build integration

package tools_test

import (
	"context"
	"testing"

	"github.com/YangKeao/haro-bot/internal/db"
	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

func TestAuditStoreInsert(t *testing.T) {
	gdb, cleanup := testutil.NewTestDB(t)
	t.Cleanup(cleanup)
	if err := db.ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	store := tools.NewAuditStore(gdb)
	err := store.Record(context.Background(), tools.AuditEntry{
		SessionID: 1,
		UserID:    2,
		Tool:      "read",
		Path:      "test.txt",
		Allowed:   true,
		Status:    "ok",
	})
	if err != nil {
		t.Fatalf("record audit: %v", err)
	}
	var rows []dbmodel.ToolAudit
	if err := gdb.Find(&rows).Error; err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if len(rows) != 1 || rows[0].Tool != "read" {
		t.Fatalf("unexpected audit rows: %+v", rows)
	}
}
