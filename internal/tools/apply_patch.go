package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ApplyPatchTool struct {
	fs *FS
}

type applyPatchArgs struct {
	Patch   string `json:"patch"`
	Workdir string `json:"workdir"`
}

func NewApplyPatchTool(fs *FS) *ApplyPatchTool {
	return &ApplyPatchTool{fs: fs}
}

func (t *ApplyPatchTool) Name() string { return "apply_patch" }

func (t *ApplyPatchTool) Description() string {
	return `Use the apply_patch tool to edit files. This tool supports creating, updating, and deleting files using a simple patch format.

Patch format:
*** Begin Patch
[ one or more file operations ]
*** End Patch

Each operation starts with one of three headers:
*** Add File: <path> - create a new file. Following lines starting with + are the file contents.
*** Delete File: <path> - remove an existing file.
*** Update File: <path> - patch an existing file (optionally with a rename).

For Update operations, you can optionally add *** Move to: <new path> to rename the file.
Then use @@ to mark hunks, and prefix lines with:
  - (space) for context lines
  - for lines to remove
  + for lines to add

Example:
*** Begin Patch
*** Add File: hello.txt
+Hello, world!
*** Update File: src/app.py
@@ def greet():
-print("Hi")
+print("Hello, world!")
*** Delete File: obsolete.txt
*** End Patch`
}

func (t *ApplyPatchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patch": map[string]any{
				"type":        "string",
				"description": "The patch content to apply",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Working directory for resolving relative paths (defaults to current directory)",
			},
		},
		"required":             []string{"patch"},
		"additionalProperties": false,
	}
}

func (t *ApplyPatchTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.fs == nil {
		return "", errors.New("apply_patch not configured")
	}

	var a applyPatchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}

	if a.Patch == "" {
		return "", errors.New("patch content required")
	}

	// Determine working directory
	workdir := tc.BaseDir
	if a.Workdir != "" {
		workdir = a.Workdir
	}
	if workdir == "" {
		workdir = t.fs.DefaultBase()
	}

	// Parse the patch
	operations, err := parsePatch(a.Patch)
	if err != nil {
		return "", fmt.Errorf("failed to parse patch: %w", err)
	}

	if len(operations) == 0 {
		return "", errors.New("no operations found in patch")
	}

	// Apply each operation
	var results []string
	for _, op := range operations {
		result, err := t.applyOperation(ctx, tc, workdir, op)
		if err != nil {
			return "", fmt.Errorf("failed to apply operation on %s: %w", op.Path, err)
		}
		results = append(results, result)
	}

	return strings.Join(results, "\n"), nil
}

type PatchOperation struct {
	Type   string   // "add", "update", "delete"
	Path   string   // relative or absolute path
	MoveTo string   // new path for rename (update only)
	Lines  []string // file content (add) or diff lines (update)
}

func parsePatch(patch string) ([]PatchOperation, error) {
	lines := strings.Split(patch, "\n")
	var operations []PatchOperation

	i := 0
	// Find *** Begin Patch
	for i < len(lines) && !strings.HasPrefix(lines[i], "*** Begin Patch") {
		i++
	}
	if i >= len(lines) {
		return nil, errors.New("patch must start with *** Begin Patch")
	}
	i++ // skip *** Begin Patch

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// End of patch
		if strings.HasPrefix(line, "*** End Patch") {
			break
		}

		// Skip empty lines
		if line == "" {
			i++
			continue
		}

		// Parse operation header
		if strings.HasPrefix(line, "*** Add File: ") {
			path := strings.TrimPrefix(line, "*** Add File: ")
			op := PatchOperation{Type: "add", Path: path}
			i++
			// Collect lines starting with +
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "***") {
				contentLine := lines[i]
				if strings.HasPrefix(contentLine, "+") {
					op.Lines = append(op.Lines, strings.TrimPrefix(contentLine, "+"))
				}
				i++
			}
			operations = append(operations, op)

		} else if strings.HasPrefix(line, "*** Delete File: ") {
			path := strings.TrimPrefix(line, "*** Delete File: ")
			operations = append(operations, PatchOperation{Type: "delete", Path: path})
			i++

		} else if strings.HasPrefix(line, "*** Update File: ") {
			path := strings.TrimPrefix(line, "*** Update File: ")
			op := PatchOperation{Type: "update", Path: path}
			i++

			// Check for Move to
			if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "*** Move to: ") {
				op.MoveTo = strings.TrimPrefix(strings.TrimSpace(lines[i]), "*** Move to: ")
				i++
			}

			// Collect diff lines
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "***") {
				contentLine := lines[i]
				trimmed := strings.TrimSpace(contentLine)
				// Skip empty lines and keep context/add/remove lines
				if trimmed != "" || contentLine != "" {
					op.Lines = append(op.Lines, contentLine)
				}
				i++
			}
			operations = append(operations, op)

		} else {
			i++
		}
	}

	return operations, nil
}

