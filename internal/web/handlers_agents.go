package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type agentInput struct {
	ProviderID                    int64    `json:"provider_id"`
	Name                          string   `json:"name"`
	Description                   string   `json:"description"`
	Icon                          string   `json:"icon"`
	Color                         string   `json:"color"`
	AvatarMode                    string   `json:"avatar_mode"`
	Instructions                  string   `json:"instructions"`
	Model                         string   `json:"model"`
	ReasoningEffortOverride       *string  `json:"reasoning_effort_override"`
	ContextWindowOverride         *int     `json:"context_window_override"`
	AutoCompactTokenLimitOverride *int     `json:"auto_compact_token_limit_override"`
	EffectiveContextWindowPercent int      `json:"effective_context_window_percent"`
	SkillNames                    []string `json:"skill_names"`
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents(r.Context(), r.URL.Query().Get("archived") == "true")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	profile, err := s.store.GetAgent(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var input agentInput
	avatar, removeAvatar, ok := decodeAgentRequest(w, r, &input)
	if !ok {
		return
	}
	if removeAvatar {
		writeError(w, http.StatusBadRequest, "invalid_avatar", "a new agent has no avatar to remove")
		return
	}
	avatarModeSpecified := input.AvatarMode != ""
	write, err := normalizeAgentInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_agent", err.Error())
		return
	}
	if avatar != nil {
		write.AvatarObjectKey, err = storeAvatarImage(r.Context(), s.objects, *avatar)
		if err != nil {
			writeError(w, http.StatusBadGateway, "object_store_error", "could not store avatar")
			return
		}
		write.AvatarMIMEType = avatar.MIMEType
		if !avatarModeSpecified {
			write.AvatarMode = "image"
		}
	}
	if write.AvatarMode == "image" && write.AvatarObjectKey == "" {
		writeError(w, http.StatusBadRequest, "invalid_avatar", "avatar_mode image requires an uploaded avatar")
		return
	}
	profile, err := s.store.CreateAgent(r.Context(), write)
	if err != nil {
		if write.AvatarObjectKey != "" {
			_ = s.objects.Delete(r.Context(), write.AvatarObjectKey)
		}
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, profile)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	existing, err := s.store.GetAgent(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var input agentInput
	avatar, removeAvatar, ok := decodeAgentRequest(w, r, &input)
	if !ok {
		return
	}
	avatarModeSpecified := input.AvatarMode != ""
	if !avatarModeSpecified {
		input.AvatarMode = existing.AvatarMode
	}
	write, err := normalizeAgentInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_agent", err.Error())
		return
	}
	write.AvatarObjectKey = existing.AvatarObjectKey
	write.AvatarMIMEType = existing.AvatarMIMEType
	oldAvatarKey := ""
	newAvatarKey := ""
	if removeAvatar {
		oldAvatarKey = existing.AvatarObjectKey
		write.AvatarObjectKey = ""
		write.AvatarMIMEType = ""
		write.AvatarMode = "icon"
	}
	if avatar != nil {
		newAvatarKey, err = storeAvatarImage(r.Context(), s.objects, *avatar)
		if err != nil {
			writeError(w, http.StatusBadGateway, "object_store_error", "could not store avatar")
			return
		}
		oldAvatarKey = existing.AvatarObjectKey
		write.AvatarObjectKey = newAvatarKey
		write.AvatarMIMEType = avatar.MIMEType
		if !avatarModeSpecified {
			write.AvatarMode = "image"
		}
	}
	if write.AvatarMode == "image" && write.AvatarObjectKey == "" {
		if newAvatarKey != "" {
			_ = s.objects.Delete(r.Context(), newAvatarKey)
		}
		writeError(w, http.StatusBadRequest, "invalid_avatar", "avatar_mode image requires a stored avatar")
		return
	}
	profile, err := s.store.UpdateAgent(r.Context(), id, write)
	if err != nil {
		if newAvatarKey != "" {
			_ = s.objects.Delete(r.Context(), newAvatarKey)
		}
		writeStoreError(w, err)
		return
	}
	if oldAvatarKey != "" && oldAvatarKey != newAvatarKey {
		if err := s.objects.Delete(r.Context(), oldAvatarKey); err != nil {
			s.log.Warn("delete replaced agent avatar")
		}
	}
	s.runtimes.Invalidate(id)
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) handleGetAgentAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	profile, err := s.store.GetAgent(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if profile.AvatarObjectKey == "" {
		writeError(w, http.StatusNotFound, "not_found", "agent has no uploaded avatar")
		return
	}
	reader, err := s.objects.Open(r.Context(), profile.AvatarObjectKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "object_store_error", "could not read avatar")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", profile.AvatarMIMEType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, reader)
}

func (s *Server) handleArchiveAgent(w http.ResponseWriter, r *http.Request) {
	s.setAgentArchived(w, r, true)
}

func (s *Server) handleRestoreAgent(w http.ResponseWriter, r *http.Request) {
	s.setAgentArchived(w, r, false)
}

