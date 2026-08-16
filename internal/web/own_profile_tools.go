package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
)

type ownProfileView struct {
	ProviderName                   string   `json:"provider_name"`
	Name                           string   `json:"name"`
	Description                    string   `json:"description"`
	Instructions                   string   `json:"instructions"`
	Icon                           string   `json:"icon"`
	Color                          string   `json:"color"`
	AvatarMode                     string   `json:"avatar_mode"`
	AvatarURL                      string   `json:"avatar_url,omitempty"`
	Model                          string   `json:"model"`
	ReasoningEffortOverride        *string  `json:"reasoning_effort_override"`
	ProviderDefaultReasoningEffort string   `json:"provider_default_reasoning_effort,omitempty"`
	ContextWindowOverride          *int     `json:"context_window_override"`
	ResolvedContextWindow          int      `json:"resolved_context_window"`
	AutoCompactTokenLimitOverride  *int     `json:"auto_compact_token_limit_override"`
	ResolvedAutoCompactTokenLimit  int      `json:"resolved_auto_compact_token_limit"`
	EffectiveContextWindowPercent  int      `json:"effective_context_window_percent"`
	SkillNames                     []string `json:"skill_names"`
}

func ownProfileResponse(profile AgentProfile) ownProfileView {
	return ownProfileView{
		ProviderName: profile.ProviderName, Name: profile.Name, Description: profile.Description, Instructions: profile.Instructions,
		Icon: profile.Icon, Color: profile.Color, AvatarMode: profile.AvatarMode, AvatarURL: profile.AvatarURL,
		Model: profile.Model, ReasoningEffortOverride: profile.ReasoningEffortOverride,
		ProviderDefaultReasoningEffort: profile.ProviderDefaultReasoningEffort,
		ContextWindowOverride:          profile.ContextWindowOverride, ResolvedContextWindow: profile.ResolvedContextWindow,
		AutoCompactTokenLimitOverride: profile.AutoCompactTokenLimitOverride, ResolvedAutoCompactTokenLimit: profile.ResolvedAutoCompactTokenLimit,
		EffectiveContextWindowPercent: profile.EffectiveContextWindowPercent, SkillNames: profile.SkillNames,
	}
}

type getOwnProfileTool struct {
	agentID int64
	store   *Store
}

func (t *getOwnProfileTool) Name() string { return "get_own_profile" }

func (t *getOwnProfileTool) Description() string {
	return "Read this agent's own editable profile and runtime settings. This tool cannot access any other agent or reveal provider credentials."
}

func (t *getOwnProfileTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
}

func (t *getOwnProfileTool) Execute(ctx context.Context, _ tools.ToolContext, _ json.RawMessage) (string, error) {
	profile, err := t.store.GetAgent(ctx, t.agentID)
	if err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(ownProfileResponse(profile), "", "  ")
	return string(encoded), err
}

type updateOwnProfileTool struct {
	agentID    int64
	store      *Store
	skills     *skills.Manager
	objects    *ObjectStore
	downloader *avatarDownloader
	invalidate func()
}

type ownProfileUpdate struct {
	Name                          *string   `json:"name"`
	Description                   *string   `json:"description"`
	Instructions                  *string   `json:"instructions"`
	Icon                          *string   `json:"icon"`
	Color                         *string   `json:"color"`
	AvatarMode                    *string   `json:"avatar_mode"`
	AvatarURL                     *string   `json:"avatar_url"`
	RemoveAvatarImage             *bool     `json:"remove_avatar_image"`
	Model                         *string   `json:"model"`
	ReasoningEffortOverride       *string   `json:"reasoning_effort_override"`
	ContextWindowOverride         *int      `json:"context_window_override"`
	AutoCompactTokenLimitOverride *int      `json:"auto_compact_token_limit_override"`
	EffectiveContextWindowPercent *int      `json:"effective_context_window_percent"`
	SkillNames                    *[]string `json:"skill_names"`
}

func (t *updateOwnProfileTool) Name() string { return "update_own_profile" }

func (t *updateOwnProfileTool) Description() string {
	return "Persistently patch this agent's own profile, avatar, model, reasoning, context, or skills. Send only fields the user explicitly asked to change and omit every unchanged field; never invent an avatar URL or placeholder image. Only call this after the user explicitly asks for the change. Provider URL, API key, prompt format, global guidelines, archive state, and other agents are never editable. Changes to runtime behavior apply from the next user message."
}

