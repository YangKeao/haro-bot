package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/skills"
	"go.uber.org/zap"
)

type InstallSkillTool struct {
	skills *skills.Manager
}

type installSkillArgs struct {
	SourceType    string   `json:"source_type"`
	InstallMethod string   `json:"install_method"`
	URL           string   `json:"url"`
	Ref           string   `json:"ref"`
	Subdir        string   `json:"subdir"`
	IncludeSkills []string `json:"include_skills"`
	Status        string   `json:"status"`
}

type installSkillResult struct {
	SourceID      int64                `json:"source_id"`
	IncludeSkills []string             `json:"include_skills,omitempty"`
	Skills        []installSkillRecord `json:"skills,omitempty"`
}

type installSkillRecord struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version,omitempty"`
	Dir         string `json:"dir,omitempty"`
}

func NewInstallSkillTool(skillsMgr *skills.Manager) *InstallSkillTool {
	return &InstallSkillTool{skills: skillsMgr}
}

func (t *InstallSkillTool) Name() string { return "install_skill" }
func (t *InstallSkillTool) Description() string {
	return "Register and download a skill source (e.g. a git repository)."
}
func (t *InstallSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_type": map[string]any{
				"type":        "string",
				"description": "Source type (default: git).",
			},
			"install_method": map[string]any{
				"type":        "string",
				"description": "Install method (defaults to source_type).",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Repository URL or source address.",
			},
			"ref": map[string]any{
				"type":        "string",
				"description": "Git ref/branch/tag/commit (optional).",
			},
			"subdir": map[string]any{
				"type":        "string",
				"description": "Subdirectory within the repo to scan for skills.",
			},
			"include_skills": map[string]any{
				"type":        "array",
				"description": "Optional skill-name allowlist. When set, only these skill names are loaded from the source.",
				"items": map[string]any{
					"type": "string",
				},
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Source status (default: active).",
			},
		},
		"required": []string{"url"},
	}
}

func (t *InstallSkillTool) Execute(ctx context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	log := logging.L().Named("install_skill")
	if t.skills == nil {
		return "", errors.New("skills manager not configured")
	}
	var payload installSkillArgs
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", err
	}
	if payload.SourceType == "" {
		payload.SourceType = "git"
	}
	sourceID, err := t.skills.RegisterSource(ctx, skills.Source{
		SourceType:    payload.SourceType,
		InstallMethod: payload.InstallMethod,
		URL:           payload.URL,
		Ref:           payload.Ref,
		Subdir:        payload.Subdir,
		SkillFilters:  payload.IncludeSkills,
		Status:        payload.Status,
	})
	if err != nil {
		log.Warn("register source failed", zap.Error(err))
		return "", err
	}
	if err := t.skills.RefreshSource(ctx, sourceID); err != nil {
		log.Warn("refresh source failed", zap.Int64("source_id", sourceID), zap.Error(err))
		return "", err
	}
	installed, err := t.skills.ListBySource(ctx, sourceID)
	if err != nil {
		log.Warn("list skills failed", zap.Int64("source_id", sourceID), zap.Error(err))
		return "", err
	}
	result := installSkillResult{
		SourceID:      sourceID,
		IncludeSkills: payload.IncludeSkills,
	}
	if len(installed) > 0 {
		result.Skills = make([]installSkillRecord, 0, len(installed))
		for _, meta := range installed {
			result.Skills = append(result.Skills, installSkillRecord{
				Name:        meta.Name,
				Description: meta.Description,
				Version:     meta.Version,
				Dir:         meta.Dir,
			})
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