func (s *Server) setAgentArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	id, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := s.store.SetAgentArchived(r.Context(), id, archived); err != nil {
		writeStoreError(w, err)
		return
	}
	s.runtimes.Invalidate(id)
	writeJSON(w, http.StatusOK, map[string]any{"archived": archived})
}

func normalizeAgentInput(input agentInput) (AgentWrite, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Model = strings.TrimSpace(input.Model)
	if input.Name == "" || len([]rune(input.Name)) > 128 {
		return AgentWrite{}, errors.New("name is required and must be at most 128 characters")
	}
	if input.ProviderID <= 0 {
		return AgentWrite{}, errors.New("provider_id is required")
	}
	if input.Model == "" {
		return AgentWrite{}, errors.New("model is required")
	}
	if len([]rune(input.Model)) > 255 {
		return AgentWrite{}, errors.New("model must be at most 255 characters")
	}
	if len([]rune(input.Description)) > 4096 {
		return AgentWrite{}, errors.New("description must be at most 4096 characters")
	}
	if len(input.Instructions) > 1<<20 {
		return AgentWrite{}, errors.New("instructions must be at most 1 MiB")
	}
	if input.Icon == "" {
		input.Icon = "sparkles"
	}
	if !validAgentIcon(input.Icon) {
		return AgentWrite{}, errors.New("icon is not supported")
	}
	if input.Color == "" {
		input.Color = "#2563EB"
	}
	if !validHexColor(input.Color) {
		return AgentWrite{}, errors.New("color must be a six-digit hex color")
	}
	if input.AvatarMode == "" {
		input.AvatarMode = "icon"
	}
	if input.AvatarMode != "icon" && input.AvatarMode != "image" {
		return AgentWrite{}, errors.New("avatar_mode must be icon or image")
	}
	if input.EffectiveContextWindowPercent <= 0 {
		input.EffectiveContextWindowPercent = 95
	}
	if input.EffectiveContextWindowPercent > 100 {
		return AgentWrite{}, errors.New("effective_context_window_percent must be between 1 and 100")
	}
	if input.ContextWindowOverride != nil && *input.ContextWindowOverride <= 0 {
		return AgentWrite{}, errors.New("context_window_override must be positive or null")
	}
	if input.AutoCompactTokenLimitOverride != nil && *input.AutoCompactTokenLimitOverride <= 0 {
		return AgentWrite{}, errors.New("auto_compact_token_limit_override must be positive or null")
	}
	if input.ReasoningEffortOverride != nil {
		effort := strings.ToLower(strings.TrimSpace(*input.ReasoningEffortOverride))
		if effort == "" {
			input.ReasoningEffortOverride = nil
		} else {
			if !validReasoningEffort(effort) {
				return AgentWrite{}, errors.New("reasoning_effort_override contains unsupported characters")
			}
			input.ReasoningEffortOverride = &effort
		}
	}
	return AgentWrite{
		ProviderID: input.ProviderID, Name: input.Name, Description: strings.TrimSpace(input.Description), Icon: input.Icon, Color: input.Color,
		AvatarMode:   input.AvatarMode,
		Instructions: strings.TrimSpace(input.Instructions), Model: input.Model,
		ReasoningEffortOverride: input.ReasoningEffortOverride, ContextWindowOverride: input.ContextWindowOverride,
		AutoCompactTokenLimitOverride: input.AutoCompactTokenLimitOverride, EffectiveContextWindowPercent: input.EffectiveContextWindowPercent,
		SkillNames: input.SkillNames,
	}, nil
}

func validReasoningEffort(value string) bool {
	if len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.') {
			return false
		}
	}
	return value != ""
}

func decodeAgentRequest(w http.ResponseWriter, r *http.Request, input *agentInput) (*avatarImage, bool, bool) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		return nil, false, decodeJSON(w, r, input)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+(1<<20))
	if err := r.ParseMultipartForm(maxAvatarBytes + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "agent form is too large or malformed")
		return nil, false, false
	}
	profileJSON := r.FormValue("profile")
	decoder := json.NewDecoder(strings.NewReader(profileJSON))
	decoder.DisallowUnknownFields()
	if profileJSON == "" || decoder.Decode(input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "multipart field 'profile' must contain valid agent JSON")
		return nil, false, false
	}
	removeAvatar := r.FormValue("remove_avatar") == "true"
	file, _, err := r.FormFile("avatar")
	if errors.Is(err, http.ErrMissingFile) {
		return nil, removeAvatar, true
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_avatar", "could not read avatar upload")
		return nil, false, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_avatar", "could not read avatar upload")
		return nil, false, false
	}
	avatar, err := validateAvatarData(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_avatar", err.Error())
		return nil, false, false
	}
	return &avatar, removeAvatar, true
}

func validAgentIcon(icon string) bool {
	switch icon {
	case "sparkles", "bot", "search", "research", "code", "book", "chart", "terminal", "brain":
		return true
	default:
		return false
	}
}

func validHexColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, char := range color[1:] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}