func (t *updateOwnProfileTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"name":                              map[string]any{"type": "string", "description": "New display name, at most 128 characters."},
			"description":                       map[string]any{"type": "string", "description": "New short profile description."},
			"instructions":                      map[string]any{"type": "string", "description": "Complete replacement agent instructions in Markdown."},
			"icon":                              map[string]any{"type": "string", "enum": []string{"sparkles", "bot", "search", "code", "book", "chart", "terminal", "brain"}},
			"color":                             map[string]any{"type": "string", "description": "Six-digit hex color such as #2563EB."},
			"avatar_mode":                       map[string]any{"type": "string", "enum": []string{"icon", "image"}, "description": "Optional avatar change. Omit unless the user asked to change the avatar. Use the built-in icon or the stored uploaded image."},
			"avatar_url":                        map[string]any{"type": "string", "description": "Optional avatar change. Omit unless the user supplied or explicitly requested an avatar image; never send an empty or invented placeholder URL. A non-empty public HTTP or HTTPS image URL is downloaded into private storage and requires avatar_mode image when that field is also present."},
			"remove_avatar_image":               map[string]any{"type": "boolean", "description": "Optional avatar change. Send true only when the user explicitly asked to delete the stored image and fall back to the built-in icon; otherwise omit it."},
			"model":                             map[string]any{"type": "string"},
			"reasoning_effort_override":         map[string]any{"type": "string", "description": "Provider-specific effort; use an empty string to return to the provider default."},
			"context_window_override":           map[string]any{"type": "integer", "minimum": 0, "description": "Positive manual value, or 0 to follow provider metadata."},
			"auto_compact_token_limit_override": map[string]any{"type": "integer", "minimum": 0, "description": "Positive manual value, or 0 to follow provider metadata."},
			"effective_context_window_percent":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			"skill_names":                       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Complete replacement list of installed skill names."},
		},
	}
}

func (t *updateOwnProfileTool) Execute(ctx context.Context, _ tools.ToolContext, raw json.RawMessage) (string, error) {
	var update ownProfileUpdate
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&update); err != nil {
		return "", fmt.Errorf("invalid profile update: %w", err)
	}
	update.normalizePatch()
	if update.empty() {
		return "", errors.New("at least one profile field is required")
	}
	if err := update.validateAvatarPatch(); err != nil {
		return "", err
	}
	current, err := t.store.GetAgent(ctx, t.agentID)
	if err != nil {
		return "", err
	}
	input := agentInputFromProfile(current)
	applyOwnProfileUpdate(&input, update)
	write, err := normalizeAgentInput(input)
	if err != nil {
		return "", err
	}
	write.AvatarObjectKey = current.AvatarObjectKey
	write.AvatarMIMEType = current.AvatarMIMEType
	if update.SkillNames != nil {
		if err := validateSelectedSkills(t.skills, write.SkillNames); err != nil {
			return "", err
		}
	}

	oldAvatarKey := ""
	newAvatarKey := ""
	if update.RemoveAvatarImage != nil && *update.RemoveAvatarImage {
		oldAvatarKey = current.AvatarObjectKey
		write.AvatarObjectKey = ""
		write.AvatarMIMEType = ""
		write.AvatarMode = "icon"
	}
	if update.AvatarURL != nil {
		image, err := t.downloader.Fetch(ctx, *update.AvatarURL)
		if err != nil {
			return "", err
		}
		newAvatarKey, err = storeAvatarImage(ctx, t.objects, image)
		if err != nil {
			return "", err
		}
		oldAvatarKey = current.AvatarObjectKey
		write.AvatarObjectKey = newAvatarKey
		write.AvatarMIMEType = image.MIMEType
		write.AvatarMode = "image"
	}
	if write.AvatarMode == "image" && write.AvatarObjectKey == "" {
		if newAvatarKey != "" {
			_ = t.objects.Delete(ctx, newAvatarKey)
		}
		return "", errors.New("avatar_mode image requires a stored image or avatar_url")
	}
	profile, err := t.store.UpdateAgent(ctx, t.agentID, write)
	if err != nil {
		if newAvatarKey != "" {
			_ = t.objects.Delete(ctx, newAvatarKey)
		}
		return "", err
	}
	if oldAvatarKey != "" && oldAvatarKey != newAvatarKey {
		_ = t.objects.Delete(ctx, oldAvatarKey)
	}
	if t.invalidate != nil {
		t.invalidate()
	}
	result := struct {
		Profile ownProfileView `json:"profile"`
		Note    string         `json:"note"`
	}{Profile: ownProfileResponse(profile), Note: "Saved. Runtime changes apply from the next user message."}
	encoded, err := json.MarshalIndent(result, "", "  ")
	return string(encoded), err
}

