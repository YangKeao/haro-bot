//go:build integration

package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	fs := tools.NewFS([]string{allowedRoot}, nil, auditStore)

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

func TestFSExecAudits(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	allowedRoot := t.TempDir()
	scriptsDir := filepath.Join(allowedRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	scriptPath := filepath.Join(scriptsDir, "hello.sh")
	scriptContent := "#!/bin/sh\necho ok\n"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("chmod script: %v", err)
	}

	otherDir := filepath.Join(allowedRoot, "other")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	otherScriptPath := filepath.Join(otherDir, "nope.sh")
	if err := os.WriteFile(otherScriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write other script: %v", err)
	}
	if err := os.Chmod(otherScriptPath, 0o755); err != nil {
		t.Fatalf("chmod other script: %v", err)
	}

	auditStore := tools.NewAuditStore(gdb)
	fs := tools.NewFS([]string{allowedRoot}, nil, auditStore)

	out, err := fs.Exec(context.Background(), 1, 2, allowedRoot, "scripts/hello.sh", nil, 5*time.Second, 1024)
	if err != nil {
		t.Fatalf("exec allowed: %v", err)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("unexpected exec output: %q", out)
	}
	if _, err := fs.Exec(context.Background(), 1, 2, allowedRoot, "other/nope.sh", nil, 5*time.Second, 1024); err == nil {
		t.Fatalf("expected denied exec to fail")
	}

	var rows []dbmodel.ToolAudit
	if err := gdb.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 audit rows, got %d", len(rows))
	}
	if rows[0].Tool != "exec" || rows[0].Status != "ok" || !rows[0].Allowed {
		t.Fatalf("unexpected audit row: %+v", rows[0])
	}
	if rows[1].Tool != "exec" || rows[1].Status != "error" || rows[1].Reason == "" {
		t.Fatalf("unexpected denied audit row: %+v", rows[1])
	}
}
