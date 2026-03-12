package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	listDirDefaultOffset = 1
	listDirDefaultLimit  = 25
	listDirDefaultDepth  = 2
	listDirMaxEntryLen   = 500
	listDirIndentSpaces  = 2
)

type ListDirTool struct {
	fs *FS
}

type listDirArgs struct {
	DirPath string `json:"dir_path"`
	Offset  *int   `json:"offset"`
	Limit   *int   `json:"limit"`
	Depth   *int   `json:"depth"`
}

func NewListDirTool(fs *FS) *ListDirTool {
	return &ListDirTool{fs: fs}
}

func (t *ListDirTool) Name() string { return "list_dir" }

func (t *ListDirTool) Description() string {
	return "Lists entries in a local directory with 1-indexed entry numbers and simple type labels."
}

func (t *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the directory to list.",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "The entry number to start listing from. Must be 1 or greater.",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "The maximum number of entries to return.",
			},
			"depth": map[string]any{
				"type":        "number",
				"description": "The maximum directory depth to traverse. Must be 1 or greater.",
			},
		},
		"required":             []string{"dir_path"},
		"additionalProperties": false,
	}
}

func (t *ListDirTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.fs == nil {
		return "", errors.New("list_dir not configured")
	}
	var a listDirArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if a.DirPath == "" {
		return "", errors.New("dir_path required")
	}
	if !filepath.IsAbs(a.DirPath) {
		return "", errors.New("dir_path must be an absolute path")
	}

	offset := listDirDefaultOffset
	if a.Offset != nil {
		if *a.Offset <= 0 {
			return "", errors.New("offset must be a 1-indexed entry number")
		}
		offset = *a.Offset
	}
	limit := listDirDefaultLimit
	if a.Limit != nil {
		if *a.Limit <= 0 {
			return "", errors.New("limit must be greater than zero")
		}
		limit = *a.Limit
	}
	depth := listDirDefaultDepth
	if a.Depth != nil {
		if *a.Depth <= 0 {
			return "", errors.New("depth must be greater than zero")
		}
		depth = *a.Depth
	}

	abs, err := t.fs.resolvePath("", a.DirPath, false)
	if err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "list_dir", a.DirPath, err)
		return "", err
	}

	info, err := os.Stat(abs)
	if err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "list_dir", abs, err)
		return "", err
	}
	if !info.IsDir() {
		err := errors.New("dir_path is not a directory")
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "list_dir", abs, err)
		return "", err
	}

	entries, err := listDirSlice(abs, offset, limit, depth)
	if err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "list_dir", abs, err)
		return "", err
	}
	out := make([]string, 0, len(entries)+1)
	out = append(out, fmt.Sprintf("Absolute path: %s", abs))
	out = append(out, entries...)
	t.fs.auditOK(ctx, tc.SessionID, tc.UserID, "list_dir", abs, map[string]any{"entries": len(entries)})
	return strings.Join(out, "\n"), nil
}

func listDirSlice(path string, offset, limit, depth int) ([]string, error) {
	var entries []dirEntry
	if err := collectDirEntries(path, "", depth, 0, &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	start := offset - 1
	if start >= len(entries) {
		return nil, errors.New("offset exceeds directory entry count")
	}
	remaining := len(entries) - start
	cappedLimit := limit
	if cappedLimit > remaining {
		cappedLimit = remaining
	}
	end := start + cappedLimit
	selected := entries[start:end]
	formatted := make([]string, 0, len(selected)+1)
	for _, entry := range selected {
		formatted = append(formatted, formatDirEntryLine(entry))
	}
	if end < len(entries) {
		formatted = append(formatted, fmt.Sprintf("More than %d entries found", cappedLimit))
	}
	return formatted, nil
}

type dirEntry struct {
	name        string
	displayName string
	depth       int
	kind        dirEntryKind
}

type dirEntryKind int

const (
	dirEntryDir dirEntryKind = iota
	dirEntryFile
	dirEntrySymlink
	dirEntryOther
)

func collectDirEntries(dirPath, relativePrefix string, depth, currentDepth int, entries *[]dirEntry) error {
	queue := []dirQueueItem{{
		dirPath:        dirPath,
		relativePrefix: relativePrefix,
		remainingDepth: depth,
		currentDepth:   currentDepth,
	}}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		readDirEntries, err := os.ReadDir(item.dirPath)
		if err != nil {
			return fmt.Errorf("failed to read directory: %w", err)
		}

		type stagedEntry struct {
			path    string
			relPath string
			kind    dirEntryKind
			entry   dirEntry
		}
		var staged []stagedEntry
		for _, entry := range readDirEntries {
			path := filepath.Join(item.dirPath, entry.Name())
			info, err := os.Lstat(path)
			if err != nil {
				return fmt.Errorf("failed to inspect entry: %w", err)
			}
			relPath := entry.Name()
			if item.relativePrefix != "" {
				relPath = filepath.Join(item.relativePrefix, relPath)
			}
			displayName := formatDirEntryComponent(entry.Name())
			sortKey := formatDirEntryName(relPath)
			kind := kindFromInfo(info)
			staged = append(staged, stagedEntry{
				path:    path,
				relPath: relPath,
				kind:    kind,
				entry: dirEntry{
					name:        sortKey,
					displayName: displayName,
					depth:       item.currentDepth,
					kind:        kind,
				},
			})
		}

		sort.Slice(staged, func(i, j int) bool {
			return staged[i].entry.name < staged[j].entry.name
		})

		for _, entry := range staged {
			if entry.kind == dirEntryDir && item.remainingDepth > 1 {
				queue = append(queue, dirQueueItem{
					dirPath:        entry.path,
					relativePrefix: entry.relPath,
					remainingDepth: item.remainingDepth - 1,
					currentDepth:   item.currentDepth + 1,
				})
			}
			*entries = append(*entries, entry.entry)
		}
	}
	return nil
}

type dirQueueItem struct {
	dirPath        string
	relativePrefix string
	remainingDepth int
	currentDepth   int
}

func formatDirEntryName(path string) string {
	normalized := filepath.ToSlash(path)
	if len(normalized) <= listDirMaxEntryLen {
		return normalized
	}
	return truncateUTF8(normalized, listDirMaxEntryLen)
}

func formatDirEntryComponent(name string) string {
	if len(name) <= listDirMaxEntryLen {
		return name
	}
	return truncateUTF8(name, listDirMaxEntryLen)
}

func formatDirEntryLine(entry dirEntry) string {
	indent := strings.Repeat(" ", entry.depth*listDirIndentSpaces)
	name := entry.displayName
	switch entry.kind {
	case dirEntryDir:
		name += "/"
	case dirEntrySymlink:
		name += "@"
	case dirEntryOther:
		name += "?"
	}
	return indent + name
}

func kindFromInfo(info os.FileInfo) dirEntryKind {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return dirEntrySymlink
	case info.IsDir():
		return dirEntryDir
	case mode.IsRegular():
		return dirEntryFile
	default:
		return dirEntryOther
	}
}
