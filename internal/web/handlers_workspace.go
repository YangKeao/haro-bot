package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/skills"
	"gorm.io/gorm"
)

type skillSourceInput struct {
	URL          string   `json:"url"`
	Ref          string   `json:"ref"`
	Subdir       string   `json:"subdir"`
	SkillFilters []string `json:"skill_filters"`
}

func guidelineJSON(item *guidelines.Guidelines) any {
	if item == nil {
		return nil
	}
	return map[string]any{
		"id": item.ID, "content": item.Content, "version": item.Version,
		"is_active": item.IsActive, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
	}
}

func (s *Server) handleGetGuidelines(w http.ResponseWriter, r *http.Request) {
	current, err := s.guidelines.GetActive(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"guidelines": guidelineJSON(current)})
}

func (s *Server) handleUpdateGuidelines(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	updated, err := s.guidelines.Update(r.Context(), input.Content)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_guidelines", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"guidelines": guidelineJSON(updated)})
}

func (s *Server) handleGuidelinesHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.guidelines.GetAll(r.Context(), limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, guidelineJSON(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

func (s *Server) handleListSkills(w http.ResponseWriter, _ *http.Request) {
	items := s.skills.List()
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"name": item.Name, "description": item.Description, "version": item.Version, "hash": item.Hash})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

func (s *Server) handleListSkillSources(w http.ResponseWriter, r *http.Request) {
	items, err := s.skills.ListSources(r.Context(), r.URL.Query().Get("archived") == "true")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, skillSourceJSON(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out})
}

func (s *Server) handleCreateSkillSource(w http.ResponseWriter, r *http.Request) {
	var input skillSourceInput
	if !decodeJSON(w, r, &input) {
		return
	}
	id, err := s.skills.RegisterSource(r.Context(), sourceFromSkillSourceInput(input))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_skill_source", err.Error())
		return
	}
	if err := s.skills.RefreshSource(r.Context(), id); err != nil {
		writeError(w, http.StatusBadGateway, "skill_refresh_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateSkillSource(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "sourceID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var input skillSourceInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.skills.UpdateSource(r.Context(), id, sourceFromSkillSourceInput(input)); err != nil {
		writeSkillSourceChangeError(w, err)
		return
	}
	if err := s.skills.RefreshSource(r.Context(), id); err != nil {
		writeError(w, http.StatusBadGateway, "skill_refresh_failed", err.Error())
		return
	}
	updated, err := s.skills.GetSource(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": skillSourceJSON(updated)})
}

func (s *Server) handleRefreshSkillSource(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "sourceID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := s.skills.RefreshSource(r.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeStoreError(w, err)
			return
		}
		if errors.Is(err, skills.ErrSourceNotActive) {
			writeError(w, http.StatusConflict, "skill_source_not_active", err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "skill_refresh_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refreshed": true})
}

func (s *Server) handleRestoreSkillSource(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "sourceID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := s.skills.RestoreSource(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.skills.RefreshSource(r.Context(), id); err != nil {
		writeError(w, http.StatusBadGateway, "skill_refresh_failed", err.Error())
		return
	}
	restored, err := s.skills.GetSource(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": skillSourceJSON(restored)})
}

func (s *Server) handleDeleteSkillSource(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "sourceID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := s.skills.DeleteSource(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func skillSourceJSON(item skills.Source) map[string]any {
	filters := item.SkillFilters
	if filters == nil {
		filters = []string{}
	}
	return map[string]any{
		"id": item.ID, "source_type": item.SourceType, "install_method": item.InstallMethod,
		"url": item.URL, "ref": item.Ref, "subdir": item.Subdir, "skill_filters": filters,
		"status": item.Status, "version": item.Version, "last_sync_at": item.LastSyncAt, "last_error": item.LastError,
	}
}

func sourceFromSkillSourceInput(input skillSourceInput) skills.Source {
	return skills.Source{
		SourceType: "git", InstallMethod: "git", URL: strings.TrimSpace(input.URL), Ref: input.Ref,
		Subdir: input.Subdir, SkillFilters: input.SkillFilters, Status: "active",
	}
}

func writeSkillSourceChangeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		writeStoreError(w, err)
	case errors.Is(err, skills.ErrSourceConflict):
		writeError(w, http.StatusConflict, "skill_source_conflict", err.Error())
	case errors.Is(err, skills.ErrSourceNotActive):
		writeError(w, http.StatusConflict, "skill_source_not_active", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "invalid_skill_source", err.Error())
	}
}
