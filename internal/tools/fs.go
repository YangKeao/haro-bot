package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

var (
	errRelativeNoBase = errors.New("relative path requires base directory")
)

type FS struct {
	audit AuditLogger
}

func NewFS(audit AuditLogger) *FS {
	return &FS{
		audit: audit,
	}
}

func (f *FS) DefaultBase() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "/"
}

func (f *FS) resolvePath(baseDir, path string, allowMissing bool) (string, error) {
	if path == "" {
		return "", errors.New("path required")
	}
	var target string
	if filepath.IsAbs(path) {
		target = path
	} else {
		if baseDir == "" {
			return "", errRelativeNoBase
		}
		target = filepath.Join(baseDir, path)
	}
	abs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if allowMissing {
		return abs, nil
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func (f *FS) auditMaybe(ctx context.Context, entry AuditEntry) {
	if f == nil || f.audit == nil {
		return
	}
	_ = f.audit.Record(ctx, entry)
}

func (f *FS) auditError(ctx context.Context, sessionID, userID int64, tool, path string, err error) {
	if err == nil {
		return
	}
	f.auditMaybe(ctx, AuditEntry{
		SessionID: sessionID,
		UserID:    userID,
		Tool:      tool,
		Path:      path,
		Allowed:   true,
		Status:    "error",
		Reason:    err.Error(),
	})
}

func (f *FS) auditOK(ctx context.Context, sessionID, userID int64, tool, path string, meta map[string]any) {
	f.auditMaybe(ctx, AuditEntry{
		SessionID: sessionID,
		UserID:    userID,
		Tool:      tool,
		Path:      path,
		Allowed:   true,
		Status:    "ok",
		Metadata:  meta,
	})
}