func (u *ownProfileUpdate) normalizePatch() {
	if u.AvatarURL != nil {
		value := strings.TrimSpace(*u.AvatarURL)
		if value == "" {
			u.AvatarURL = nil
		} else {
			u.AvatarURL = &value
		}
	}
	if u.RemoveAvatarImage != nil && !*u.RemoveAvatarImage {
		u.RemoveAvatarImage = nil
	}
}

func (u ownProfileUpdate) validateAvatarPatch() error {
	if u.AvatarURL == nil {
		return nil
	}
	if u.RemoveAvatarImage != nil && *u.RemoveAvatarImage {
		return errors.New("avatar_url and remove_avatar_image cannot be used together")
	}
	if u.AvatarMode != nil && strings.TrimSpace(*u.AvatarMode) != "image" {
		return errors.New("avatar_url cannot be used with avatar_mode icon")
	}
	return nil
}

func (u ownProfileUpdate) empty() bool {
	return u.Name == nil && u.Description == nil && u.Instructions == nil && u.Icon == nil && u.Color == nil &&
		u.AvatarMode == nil && u.AvatarURL == nil && u.RemoveAvatarImage == nil && u.Model == nil &&
		u.ReasoningEffortOverride == nil && u.ContextWindowOverride == nil &&
		u.AutoCompactTokenLimitOverride == nil && u.EffectiveContextWindowPercent == nil && u.SkillNames == nil
}

func agentInputFromProfile(profile AgentProfile) agentInput {
	return agentInput{
		ProviderID: profile.ProviderID, SandboxID: profile.SandboxID, Name: profile.Name, Description: profile.Description, Icon: profile.Icon, Color: profile.Color,
		AvatarMode: profile.AvatarMode, Instructions: profile.Instructions,
		Model: profile.Model, ReasoningEffortOverride: profile.ReasoningEffortOverride,
		ContextWindowOverride:         profile.ContextWindowOverride,
		AutoCompactTokenLimitOverride: profile.AutoCompactTokenLimitOverride,
		EffectiveContextWindowPercent: profile.EffectiveContextWindowPercent, SkillNames: append([]string(nil), profile.SkillNames...),
	}
}

func applyOwnProfileUpdate(input *agentInput, update ownProfileUpdate) {
	if update.Name != nil {
		input.Name = *update.Name
	}
	if update.Description != nil {
		input.Description = *update.Description
	}
	if update.Instructions != nil {
		input.Instructions = *update.Instructions
	}
	if update.Icon != nil {
		input.Icon = *update.Icon
	}
	if update.Color != nil {
		input.Color = *update.Color
	}
	if update.AvatarMode != nil {
		input.AvatarMode = *update.AvatarMode
	}
	if update.Model != nil {
		input.Model = *update.Model
	}
	if update.ReasoningEffortOverride != nil {
		if strings.TrimSpace(*update.ReasoningEffortOverride) == "" {
			input.ReasoningEffortOverride = nil
		} else {
			input.ReasoningEffortOverride = update.ReasoningEffortOverride
		}
	}
	if update.ContextWindowOverride != nil {
		if *update.ContextWindowOverride == 0 {
			input.ContextWindowOverride = nil
		} else {
			input.ContextWindowOverride = update.ContextWindowOverride
		}
	}
	if update.AutoCompactTokenLimitOverride != nil {
		if *update.AutoCompactTokenLimitOverride == 0 {
			input.AutoCompactTokenLimitOverride = nil
		} else {
			input.AutoCompactTokenLimitOverride = update.AutoCompactTokenLimitOverride
		}
	}
	if update.EffectiveContextWindowPercent != nil {
		input.EffectiveContextWindowPercent = *update.EffectiveContextWindowPercent
	}
	if update.SkillNames != nil {
		input.SkillNames = append([]string(nil), (*update.SkillNames)...)
	}
}

func validateSelectedSkills(manager *skills.Manager, names []string) error {
	installed := make(map[string]struct{})
	for _, skill := range manager.List() {
		installed[skill.Name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := installed[strings.TrimSpace(name)]; !ok {
			return fmt.Errorf("skill %q is not installed", name)
		}
	}
	return nil
}
