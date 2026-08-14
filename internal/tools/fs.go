package tools

import (
	"errors"
	"os"
	"path/filepath"
)

var (
	errRelativeNoBase = errors.New("relative path requires base directory")
)

type FS struct{}

func NewFS() *FS {
	return &FS{}
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
