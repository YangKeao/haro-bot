package server

import (
	"encoding/json"
	"net/http"

	"github.com/YangKeao/haro-bot/internal/skills"
)

type skillRegisterRequest struct {
	SourceType    string `json:"source_type"`
	InstallMethod string `json:"install_method"`
	URL           string `json:"url"`
	Ref           string `json:"ref"`
	Subdir        string `json:"subdir"`
	Status        string `json:"status"`
}

type skillRegisterResponse struct {
	ID int64 `json:"id"`
}

type skillRefreshRequest struct {
	SourceID int64 `json:"source_id"`
}

type skillRefreshResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleSkillRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.skills == nil {
		http.Error(w, "skills manager not configured", http.StatusInternalServerError)
		return
	}
	var req skillRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.SourceType == "" {
		req.SourceType = "git"
	}
	id, err := s.skills.RegisterSource(r.Context(), skills.Source{
		SourceType:    req.SourceType,
		InstallMethod: req.InstallMethod,
		URL:           req.URL,
		Ref:           req.Ref,
		Subdir:        req.Subdir,
		Status:        req.Status,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, skillRegisterResponse{ID: id})
}

func (s *Server) handleSkillRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.skills == nil {
		http.Error(w, "skills manager not configured", http.StatusInternalServerError)
		return
	}
	var req skillRefreshRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	var err error
	if req.SourceID > 0 {
		err = s.skills.RefreshSource(r.Context(), req.SourceID)
	} else {
		err = s.skills.RefreshAll(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, skillRefreshResponse{Status: "ok"})
}
