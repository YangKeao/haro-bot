package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	readDefaultOffset = 1
	readDefaultLimit  = 2000
	readMaxLineLength = 500
	readTabWidth      = 4
)

type ReadFileTool struct {
	fs *FS
}

type readFileArgs struct {
	FilePath   string `json:"file_path"`
	Offset     *int   `json:"offset"`
	Limit      *int   `json:"limit"`
	Mode       string `json:"mode"`
	AnchorLine *int   `json:"anchor_line"`
	MaxLevels  int    `json:"max_levels"`
	MaxLines   *int   `json:"max_lines"`
}

func NewReadFileTool(fs *FS) *ReadFileTool {
	return &ReadFileTool{fs: fs}
}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Reads a local file with 1-indexed line numbers, supporting slice and indentation-aware block modes."
}

func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "The line number to start reading from. Must be 1 or greater.",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "The maximum number of lines to return.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Optional mode selector: \"slice\" for simple ranges (default) or \"indentation\" to expand around an anchor line.",
			},
			"anchor_line": map[string]any{
				"type":        "number",
				"description": "Anchor line to center the indentation lookup on (defaults to offset).",
			},
			"max_levels": map[string]any{
				"type":        "number",
				"description": "How many parent indentation levels (smaller indents) to include.",
			},
			"max_lines": map[string]any{
				"type":        "number",
				"description": "Hard cap on the number of lines returned when using indentation mode.",
			},
		},
		"required":             []string{"file_path"},
		"additionalProperties": false,
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.fs == nil {
		return "", errors.New("read_file not configured")
	}
	var a readFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if a.FilePath == "" {
		return "", errors.New("file_path required")
	}
	if !filepath.IsAbs(a.FilePath) {
		return "", errors.New("file_path must be an absolute path")
	}

	offset := readDefaultOffset
	if a.Offset != nil {
		if *a.Offset <= 0 {
			return "", errors.New("offset must be a 1-indexed line number")
		}
		offset = *a.Offset
	}

	limit := readDefaultLimit
	if a.Limit != nil {
		if *a.Limit <= 0 {
			return "", errors.New("limit must be greater than zero")
		}
		limit = *a.Limit
	}

	mode := strings.ToLower(strings.TrimSpace(a.Mode))
	if mode == "" {
		mode = "slice"
	}
	if mode != "slice" && mode != "indentation" {
		return "", errors.New("mode must be \"slice\" or \"indentation\"")
	}

	abs, allowed, err := t.fs.resolvePathWithApproval(ctx, tc, "read_file", "", a.FilePath, false)
	if err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "read_file", a.FilePath, allowed, err)
		return "", err
	}

	var lines []string
	switch mode {
	case "slice":
		lines, err = readFileSlice(abs, offset, limit)
	case "indentation":
		lines, err = readFileIndentation(abs, offset, limit, a.AnchorLine, a.MaxLevels, a.MaxLines)
	}
	if err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "read_file", abs, true, err)
		return "", err
	}
	t.fs.auditOK(ctx, tc.SessionID, tc.UserID, "read_file", abs, map[string]any{"lines": len(lines)})
	return strings.Join(lines, "\n"), nil
}

