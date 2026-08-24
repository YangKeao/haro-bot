package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/YangKeao/haro-bot/internal/mcpmanager"
)

func (s *Server) requireMCP(w http.ResponseWriter) bool {
	if s.mcp == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP management is unavailable")
		return false
	}
	return true
}

func (s *Server) handleListMCPServers(w http.ResponseWriter, r *http.Request) {
	if !s.requireMCP(w) {
		return
	}
	servers, err := s.mcp.ListServers(r.Context(), r.URL.Query().Get("disabled") == "true")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func (s *Server) handleGetMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireMCP(w) {
		return
	}
	id, err := parseID(r, "serverID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	server, err := s.mcp.GetServer(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, server)
}

func (s *Server) handleCreateMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireMCP(w) {
		return
	}
	var input mcpmanager.ServerWrite
	if !decodeJSON(w, r, &input) {
		return
	}
	server, err := s.mcp.CreateServer(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_mcp_server", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, server)
}

func (s *Server) handleUpdateMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireMCP(w) {
		return
	}
	id, err := parseID(r, "serverID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var input mcpmanager.ServerWrite
	if !decodeJSON(w, r, &input) {
		return
	}
	server, err := s.mcp.UpdateServer(r.Context(), id, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_mcp_server", err.Error())
		return
	}
	s.runtimes.InvalidateAll()
	writeJSON(w, http.StatusOK, server)
}

func (s *Server) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireMCP(w) {
		return
	}
	id, err := parseID(r, "serverID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := s.mcp.DeleteServer(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.runtimes.InvalidateAll()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetMCPConnection(w http.ResponseWriter, r *http.Request) {
	if !s.requireMCP(w) {
		return
	}
	agentID, serverID, ok := parseAgentMCPIDs(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetAgent(r.Context(), agentID); err != nil {
		writeStoreError(w, err)
		return
	}
	connection, err := s.mcp.AgentConnection(r.Context(), agentID, serverID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, connection)
}

func (s *Server) handlePutMCPCredentials(w http.ResponseWriter, r *http.Request) {
	if !s.requireMCP(w) {
		return
	}
	agentID, serverID, ok := parseAgentMCPIDs(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetAgent(r.Context(), agentID); err != nil {
		writeStoreError(w, err)
		return
	}
	var input mcpmanager.AgentCredentialWrite
	if !decodeJSON(w, r, &input) {
		return
	}
	connection, err := s.mcp.SetAgentCredential(r.Context(), agentID, serverID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_mcp_credentials", err.Error())
		return
	}
	s.runtimes.Invalidate(agentID)
	writeJSON(w, http.StatusOK, connection)
}

func (s *Server) handleStartMCPOAuth(w http.ResponseWriter, r *http.Request) {
	if !s.requireMCP(w) {
		return
	}
	agentID, serverID, ok := parseAgentMCPIDs(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetAgent(r.Context(), agentID); err != nil {
		writeStoreError(w, err)
		return
	}
	result, err := s.mcp.StartOAuth(r.Context(), agentID, serverID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "mcp_oauth_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMCPOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP management is unavailable")
		return
	}
	if oauthErr := strings.TrimSpace(r.URL.Query().Get("error")); oauthErr != "" {
		writeError(w, http.StatusBadRequest, "mcp_oauth_denied", oauthErr+": "+r.URL.Query().Get("error_description"))
		return
	}
	result, err := s.mcp.CompleteOAuth(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"), r.URL.Query().Get("iss"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "mcp_oauth_error", err.Error())
		return
	}
	s.runtimes.Invalidate(result.AgentID)
	target := "/agents/" + strconv.FormatInt(result.AgentID, 10) + "/edit?mcp_oauth=connected#tools"
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func parseAgentMCPIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	agentID, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return 0, 0, false
	}
	serverID, err := parseID(r, "serverID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return 0, 0, false
	}
	return agentID, serverID, true
}
