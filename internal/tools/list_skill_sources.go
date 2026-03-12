package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/YangKeao/haro-bot/internal/skills"
)

type ListSkillSourcesTool struct {
	skills *skills.Manager
}

type listSkillSourcesArgs struct {
	IncludeDisabled bool `json:"include_disabled"`
}

type listSkillSourcesResult struct {
	Sources []listSkillSourceRecord `json:"sources"`
}

type listSkillSourceRecord struct {
	ID            int64             `json:"id"`
	SourceType    string            `json:"source_type"`
	InstallMethod string            `json:"install_method"`
	URL           string            `json:"url"`
	Ref           string            `json:"ref,omitempty"`
	Subdir        string            `json:"subdir,omitempty"`
	IncludeSkills []string          `json:"include_skills,omitempty"`
	Status        string            `json:"status"`
	Version       string            `json:"version,omitempty"`
	LastSyncAt    string            `json:"last_sync_at,omitempty"`
	LastError     string            `json:"last_error,omitempty"`
	Skills        []listSkillRecord `json:"skills,omitempty"`
}

type listSkillRecord struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version,omitempty"`
}

func NewListSkillSourcesTool(skillsMgr *skills.Manager) *ListSkillSourcesTool {
	return &ListSkillSourcesTool{skills: skillsMgr}
}

func (t *ListSkillSourcesTool) Name() string { return "list_skill_sources" }

func (t *ListSkillSourcesTool) Description() string {
	return "List registered skill sources and the skills currently loaded from each source."
}

func (t *ListSkillSourcesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"include_disabled": map[string]any{
				"type":        "boolean",
				"description": "When true, include disabled or deleted sources.",
			},
		},
	}
}

func (t *ListSkillSourcesTool) Execute(ctx context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	if t.skills == nil {
		return "", errors.New("skills manager not configured")
	}
	var payload listSkillSourcesArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", err
		}
	}
	sources, err := t.skills.ListSources(ctx, payload.IncludeDisabled)
	if err != nil {
		return "", err
	}
	result := listSkillSourcesResult{
		Sources: make([]listSkillSourceRecord, 0, len(sources)),
	}
	for _, source := range sources {
		record := listSkillSourceRecord{
			ID:            source.ID,
			SourceType:    source.SourceType,
			InstallMethod: source.InstallMethod,
			URL:           source.URL,
			Ref:           source.Ref,
			Subdir:        source.Subdir,
			IncludeSkills: source.SkillFilters,
			Status:        source.Status,
			Version:       source.Version,
			LastError:     source.LastError,
		}
		if source.LastSyncAt != nil {
			record.LastSyncAt = source.LastSyncAt.UTC().Format(time.RFC3339)
		}
		loadedSkills, err := t.skills.ListBySource(ctx, source.ID)
		if err != nil {
			return "", err
		}
		if len(loadedSkills) > 0 {
			record.Skills = make([]listSkillRecord, 0, len(loadedSkills))
			for _, skill := range loadedSkills {
				record.Skills = append(record.Skills, listSkillRecord{
					Name:        skill.Name,
					Description: skill.Description,
					Version:     skill.Version,
				})
			}
		}
		result.Sources = append(result.Sources, record)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
