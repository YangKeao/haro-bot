package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/YangKeao/haro-bot/internal/skills"
)

type RefreshSkillsTool struct {
	skills *skills.Manager
}

type refreshSkillsArgs struct {
	SourceID int64 `json:"source_id"`
}

type refreshSkillsResult struct {
	Scope      string               `json:"scope"`
	SourceID   int64                `json:"source_id,omitempty"`
	Skills     []refreshSkillRecord `json:"skills,omitempty"`
	SkillCount int                  `json:"skill_count"`
}

type refreshSkillRecord struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version,omitempty"`
}

func NewRefreshSkillsTool(skillsMgr *skills.Manager) *RefreshSkillsTool {
	return &RefreshSkillsTool{skills: skillsMgr}
}

func (t *RefreshSkillsTool) Name() string { return "refresh_skills" }

func (t *RefreshSkillsTool) Description() string {
	return "Refresh all skill sources, or one specific source when source_id is provided."
}

func (t *RefreshSkillsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_id": map[string]any{
				"type":        "integer",
				"description": "Optional source id. When omitted, refreshes all active sources.",
			},
		},
	}
}

func (t *RefreshSkillsTool) Execute(ctx context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	if t.skills == nil {
		return "", errors.New("skills manager not configured")
	}
	var payload refreshSkillsArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", err
		}
	}
	result := refreshSkillsResult{}
	if payload.SourceID > 0 {
		if err := t.skills.RefreshSource(ctx, payload.SourceID); err != nil {
			return "", err
		}
		loadedSkills, err := t.skills.ListBySource(ctx, payload.SourceID)
		if err != nil {
			return "", err
		}
		result.Scope = "source"
		result.SourceID = payload.SourceID
		result.SkillCount = len(loadedSkills)
		result.Skills = make([]refreshSkillRecord, 0, len(loadedSkills))
		for _, skill := range loadedSkills {
			result.Skills = append(result.Skills, refreshSkillRecord{
				Name:        skill.Name,
				Description: skill.Description,
				Version:     skill.Version,
			})
		}
	} else {
		if err := t.skills.RefreshAll(ctx); err != nil {
			return "", err
		}
		loadedSkills := t.skills.List()
		result.Scope = "all"
		result.SkillCount = len(loadedSkills)
		result.Skills = make([]refreshSkillRecord, 0, len(loadedSkills))
		for _, skill := range loadedSkills {
			result.Skills = append(result.Skills, refreshSkillRecord{
				Name:        skill.Name,
				Description: skill.Description,
				Version:     skill.Version,
			})
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
