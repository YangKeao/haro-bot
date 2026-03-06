package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type ReadTool struct {
	fs       *FS
	maxBytes int64
}

type readArgs struct {
	Path string `json:"path"`
}

func NewReadTool(fs *FS, maxBytes int64) *ReadTool {
	return &ReadTool{fs: fs, maxBytes: maxBytes}
}

func (t *ReadTool) Name() string        { return "read" }
func (t *ReadTool) Description() string { return "Read a file from allowed paths." }
func (t *ReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path under the allowed base directory",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var a readArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	return t.fs.Read(ctx, tc.SessionID, tc.UserID, tc.BaseDir, a.Path, t.maxBytes)
}

type WriteTool struct {
	fs *FS
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func NewWriteTool(fs *FS) *WriteTool { return &WriteTool{fs: fs} }

func (t *WriteTool) Name() string        { return "write" }
func (t *WriteTool) Description() string { return "Write a file to allowed paths." }
func (t *WriteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path under the allowed base directory",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "File contents",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var a writeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if err := t.fs.Write(ctx, tc.SessionID, tc.UserID, tc.BaseDir, a.Path, a.Content); err != nil {
		return "", err
	}
	return "ok", nil
}

type SearchTool struct {
	fs *FS
}

type searchArgs struct {
	Pattern    string `json:"pattern"`
	MaxResults int    `json:"max_results"`
}

func NewSearchTool(fs *FS) *SearchTool { return &SearchTool{fs: fs} }

func (t *SearchTool) Name() string { return "search" }
func (t *SearchTool) Description() string {
	return "Search files under the allowed base directory using a regular expression."
}
func (t *SearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of matches to return",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *SearchTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var a searchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	return t.fs.Search(ctx, tc.SessionID, tc.UserID, tc.BaseDir, a.Pattern, a.MaxResults)
}

type EditTool struct {
	fs *FS
}

type editArgs struct {
	Path       string `json:"path"`
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replace_all"`
}

func NewEditTool(fs *FS) *EditTool { return &EditTool{fs: fs} }

func (t *EditTool) Name() string        { return "edit" }
func (t *EditTool) Description() string { return "Edit a file by replacing text within allowed paths." }
func (t *EditTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path under the allowed base directory",
			},
			"old": map[string]any{
				"type":        "string",
				"description": "Text to replace",
			},
			"new": map[string]any{
				"type":        "string",
				"description": "Replacement text",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences if true",
			},
		},
		"required": []string{"path", "old", "new"},
	}
}

func (t *EditTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var a editArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	count, err := t.fs.Edit(ctx, tc.SessionID, tc.UserID, tc.BaseDir, a.Path, a.Old, a.New, a.ReplaceAll)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("replaced %d occurrence(s)", count), nil
}

type ExecTool struct {
	fs             *FS
	maxOutputBytes int
}

type execArgs struct {
	Path           string   `json:"path"`
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

func NewExecTool(fs *FS, maxOutputBytes int) *ExecTool {
	return &ExecTool{fs: fs, maxOutputBytes: maxOutputBytes}
}

func (t *ExecTool) Name() string        { return "exec" }
func (t *ExecTool) Description() string { return "Execute an allowed script within allowed paths." }
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path under the allowed base directory",
			},
			"args": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var a execArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	timeout := 30 * time.Second
	if a.TimeoutSeconds > 0 {
		timeout = time.Duration(a.TimeoutSeconds) * time.Second
	}
	return t.fs.Exec(ctx, tc.SessionID, tc.UserID, tc.BaseDir, a.Path, a.Args, timeout, t.maxOutputBytes)
}
