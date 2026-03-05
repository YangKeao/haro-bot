package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/skills"
	"go.uber.org/zap"
)

type ActivateSkillTool struct {
	skills *skills.Manager
}

type activateSkillArgs struct {
	Name string `json:"name"`
	Goal string `json:"goal"`
}

func NewActivateSkillTool(skillsMgr *skills.Manager) *ActivateSkillTool {
	return &ActivateSkillTool{skills: skillsMgr}
}

func (t *ActivateSkillTool) Name() string { return "activate_skill" }
func (t *ActivateSkillTool) Description() string {
	return "Activate a skill from the available skills list."
}
func (t *ActivateSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name",
			},
			"goal": map[string]any{
				"type":        "string",
				"description": "User goal for the skill",
			},
		},
		"required": []string{"name"},
	}
}

func (t *ActivateSkillTool) Execute(ctx context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	log := logging.L().Named("activate_skill")
	if t.skills == nil {
		return "", errors.New("skills manager not configured")
	}
	var payload activateSkillArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if payload.Name == "" {
		return "", errors.New("activate_skill missing name")
	}
	if _, err := t.skills.Load(payload.Name); err != nil {
		log.Warn("activate skill failed", zap.String("name", payload.Name), zap.Error(err))
		return "", err
	}
	log.Info("skill activated", zap.String("name", payload.Name))
	return "activated skill: " + payload.Name, nil
}
