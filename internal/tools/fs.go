package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	errPathDenied     = errors.New("path not allowed")
	errRelativeNoBase = errors.New("relative path requires base directory")
	errSymlink        = errors.New("path contains symlink")
)

type FS struct {
	allowedRoots    []string
	allowExec       bool
	allowedExecDirs []string
	audit           AuditLogger
}

func NewFS(allowedRoots []string, allowExec bool, allowedExecDirs []string, audit AuditLogger) *FS {
	return &FS{
		allowedRoots:    canonicalizeRoots(allowedRoots),
		allowExec:       allowExec,
		allowedExecDirs: allowedExecDirs,
		audit:           audit,
	}
}

func (f *FS) ExecEnabled() bool {
	if f == nil {
		return false
	}
	return f.allowExec
}

func (f *FS) DefaultBase() string {
	if f == nil || len(f.allowedRoots) == 0 {
		return ""
	}
	return f.allowedRoots[0]
}

func (f *FS) Read(ctx context.Context, sessionID, userID int64, baseDir, path string, maxBytes int64) (string, error) {
	abs, allowed, err := f.resolvePath(baseDir, path, false)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "read", path, allowed, err)
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "read", abs, true, err)
		return "", err
	}
	if info.IsDir() {
		err = errors.New("path is a directory")
		f.auditError(ctx, sessionID, userID, "read", abs, true, err)
		return "", err
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		err = errors.New("file too large")
		f.auditError(ctx, sessionID, userID, "read", abs, true, err)
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "read", abs, true, err)
		return "", err
	}
	f.auditOK(ctx, sessionID, userID, "read", abs, nil)
	return string(data), nil
}

func (f *FS) Write(ctx context.Context, sessionID, userID int64, baseDir, path, content string) error {
	abs, allowed, err := f.resolvePath(baseDir, path, true)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "write", path, allowed, err)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		f.auditError(ctx, sessionID, userID, "write", abs, true, err)
		return err
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		f.auditError(ctx, sessionID, userID, "write", abs, true, err)
		return err
	}
	f.auditOK(ctx, sessionID, userID, "write", abs, nil)
	return nil
}

func (f *FS) Edit(ctx context.Context, sessionID, userID int64, baseDir, path, oldText, newText string, replaceAll bool) (int, error) {
	abs, allowed, err := f.resolvePath(baseDir, path, false)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "edit", path, allowed, err)
		return 0, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "edit", abs, true, err)
		return 0, err
	}
	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		err = errors.New("pattern not found")
		f.auditError(ctx, sessionID, userID, "edit", abs, true, err)
		return 0, err
	}
	if replaceAll {
		content = strings.ReplaceAll(content, oldText, newText)
	} else {
		content = strings.Replace(content, oldText, newText, 1)
		count = 1
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		f.auditError(ctx, sessionID, userID, "edit", abs, true, err)
		return 0, err
	}
	f.auditOK(ctx, sessionID, userID, "edit", abs, map[string]any{"replacements": count})
	return count, nil
}

func (f *FS) Search(ctx context.Context, sessionID, userID int64, baseDir, pattern string, maxResults int) (string, error) {
	if baseDir == "" {
		err := errRelativeNoBase
		f.auditError(ctx, sessionID, userID, "search", baseDir, false, err)
		return "", err
	}
	root, allowed, err := f.resolvePath("", baseDir, false)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "search", baseDir, allowed, err)
		return "", err
	}
	if maxResults <= 0 {
		maxResults = 50
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "search", root, true, err)
		return "", err
	}
	results := make([]SearchMatch, 0, maxResults)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				results = append(results, SearchMatch{Path: rel, Line: lineNum, Text: line})
				if len(results) >= maxResults {
					_ = f.Close()
					return errSearchLimit
				}
			}
		}
		_ = f.Close()
		return nil
	})
	if walkErr != nil && walkErr != errSearchLimit {
		f.auditError(ctx, sessionID, userID, "search", root, true, walkErr)
		return "", walkErr
	}
	payload, err := json.Marshal(results)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "search", root, true, err)
		return "", err
	}
	f.auditOK(ctx, sessionID, userID, "search", root, map[string]any{"count": len(results)})
	return string(payload), nil
}

func (f *FS) Exec(ctx context.Context, sessionID, userID int64, baseDir, path string, args []string, timeout time.Duration, maxOutputBytes int) (string, error) {
	if !f.allowExec {
		err := errors.New("exec disabled")
		f.auditError(ctx, sessionID, userID, "exec", path, false, err)
		return "", err
	}
	abs, allowed, err := f.resolvePath(baseDir, path, false)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "exec", path, allowed, err)
		return "", err
	}
	if !f.execPathAllowed(baseDir, abs) {
		err := errors.New("exec path not allowed")
		f.auditError(ctx, sessionID, userID, "exec", abs, true, err)
		return "", err
	}
	output, err := runScript(ctx, abs, baseDir, args, timeout, maxOutputBytes)
	if err != nil {
		f.auditError(ctx, sessionID, userID, "exec", abs, true, err)
	} else {
		f.auditOK(ctx, sessionID, userID, "exec", abs, nil)
	}
	return output, err
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

func (f *FS) execPathAllowed(baseDir, abs string) bool {
	if baseDir == "" {
		return false
	}
	rel, err := filepath.Rel(baseDir, abs)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	if len(f.allowedExecDirs) == 0 {
		return strings.HasPrefix(rel, "scripts"+string(filepath.Separator)) || rel == "scripts"
	}
	for _, dir := range f.allowedExecDirs {
		dir = filepath.Clean(dir)
		if rel == dir || strings.HasPrefix(rel, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (f *FS) auditMaybe(ctx context.Context, entry AuditEntry) {
	if f.audit == nil {
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

var errSearchLimit = errors.New("limit reached")

// SearchMatch mirrors the match results for the search tool.
// Declared here to keep tools independent from skills.
type SearchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}
