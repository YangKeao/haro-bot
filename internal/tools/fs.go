package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	errPathDenied     = errors.New("path not allowed")
	errRelativeNoBase = errors.New("relative path requires base directory")
	errSymlink        = errors.New("path contains symlink")
)

type FS struct {
	unrestricted bool
	allowedRoots []string
	audit        AuditLogger
	approver     Approver
}

func NewFS(allowedRoots []string, audit AuditLogger, unrestricted bool) *FS {
	return &FS{
		unrestricted: unrestricted,
		allowedRoots: canonicalizeRoots(allowedRoots),
		audit:        audit,
	}
}

func (f *FS) SetApprover(approver Approver) {
	if f == nil || f.unrestricted {
		return
	}
	f.approver = approver
}

func (f *FS) DefaultBase() string {
	if f == nil {
		return ""
	}
	if f.unrestricted {
		if cwd, err := os.Getwd(); err == nil {
			return cwd
		}
		return "/"
	}
	if len(f.allowedRoots) == 0 {
		return ""
	}
	return f.allowedRoots[0]
}

func (f *FS) resolvePath(baseDir, path string, allowMissing bool) (string, bool, error) {
	if path == "" {
		return "", false, errors.New("path required")
	}
	var target string
	if filepath.IsAbs(path) {
		target = path
	} else {
		if baseDir == "" {
			return "", false, errRelativeNoBase
		}
		target = filepath.Join(baseDir, path)
	}
	abs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", false, err
	}

	// Unrestricted mode: skip path restrictions and symlink checks
	if f.unrestricted {
		return abs, true, nil
	}

	resolvedTarget, err := resolvePathWithSymlinks(abs, allowMissing)
	if err != nil {
		return "", false, err
	}
	for _, root := range f.allowedRoots {
		if root == "" {
			continue
		}
		resolvedRoot, err := resolvePathWithSymlinks(root, true)
		if err != nil {
			continue
		}
		if isWithin(resolvedRoot, resolvedTarget) {
			return resolvedTarget, true, nil
		}
	}
	return resolvedTarget, false, errPathDenied
}

func (f *FS) resolvePathWithApproval(ctx context.Context, tc ToolContext, tool, baseDir, path string, allowMissing bool) (string, bool, error) {
	abs, allowed, err := f.resolvePath(baseDir, path, allowMissing)
	if err == nil {
		return abs, allowed, nil
	}
	if f.unrestricted {
		return abs, allowed, err
	}
	if !errors.Is(err, errPathDenied) || f.approver == nil {
		return abs, allowed, err
	}
	if err := f.requestApproval(ctx, tc, tool, abs, err.Error()); err != nil {
		return abs, false, err
	}
	return abs, true, nil
}

func (f *FS) requestApproval(ctx context.Context, tc ToolContext, tool, path, reason string) error {
	if f == nil || f.unrestricted || f.approver == nil {
		return nil
	}
	decision, reqErr := f.approver.RequestApproval(ctx, ApprovalRequest{
		SessionID: tc.SessionID,
		UserID:    tc.UserID,
		Tool:      tool,
		Path:      path,
		Reason:    reason,
	})
	if reqErr != nil {
		return reqErr
	}
	switch decision {
	case ApprovalAllow:
		return nil
	case ApprovalStop:
		return ErrApprovalStopped
	case ApprovalDeny:
		fallthrough
	default:
		return ErrApprovalDenied
	}
}

func (f *FS) auditMaybe(ctx context.Context, entry AuditEntry) {
	if f == nil || f.audit == nil {
		return
	}
	_ = f.audit.Record(ctx, entry)
}

func (f *FS) auditError(ctx context.Context, sessionID, userID int64, tool, path string, allowed bool, err error) {
	if err == nil {
		return
	}
	f.auditMaybe(ctx, AuditEntry{
		SessionID: sessionID,
		UserID:    userID,
		Tool:      tool,
		Path:      path,
		Allowed:   allowed,
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

func canonicalizeRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		out = append(out, abs)
	}
	return out
}

func resolvePathWithSymlinks(path string, allowMissing bool) (string, error) {
	if !allowMissing {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		return resolved, nil
	}
	abs := path
	cur := abs
	for {
		info, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				parent := filepath.Dir(cur)
				if parent == cur {
					break
				}
				cur = parent
				continue
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errSymlink
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return abs, nil
}

func isWithin(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if root == target {
		return true
	}
	if strings.HasPrefix(target, root+string(filepath.Separator)) {
		return true
	}
	return false
}