func readFileSlice(path string, offset, limit int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	collected := make([]string, 0, limit)
	seen := 0
	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		if len(lineBytes) == 0 && errors.Is(err, io.EOF) {
			break
		}
		lineBytes = trimLineEnding(lineBytes)
		seen++
		if seen < offset {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}
		if len(collected) < limit {
			collected = append(collected, fmt.Sprintf("L%d: %s", seen, formatLine(lineBytes)))
		}
		if len(collected) == limit {
			break
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if seen < offset {
		return nil, errors.New("offset exceeds file length")
	}
	return collected, nil
}

func readFileIndentation(path string, offset, limit int, anchorLineArg *int, maxLevels int, maxLinesArg *int) ([]string, error) {
	lines, err := collectFileLines(path)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errors.New("anchor_line exceeds file length")
	}

	anchorLine := offset
	if anchorLineArg != nil {
		if *anchorLineArg <= 0 {
			return nil, errors.New("anchor_line must be a 1-indexed line number")
		}
		anchorLine = *anchorLineArg
	}
	if anchorLine <= 0 {
		return nil, errors.New("anchor_line must be a 1-indexed line number")
	}
	if anchorLine > len(lines) {
		return nil, errors.New("anchor_line exceeds file length")
	}

	guardLimit := limit
	if maxLinesArg != nil {
		if *maxLinesArg <= 0 {
			return nil, errors.New("max_lines must be greater than zero")
		}
		guardLimit = *maxLinesArg
	}
	if guardLimit <= 0 {
		return nil, errors.New("max_lines must be greater than zero")
	}

	anchorIndex := anchorLine - 1
	effectiveIndents := computeEffectiveIndents(lines)
	anchorIndent := effectiveIndents[anchorIndex]

	minIndent := 0
	if maxLevels > 0 {
		candidate := anchorIndent - maxLevels*readTabWidth
		if candidate > 0 {
			minIndent = candidate
		}
	}

	finalLimit := limit
	if guardLimit < finalLimit {
		finalLimit = guardLimit
	}
	if len(lines) < finalLimit {
		finalLimit = len(lines)
	}
	if finalLimit == 1 {
		line := lines[anchorIndex]
		return []string{fmt.Sprintf("L%d: %s", line.number, line.display)}, nil
	}

	out := make([]*lineRecord, 0, finalLimit)
	out = append(out, lines[anchorIndex])

	i := anchorIndex - 1
	j := anchorIndex + 1

	for len(out) < finalLimit {
		progressed := 0

		if i >= 0 {
			if effectiveIndents[i] >= minIndent {
				out = append([]*lineRecord{lines[i]}, out...)
				progressed++
				if effectiveIndents[i] == minIndent {
					// Stop at parent block boundary
					i = -1
					continue
				}
				i--
				if len(out) >= finalLimit {
					break
				}
			} else {
				i = -1
			}
		}

		if j < len(lines) {
			if effectiveIndents[j] >= anchorIndent {
				out = append(out, lines[j])
				progressed++
				j++
			} else {
				j = len(lines)
			}
		}

		if progressed == 0 {
			break
		}
	}

	out = trimEmptyLines(out)
	formatted := make([]string, 0, len(out))
	for _, line := range out {
		formatted = append(formatted, fmt.Sprintf("L%d: %s", line.number, line.display))
	}
	return formatted, nil
}

type lineRecord struct {
	number  int
	raw     string
	display string
	indent  int
}

func (l *lineRecord) isBlank() bool {
	return strings.TrimSpace(l.raw) == ""
}

func (l *lineRecord) isComment() bool {
func collectFileLines(path string) ([]*lineRecord, error) {
	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		if len(lineBytes) == 0 && errors.Is(err, io.EOF) {
			break
		}
		lineBytes = trimLineEnding(lineBytes)
		number++
		raw := string(lineBytes)
		record := &lineRecord{
			number:  number,
			raw:     raw,
			display: formatLine(lineBytes),
			indent:  measureIndent(raw),
		}
		lines = append(lines, record)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return lines, nil
}

func computeEffectiveIndents(lines []*lineRecord) []int {
	out := make([]int, 0, len(lines))
	prev := 0
	for _, line := range lines {
		if line.isBlank() {
			out = append(out, prev)
			continue
		}
		prev = line.indent
		out = append(out, prev)
	}
	return out
}

func measureIndent(line string) int {
	indent := 0
	for _, r := range line {
		if r == ' ' {
			indent++
			continue
		}
		if r == '\t' {
			indent += readTabWidth
			continue
		}
		break
	}
	return indent
}

func trimEmptyLines(lines []*lineRecord) []*lineRecord {
	for len(lines) > 0 && lines[0].isBlank() {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1].isBlank() {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func formatLine(bytes []byte) string {
	decoded := string(bytes)
	if len(decoded) <= readMaxLineLength {
		return decoded
	}
	return truncateUTF8(decoded, readMaxLineLength)
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	if cut <= 0 {
		return ""
	}
	return s[:cut]
}

func trimLineEnding(line []byte) []byte {
	if len(line) == 0 {
		return line
	}
	if line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line
}
