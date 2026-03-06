package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/memory"
)

const (
	defaultMemorySearchLimit    = 20
	maxMemorySearchLimit        = 50
	defaultMemorySearchMaxChars = 300
)

type MemorySearchTool struct {
	store memory.StoreAPI
}

type memorySearchArgs struct {
	Query       string `json:"query"`
	Limit       int    `json:"limit"`
	IncludeTool bool   `json:"include_tool"`
	MaxChars    int    `json:"max_chars"`
}

type memorySearchResult struct {
	ID         int64  `json:"id"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
	ContentLen int    `json:"content_len"`
	CreatedAt  string `json:"created_at"`
}

type memorySearchOutput struct {
	Query   string               `json:"query"`
	Count   int                  `json:"count"`
	Results []memorySearchResult `json:"results"`
}

func NewMemorySearchTool(store memory.StoreAPI) *MemorySearchTool {
	return &MemorySearchTool{store: store}
}

func (t *MemorySearchTool) Name() string { return "memory_search" }

func (t *MemorySearchTool) Description() string {
	return "Search past session messages for a query substring."
}

func (t *MemorySearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query substring.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum results (default 20, max 50).",
			},
			"include_tool": map[string]any{
				"type":        "boolean",
				"description": "Include tool output messages (default false).",
			},
			"max_chars": map[string]any{
				"type":        "integer",
				"description": "Maximum characters per result content (default 300, 0 for no limit).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *MemorySearchTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.store == nil {
		return "", errors.New("memory store not configured")
	}
	if tc.SessionID == 0 {
		return "", errors.New("session_id required")
	}
	var payload memorySearchArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	query := strings.TrimSpace(payload.Query)
	if query == "" {
		return "", errors.New("query required")
	}
	limit := payload.Limit
	if limit <= 0 {
		limit = defaultMemorySearchLimit
	}
	if limit > maxMemorySearchLimit {
		limit = maxMemorySearchLimit
	}
	maxChars := payload.MaxChars
	if maxChars == 0 {
		maxChars = defaultMemorySearchMaxChars
	}

	msgs, err := t.store.SearchMessages(ctx, tc.SessionID, query, limit, payload.IncludeTool)
	if err != nil {
		return "", err
	}

	results := make([]memorySearchResult, 0, len(msgs))
	for _, msg := range msgs {
		content := msg.Content
		truncated := false
		if maxChars > 0 && len(content) > maxChars {
			content = content[:maxChars]
			truncated = true
		}
		results = append(results, memorySearchResult{
			ID:         msg.ID,
			Role:       msg.Role,
			Content:    content,
			Truncated:  truncated,
			ContentLen: len(msg.Content),
			CreatedAt:  msg.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	out := memorySearchOutput{
		Query:   query,
		Count:   len(results),
		Results: results,
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
