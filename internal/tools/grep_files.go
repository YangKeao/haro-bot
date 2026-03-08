package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	grepDefaultLimit = 100
	grepMaxLimit     = 2000
	grepTimeout      = 30 * time.Second
)

type GrepFilesTool struct {
	fs *FS
}

type grepFilesArgs struct {
	Pattern string  `json:"pattern"`
	Include *string `json:"include"`
	Path    *string `json:"path"`
	Limit   *int    `json:"limit"`
}

func NewGrepFilesTool(fs *FS) *GrepFilesTool {
	return &GrepFilesTool{fs: fs}
}

func (t *GrepFilesTool) Name() string { return "grep_files" }

func (t *GrepFilesTool) Description() string {
	return "Finds files whose contents match the pattern and lists them by modification time."
}

func (t *GrepFilesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for.",
			},
			"include": map[string]any{
				"type":        "string",
				"description": "Optional glob that limits which files are searched (e.g. \"*.rs\" or \"*.{ts,tsx}\").",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory or file path to search. Defaults to the session's working directory.",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Maximum number of file paths to return (defaults to 100).",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}

func (t *GrepFilesTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.fs == nil {
		return "", errors.New("grep_files not configured")
	}
	var a grepFilesArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	pattern := strings.TrimSpace(a.Pattern)
	if pattern == "" {
		return "", errors.New("pattern must not be empty")
	}
	limit := grepDefaultLimit
	if a.Limit != nil {
		if *a.Limit <= 0 {
			return "", errors.New("limit must be greater than zero")
		}
		limit = *a.Limit
	}
	if limit > grepMaxLimit {
		limit = grepMaxLimit
	}

	path := ""
	if a.Path != nil {
		path = strings.TrimSpace(*a.Path)
	}
	if path == "" {
		if tc.BaseDir == "" {
			return "", errors.New("path required")
		}
		path = tc.BaseDir
	}

	abs, allowed, err := t.fs.resolvePathWithApproval(ctx, tc, "grep_files", tc.BaseDir, path, false)
	if err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "grep_files", path, allowed, err)
		return "", err
	}

	if _, err := os.Stat(abs); err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "grep_files", abs, true, err)
		return "", err
	}

	include := ""
	if a.Include != nil {
		include = strings.TrimSpace(*a.Include)
	}

	cwd := tc.BaseDir
	if cwd == "" {
		cwd = filepath.Dir(abs)
	}

	results, err := runGrepFiles(ctx, pattern, include, abs, cwd, limit)
	if err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "grep_files", abs, true, err)
		return "", err
	}
	if len(results) == 0 {
		t.fs.auditOK(ctx, tc.SessionID, tc.UserID, "grep_files", abs, map[string]any{"count": 0})
		return "No matches found.", nil
	}
	t.fs.auditOK(ctx, tc.SessionID, tc.UserID, "grep_files", abs, map[string]any{"count": len(results)})
	return strings.Join(results, "\n"), nil
}

func runGrepFiles(ctx context.Context, pattern, include, searchPath, cwd string, limit int) ([]string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, grepTimeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "rg",
		"--files-with-matches",
		"--sortr=modified",
		"--regexp", pattern,
		"--no-messages",
	)
	if include != "" {
		cmd.Args = append(cmd.Args, "--glob", include)
	}
	cmd.Args = append(cmd.Args, "--", searchPath)
	cmd.Dir = cwd

	output, err := cmd.CombinedOutput()
	if err != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return nil, errors.New("rg timed out after 30 seconds")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return nil, nil
			}
			stderr := strings.TrimSpace(string(output))
			if stderr == "" {
				stderr = exitErr.Error()
			}
			return nil, errors.New("rg failed: " + stderr)
		}
		return nil, errors.New("failed to launch rg: " + err.Error() + ". Ensure ripgrep is installed and on PATH.")
	}
	return parseGrepResults(output, limit), nil
}

func parseGrepResults(stdout []byte, limit int) []string {
	if limit <= 0 {
		return nil
	}
	lines := strings.Split(string(stdout), "\n")
	results := make([]string, 0, limit)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		results = append(results, line)
		if len(results) == limit {
			break
		}
	}
	return results
}
