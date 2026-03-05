package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	errPathEscape = errors.New("path escapes base directory")
	errSymlink    = errors.New("path contains symlink")
)

func safeJoin(baseDir, rel string) (string, error) {
	rel = filepath.Clean(rel)
	if rel == "." {
		return baseDir, nil
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errPathEscape
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(baseAbs, rel)
	resolvedBase, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return "", err
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	if resolvedTarget != resolvedBase && !strings.HasPrefix(resolvedTarget, resolvedBase+string(filepath.Separator)) {
		return "", errPathEscape
	}
	return resolvedTarget, nil
}

func ensureNoSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errSymlink
	}
	return nil
}

func safeJoinAllowMissing(baseDir, rel string) (string, error) {
	rel = filepath.Clean(rel)
	if rel == "." {
		return baseDir, nil
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errPathEscape
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	resolvedBase, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return "", err
	}
	target := filepath.Join(resolvedBase, rel)
	relCheck, err := filepath.Rel(resolvedBase, target)
	if err != nil {
		return "", err
	}
	if relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", errPathEscape
	}
	cur := resolvedBase
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		info, err := os.Lstat(next)
		if err != nil {
			if os.IsNotExist(err) {
				break
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errSymlink
		}
		cur = next
	}
	return target, nil
}

func containsPathSegment(path, segment string) bool {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for _, p := range parts {
		if p == segment {
			return true
		}
	}
	return false
}