func (t *ApplyPatchTool) applyOperation(ctx context.Context, tc ToolContext, workdir string, op PatchOperation) (string, error) {
	// Resolve path

	// Check if path is allowed
	allowedPath, err := t.fs.resolvePath(workdir, op.Path, true)
	if err != nil {
		return "", err
	}

	switch op.Type {
	case "add":
		return t.addFile(ctx, tc, allowedPath, op)
	case "delete":
		return t.deleteFile(ctx, tc, allowedPath, op)
	case "update":
		return t.updateFile(ctx, tc, allowedPath, op)
	default:
		return "", fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

func (t *ApplyPatchTool) addFile(ctx context.Context, tc ToolContext, path string, op PatchOperation) (string, error) {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("file already exists: %s", path)
	}

	// Create parent directories if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Write file content
	content := strings.Join(op.Lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "apply_patch", path, err)
		return "", fmt.Errorf("failed to create file: %w", err)
	}

	t.fs.auditOK(ctx, tc.SessionID, tc.UserID, "apply_patch", path, map[string]any{
		"operation": "add",
		"lines":     len(op.Lines),
	})
	return fmt.Sprintf("Created file: %s (%d lines)", path, len(op.Lines)), nil
}

func (t *ApplyPatchTool) deleteFile(ctx context.Context, tc ToolContext, path string, op PatchOperation) (string, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", path)
	}

	// Delete file
	if err := os.Remove(path); err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "apply_patch", path, err)
		return "", fmt.Errorf("failed to delete file: %w", err)
	}

	t.fs.auditOK(ctx, tc.SessionID, tc.UserID, "apply_patch", path, map[string]any{
		"operation": "delete",
	})
	return fmt.Sprintf("Deleted file: %s", path), nil
}

func (t *ApplyPatchTool) updateFile(ctx context.Context, tc ToolContext, path string, op PatchOperation) (string, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", path)
	}

	// Read current file content
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Apply diff
	newContent, err := t.applyDiff(string(content), op.Lines)
	if err != nil {
		return "", fmt.Errorf("failed to apply diff: %w", err)
	}

	// Handle rename if specified
	targetPath := path
	if op.MoveTo != "" {
		if filepath.IsAbs(op.MoveTo) {
			targetPath = op.MoveTo
		} else {
			targetPath = filepath.Join(filepath.Dir(path), op.MoveTo)
		}
		// Create parent directories if needed
		dir := filepath.Dir(targetPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create parent directories: %w", err)
		}
	}

	// Write updated content
	if err := os.WriteFile(targetPath, []byte(newContent), 0644); err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "apply_patch", path, err)
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	// Delete old file if renamed
	if op.MoveTo != "" && targetPath != path {
		if err := os.Remove(path); err != nil {
			return "", fmt.Errorf("failed to remove old file after rename: %w", err)
		}
	}

	t.fs.auditOK(ctx, tc.SessionID, tc.UserID, "apply_patch", path, map[string]any{
		"operation": "update",
		"renamed":   op.MoveTo != "",
		"new_path":  targetPath,
	})

	if op.MoveTo != "" {
		return fmt.Sprintf("Updated and renamed file: %s -> %s", path, targetPath), nil
	}
	return fmt.Sprintf("Updated file: %s", path), nil
}

func (t *ApplyPatchTool) applyDiff(original string, diffLines []string) (string, error) {
	originalLines := strings.Split(original, "\n")
	var result []string
	origIdx := 0

	i := 0
	for i < len(diffLines) {
		line := diffLines[i]

		// Skip @@ markers and empty lines
		if strings.HasPrefix(strings.TrimSpace(line), "@@") || strings.TrimSpace(line) == "" {
			i++
			continue
		}

		if len(line) == 0 {
			i++
			continue
		}

		prefix := line[0]
		content := ""
		if len(line) > 1 {
			content = line[1:]
		}

		switch prefix {
		case ' ':
			// Context line - verify it matches
			if origIdx < len(originalLines) {
				if originalLines[origIdx] != content {
					// Try to find matching line
					found := false
					for j := origIdx; j < len(originalLines) && j < origIdx+5; j++ {
						if originalLines[j] == content {
							// Add skipped lines
							for k := origIdx; k < j; k++ {
								result = append(result, originalLines[k])
							}
							origIdx = j
							found = true
							break
						}
					}
					if !found {
						return "", fmt.Errorf("context mismatch at line %d: expected %q, got %q", origIdx+1, content, originalLines[origIdx])
					}
				}
				result = append(result, content)
				origIdx++
			}
			i++

		case '-':
			// Remove line - verify it matches
			if origIdx < len(originalLines) {
				if originalLines[origIdx] != content {
					return "", fmt.Errorf("remove mismatch at line %d: expected %q, got %q", origIdx+1, content, originalLines[origIdx])
				}
				origIdx++
			}
			i++

		case '+':
			// Add line
			result = append(result, content)
			i++

		default:
			// Unknown prefix, treat as context
			if origIdx < len(originalLines) {
				result = append(result, originalLines[origIdx])
				origIdx++
			}
			i++
		}
	}

	// Add remaining original lines
	for origIdx < len(originalLines) {
		result = append(result, originalLines[origIdx])
		origIdx++
	}

	return strings.Join(result, "\n"), nil
}

// Helper function to read file lines
func readFileLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
