package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/YangKeao/haro-bot/internal/skills"
)

type DeleteSkillSourceTool struct {
	skills *skills.Manager
}

type deleteSkillSourceArgs struct {
	SourceID int64 `json:"source_id"`
}

type deleteSkillSourceResult struct {
	SourceID int64 `json:"source_id"`
	Deleted  bool  `json:"deleted"`
}

func NewDeleteSkillSourceTool(skillsMgr *skills.Manager) *DeleteSkillSourceTool {
	return &DeleteSkillSourceTool{skills: skillsMgr}
}

func (t *DeleteSkillSourceTool) Name() string { return "delete_skill_source" }

func (t *DeleteSkillSourceTool) Description() string {
	return "Delete a registered skill source and remove its loaded skills."
}

func (t *DeleteSkillSourceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_id": map[string]any{
				"type":        "integer",
				"description": "Registered skill source id.",
			},
		},
		"required": []string{"source_id"},
	}
}

func (t *DeleteSkillSourceTool) Execute(ctx context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	if t.skills == nil {
		return "", errors.New("skills manager not configured")
	}
	var payload deleteSkillSourceArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if payload.SourceID <= 0 {
		return "", errors.New("delete_skill_source missing source_id")
	}
	if err := t.skills.DeleteSource(ctx, payload.SourceID); err != nil {
		return "", err
	}
	raw, err := json.Marshal(deleteSkillSourceResult{
		SourceID: payload.SourceID,
		Deleted:  true,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
