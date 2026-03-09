package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/YangKeao/haro-bot/internal/guidelines"
)

type UpdateGuidelinesTool struct {
	guidelinesMgr *guidelines.Manager
}

func NewUpdateGuidelinesTool(mgr *guidelines.Manager) *UpdateGuidelinesTool {
	return &UpdateGuidelinesTool{guidelinesMgr: mgr}
}

func (t *UpdateGuidelinesTool) Name() string {
	return "update_guidelines"
}

func (t *UpdateGuidelinesTool) Description() string {
	return "Updates the bot's guidelines (core principles, personality, and behavioral rules). Guidelines are loaded into the system prompt and guide the LLM's behavior. Use this tool to modify how the bot behaves, its personality traits, or important rules it should follow. Changes create a new version; previous versions can be restored via rollback."
}

func (t *UpdateGuidelinesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The new guidelines content. This should be a well-structured document describing the bot's personality, principles, and behavioral rules. Markdown format is recommended.",
			},
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"update", "view", "history", "rollback"},
				"description": "The action to perform: 'update' to set new content, 'view' to see current guidelines, 'history' to list all versions, 'rollback' to restore a previous version.",
			},
			"version": map[string]any{
				"type":        "integer",
				"description": "Required for 'rollback' action: the version number to restore.",
			},
		},
		"required": []string{"action"},
	}
}

type guidelinesInput struct {
	Action  string `json:"action"`
	Content string `json:"content"`
	Version int    `json:"version"`
}

func (t *UpdateGuidelinesTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t.guidelinesMgr == nil {
		return "", fmt.Errorf("guidelines manager not available")
	}

	var input guidelinesInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parse parameters: %w", err)
	}

	var result map[string]any

	switch input.Action {
	case "view":
		current, err := t.guidelinesMgr.GetActive(ctx)
		if err != nil {
			return "", fmt.Errorf("get guidelines: %w", err)
		}
		if current == nil {
			result = map[string]any{
				"message": "No guidelines are currently set. Use 'update' action to create one.",
			}
		} else {
			result = map[string]any{
				"version":  current.Version,
				"content":  current.Content,
				"isActive": current.IsActive,
			}
		}

	case "update":
		if input.Content == "" {
			return "", fmt.Errorf("content is required for update action")
		}
		updated, err := t.guidelinesMgr.Update(ctx, input.Content)
		if err != nil {
			return "", fmt.Errorf("update guidelines: %w", err)
		}
		result = map[string]any{
			"message":  fmt.Sprintf("Guidelines updated to version %d", updated.Version),
			"version":  updated.Version,
			"isActive": updated.IsActive,
		}

	case "history":
		all, err := t.guidelinesMgr.GetAll(ctx, 20)
		if err != nil {
			return "", fmt.Errorf("get history: %w", err)
		}
		if len(all) == 0 {
			result = map[string]any{
				"message": "No guidelines history found.",
			}
		} else {
			versions := make([]map[string]any, len(all))
			for i, c := range all {
				versions[i] = map[string]any{
					"version":  c.Version,
					"isActive": c.IsActive,
					"preview":  truncatePreview(c.Content, 200),
				}
			}
			result = map[string]any{
				"versions": versions,
			}
		}

	case "rollback":
		if input.Version <= 0 {
			return "", fmt.Errorf("version is required for rollback action")
		}
		restored, err := t.guidelinesMgr.Rollback(ctx, input.Version)
		if err != nil {
			return "", fmt.Errorf("rollback guidelines: %w", err)
		}
		result = map[string]any{
			"message":  fmt.Sprintf("Guidelines rolled back to version %d", restored.Version),
			"version":  restored.Version,
			"isActive": restored.IsActive,
		}

	default:
		return "", fmt.Errorf("unknown action: %s", input.Action)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(data), nil
}

func truncatePreview(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
