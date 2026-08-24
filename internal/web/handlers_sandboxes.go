package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"gorm.io/gorm"
)

func (s *Server) handleListSandboxes(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	items, err := s.sandboxes.List(r.Context())
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sandboxes": items, "config": sandboxPublicConfig(s.sandboxes)})
}

func (s *Server) handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	id, err := parseID(r, "sandboxID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	item, err := s.sandboxes.Get(r.Context(), id)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sandbox": item, "config": sandboxPublicConfig(s.sandboxes)})
}

func (s *Server) handleCreateSandbox(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	var input sandbox.Write
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.sandboxes.Create(r.Context(), input)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	for _, agentID := range item.AgentIDs {
		s.runtimes.Invalidate(agentID)
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateSandbox(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	id, err := parseID(r, "sandboxID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	before, err := s.sandboxes.Get(r.Context(), id)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	var input sandbox.Write
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.sandboxes.Update(r.Context(), id, input)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	seen := map[int64]struct{}{}
	for _, agentID := range append(before.AgentIDs, item.AgentIDs...) {
		if _, ok := seen[agentID]; !ok {
			s.runtimes.Invalidate(agentID)
			seen[agentID] = struct{}{}
		}
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleApplySandbox(w http.ResponseWriter, r *http.Request) {
	s.withSandboxIDStatus(w, r, http.StatusAccepted, func(id int64) (sandbox.Profile, error) { return s.sandboxes.Apply(r.Context(), id) })
}

func (s *Server) handleRestartSandbox(w http.ResponseWriter, r *http.Request) {
	s.withSandboxIDStatus(w, r, http.StatusAccepted, func(id int64) (sandbox.Profile, error) { return s.sandboxes.Restart(r.Context(), id) })
}

func (s *Server) handleStartSandbox(w http.ResponseWriter, r *http.Request) {
	s.withSandboxIDStatus(w, r, http.StatusAccepted, func(id int64) (sandbox.Profile, error) {
		return s.sandboxes.SetOperatingMode(r.Context(), id, sandbox.StateRunning)
	})
}

func (s *Server) handlePauseSandbox(w http.ResponseWriter, r *http.Request) {
	s.withSandboxIDStatus(w, r, http.StatusAccepted, func(id int64) (sandbox.Profile, error) {
		return s.sandboxes.SetOperatingMode(r.Context(), id, sandbox.StateSuspended)
	})
}

func (s *Server) handleResetSandboxWorkspace(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	id, err := parseID(r, "sandboxID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	item, err := s.sandboxes.Get(r.Context(), id)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	var input struct {
		ConfirmName string `json:"confirm_name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.ConfirmName != item.Name {
		writeError(w, http.StatusBadRequest, "confirmation_required", "confirm_name must exactly match the sandbox name")
		return
	}
	if err := s.sandboxes.ResetWorkspace(r.Context(), id); err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": true})
}

func (s *Server) handleDeleteSandbox(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	id, err := parseID(r, "sandboxID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	item, err := s.sandboxes.Get(r.Context(), id)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	var input struct {
		ConfirmName string `json:"confirm_name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.ConfirmName != item.Name {
		writeError(w, http.StatusBadRequest, "confirmation_required", "confirm_name must exactly match the sandbox name")
		return
	}
	if err := s.sandboxes.Delete(r.Context(), id); err != nil {
		writeSandboxError(w, err)
		return
	}
	for _, agentID := range item.AgentIDs {
		s.runtimes.Invalidate(agentID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetAgentEnvironment(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	agentID, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if _, err := s.store.GetAgent(r.Context(), agentID); err != nil {
		writeStoreError(w, err)
		return
	}
	variables, err := s.sandboxes.ListAgentEnvironment(r.Context(), agentID)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variables": variables})
}

func (s *Server) handleUpdateAgentEnvironment(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	agentID, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if _, err := s.store.GetAgent(r.Context(), agentID); err != nil {
		writeStoreError(w, err)
		return
	}
	var input struct {
		Variables []sandbox.EnvironmentWrite `json:"variables"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	variables, err := s.sandboxes.ReplaceAgentEnvironment(r.Context(), agentID, input.Variables)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variables": variables})
}

func (s *Server) handleListSessionProcesses(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	sessionID, err := parseID(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if _, err := s.store.GetSession(r.Context(), s.userID, sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	processes, err := s.sandboxes.ListProcessesForSession(r.Context(), sessionID)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"processes": processes})
}

func (s *Server) handleGetProcess(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	processID := strings.TrimSpace(r.PathValue("processID"))
	agentID, err := s.sandboxes.ProcessAgentID(r.Context(), processID)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	process, err := s.sandboxes.GetProcess(r.Context(), agentID, processID)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, process)
}

func (s *Server) handleProcessStdin(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	processID := strings.TrimSpace(r.PathValue("processID"))
	agentID, err := s.sandboxes.ProcessAgentID(r.Context(), processID)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	var input sandbox.StdinRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	process, err := s.sandboxes.WriteProcessStdin(r.Context(), agentID, processID, input)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, process)
}

func (s *Server) handleProcessSignal(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	processID := strings.TrimSpace(r.PathValue("processID"))
	agentID, err := s.sandboxes.ProcessAgentID(r.Context(), processID)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	var input sandbox.SignalRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	process, err := s.sandboxes.SignalProcess(r.Context(), agentID, processID, input.Signal)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, process)
}

func (s *Server) withSandboxID(w http.ResponseWriter, r *http.Request, action func(int64) (sandbox.Profile, error)) {
	s.withSandboxIDStatus(w, r, http.StatusOK, action)
}

func (s *Server) withSandboxIDStatus(w http.ResponseWriter, r *http.Request, status int, action func(int64) (sandbox.Profile, error)) {
	if !s.requireSandboxes(w) {
		return
	}
	id, err := parseID(r, "sandboxID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	item, err := action(id)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, status, item)
}

func (s *Server) requireSandboxes(w http.ResponseWriter) bool {
	if s.sandboxes == nil || !s.sandboxes.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "sandbox_disabled", "sandbox support is disabled or unavailable")
		return false
	}
	return true
}

func writeSandboxError(w http.ResponseWriter, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if errors.Is(err, sandbox.ErrOperationInProgress) {
		writeError(w, http.StatusConflict, "operation_in_progress", err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, "sandbox_error", err.Error())
}

func sandboxPublicConfig(service *sandbox.Service) map[string]any {
	cfg := service.Config()
	return map[string]any{
		"default_image": cfg.DefaultImage,
		"defaults":      map[string]int{"cpu_limit_millis": cfg.DefaultCPULimitMillis, "memory_limit_mib": cfg.DefaultMemoryLimitMiB, "ephemeral_storage_mib": cfg.DefaultEphemeralStorageMiB, "workspace_storage_mib": cfg.DefaultWorkspaceStorageMiB},
		"maximums":      map[string]int{"cpu_limit_millis": cfg.MaxCPULimitMillis, "memory_limit_mib": cfg.MaxMemoryLimitMiB, "ephemeral_storage_mib": cfg.MaxEphemeralStorageMiB, "workspace_storage_mib": cfg.MaxWorkspaceStorageMiB, "running": cfg.MaxRunning},
	}
}
