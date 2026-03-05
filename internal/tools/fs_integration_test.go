//go:build integration

package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

func TestFSReadAudits(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	allowedRoot := t.TempDir()
	deniedRoot := t.TempDir()
	allowedFile := filepath.Join(allowedRoot, "ok.txt")
	if err := os.WriteFile(allowedFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write allowed file: %v", err)
	}
	deniedFile := filepath.Join(deniedRoot, "nope.txt")
	if err := os.WriteFile(deniedFile, []byte("nope"), 0o644); err != nil {
		t.Fatalf("write denied file: %v", err)
	}

	auditStore := tools.NewAuditStore(gdb)
	fs := tools.NewFS([]string{allowedRoot}, false, nil, auditStore)

	if _, err := fs.Read(context.Background(), 1, 2, allowedRoot, "ok.txt", 1024); err != nil {
		t.Fatalf("read allowed: %v", err)
	}
	if _, err := fs.Read(context.Background(), 1, 2, allowedRoot, deniedFile, 1024); err == nil {
		t.Fatalf("expected denied read to fail")
	}

	var rows []dbmodel.ToolAudit
	if err := gdb.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 audit rows, got %d", len(rows))
	}
	if !rows[0].Allowed || rows[0].Tool != "read" {
		t.Fatalf("unexpected audit row: %+v", rows[0])
	}
	if rows[1].Allowed || rows[1].Tool != "read" {
		t.Fatalf("unexpected denied audit row: %+v", rows[1])
	}
}
