package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/mcpmanager"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/skills"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Server struct {
	cfg                     config.WebConfig
	store                   *Store
	conversation            memory.StoreAPI
	objects                 *ObjectStore
	runtimes                *RuntimeRegistry
	guidelines              *guidelines.Manager
	skills                  *skills.Manager
	sandboxes               *sandbox.Service
	mcp                     *mcpmanager.Manager
	userID                  int64
	telegramTokenConfigured bool
	assetsDir               string
	log                     *zap.Logger
	auth                    *authenticator
	runMu                   sync.Mutex
	runs                    map[int64]context.CancelFunc
}

type ServerDeps struct {
	Config                  config.WebConfig
	Store                   *Store
	Conversation            memory.StoreAPI
	Objects                 *ObjectStore
	Runtimes                *RuntimeRegistry
	Guidelines              *guidelines.Manager
	Skills                  *skills.Manager
	Sandboxes               *sandbox.Service
	MCP                     *mcpmanager.Manager
	UserID                  int64
	Logger                  *zap.Logger
	TelegramTokenConfigured bool
}

func NewServer(ctx context.Context, deps ServerDeps) (*Server, error) {
	if strings.TrimSpace(deps.Config.AccessToken) == "" {
		return nil, errors.New("web.access_token is required")
	}
	if deps.Store == nil || deps.Conversation == nil || deps.Objects == nil || deps.Runtimes == nil || deps.Skills == nil || deps.Guidelines == nil {
		return nil, errors.New("web server dependencies are incomplete")
	}
	assetsDir, err := filepath.Abs(deps.Config.AssetsDir)
	if err != nil {
		return nil, fmt.Errorf("resolve web assets: %w", err)
	}
	if stat, err := os.Stat(filepath.Join(assetsDir, "index.html")); err != nil || stat.IsDir() {
		return nil, fmt.Errorf("web assets missing index.html in %s", assetsDir)
	}
	if err := deps.Objects.EnsureBucket(ctx, deps.Config.ObjectStorage.Region); err != nil {
		return nil, err
	}
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}
	return &Server{
		cfg: deps.Config, store: deps.Store, conversation: deps.Conversation, objects: deps.Objects,
		runtimes: deps.Runtimes, guidelines: deps.Guidelines, skills: deps.Skills, sandboxes: deps.Sandboxes, mcp: deps.MCP, userID: deps.UserID,
		assetsDir: assetsDir, log: log.Named("web"), auth: newAuthenticator(deps.Config.AccessToken, deps.Config.CookieSecure),
		telegramTokenConfigured: deps.TelegramTokenConfigured,
		runs:                    make(map[int64]context.CancelFunc),
	}, nil
}

func (s *Server) beginRun(parent context.Context, sessionID int64) (context.Context, func(), bool) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if _, exists := s.runs[sessionID]; exists {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	s.runs[sessionID] = cancel
	return ctx, func() {
		s.runMu.Lock()
		delete(s.runs, sessionID)
		s.runMu.Unlock()
		cancel()
	}, true
}

func (s *Server) cancelRun(sessionID int64) bool {
	s.runMu.Lock()
	cancel := s.runs[sessionID]
	s.runMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/login", s.auth.login)
	mux.HandleFunc("GET /api/v1/mcp/oauth/callback", s.handleMCPOAuthCallback)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/auth/session", s.handleAuthSession)
	api.HandleFunc("POST /api/v1/auth/logout", s.auth.logout)

	api.HandleFunc("GET /api/v1/agents", s.handleListAgents)
	api.HandleFunc("POST /api/v1/agents", s.handleCreateAgent)
	api.HandleFunc("GET /api/v1/agents/{agentID}", s.handleGetAgent)
	api.HandleFunc("PUT /api/v1/agents/{agentID}", s.handleUpdateAgent)
	api.HandleFunc("GET /api/v1/agents/{agentID}/avatar", s.handleGetAgentAvatar)
	api.HandleFunc("POST /api/v1/agents/{agentID}/archive", s.handleArchiveAgent)
	api.HandleFunc("POST /api/v1/agents/{agentID}/restore", s.handleRestoreAgent)
	api.HandleFunc("GET /api/v1/agents/{agentID}/environment", s.handleGetAgentEnvironment)
	api.HandleFunc("PUT /api/v1/agents/{agentID}/environment", s.handleUpdateAgentEnvironment)
	api.HandleFunc("GET /api/v1/agents/{agentID}/mcp-servers/{serverID}/connection", s.handleGetMCPConnection)
	api.HandleFunc("PUT /api/v1/agents/{agentID}/mcp-servers/{serverID}/credentials", s.handlePutMCPCredentials)
	api.HandleFunc("POST /api/v1/agents/{agentID}/mcp-servers/{serverID}/oauth/start", s.handleStartMCPOAuth)

	api.HandleFunc("GET /api/v1/mcp-servers", s.handleListMCPServers)
	api.HandleFunc("POST /api/v1/mcp-servers", s.handleCreateMCPServer)
	api.HandleFunc("GET /api/v1/mcp-servers/{serverID}", s.handleGetMCPServer)
	api.HandleFunc("PUT /api/v1/mcp-servers/{serverID}", s.handleUpdateMCPServer)
	api.HandleFunc("DELETE /api/v1/mcp-servers/{serverID}", s.handleDeleteMCPServer)

	api.HandleFunc("GET /api/v1/sandboxes", s.handleListSandboxes)
	api.HandleFunc("GET /api/v1/sandboxes/events", s.handleSandboxEvents)
	api.HandleFunc("POST /api/v1/sandboxes", s.handleCreateSandbox)
	api.HandleFunc("GET /api/v1/sandboxes/{sandboxID}", s.handleGetSandbox)
	api.HandleFunc("GET /api/v1/sandboxes/{sandboxID}/terminal", s.handleSandboxTerminal)
	api.HandleFunc("PUT /api/v1/sandboxes/{sandboxID}", s.handleUpdateSandbox)
	api.HandleFunc("DELETE /api/v1/sandboxes/{sandboxID}", s.handleDeleteSandbox)
	api.HandleFunc("POST /api/v1/sandboxes/{sandboxID}/apply", s.handleApplySandbox)
	api.HandleFunc("POST /api/v1/sandboxes/{sandboxID}/restart", s.handleRestartSandbox)
	api.HandleFunc("POST /api/v1/sandboxes/{sandboxID}/start", s.handleStartSandbox)
	api.HandleFunc("POST /api/v1/sandboxes/{sandboxID}/pause", s.handlePauseSandbox)
	api.HandleFunc("POST /api/v1/sandboxes/{sandboxID}/reset-workspace", s.handleResetSandboxWorkspace)

	api.HandleFunc("GET /api/v1/providers", s.handleListProviders)
	api.HandleFunc("POST /api/v1/providers", s.handleCreateProvider)
	api.HandleFunc("GET /api/v1/providers/{providerID}", s.handleGetProvider)
	api.HandleFunc("PUT /api/v1/providers/{providerID}", s.handleUpdateProvider)
	api.HandleFunc("POST /api/v1/providers/{providerID}/archive", s.handleArchiveProvider)
	api.HandleFunc("POST /api/v1/providers/{providerID}/restore", s.handleRestoreProvider)
	api.HandleFunc("GET /api/v1/providers/{providerID}/models", s.handleGetProviderModels)
	api.HandleFunc("POST /api/v1/providers/{providerID}/models/refresh", s.handleRefreshProviderModels)
	api.HandleFunc("GET /api/v1/integrations/telegram", s.handleGetTelegramIntegration)
	api.HandleFunc("PUT /api/v1/integrations/telegram", s.handleUpdateTelegramIntegration)

	api.HandleFunc("GET /api/v1/agents/{agentID}/sessions", s.handleListSessions)
	api.HandleFunc("POST /api/v1/agents/{agentID}/sessions", s.handleCreateSession)
	api.HandleFunc("GET /api/v1/sessions/recent", s.handleListRecentSessions)
	api.HandleFunc("GET /api/v1/sessions/{sessionID}", s.handleGetSession)
	api.HandleFunc("PATCH /api/v1/sessions/{sessionID}", s.handleRenameSession)
	api.HandleFunc("POST /api/v1/sessions/{sessionID}/archive", s.handleArchiveSession)
	api.HandleFunc("POST /api/v1/sessions/{sessionID}/restore", s.handleRestoreSession)
	api.HandleFunc("GET /api/v1/sessions/{sessionID}/messages", s.handleListMessages)
	api.HandleFunc("POST /api/v1/sessions/{sessionID}/runs", s.handleRun)
	api.HandleFunc("POST /api/v1/sessions/{sessionID}/cancel", s.handleCancelRun)
	api.HandleFunc("GET /api/v1/sessions/{sessionID}/processes", s.handleListSessionProcesses)
	api.HandleFunc("GET /api/v1/processes/{processID}", s.handleGetProcess)
	api.HandleFunc("GET /api/v1/processes/{processID}/logs", s.handleGetProcess)
	api.HandleFunc("POST /api/v1/processes/{processID}/stdin", s.handleProcessStdin)
	api.HandleFunc("POST /api/v1/processes/{processID}/signal", s.handleProcessSignal)

	api.HandleFunc("POST /api/v1/sessions/{sessionID}/attachments", s.handleUploadAttachment)
	api.HandleFunc("GET /api/v1/attachments/{attachmentID}", s.handleGetAttachment)
	api.HandleFunc("DELETE /api/v1/attachments/{attachmentID}", s.handleDeleteAttachment)

	api.HandleFunc("GET /api/v1/guidelines", s.handleGetGuidelines)
	api.HandleFunc("PUT /api/v1/guidelines", s.handleUpdateGuidelines)
	api.HandleFunc("GET /api/v1/guidelines/history", s.handleGuidelinesHistory)
	api.HandleFunc("GET /api/v1/skills", s.handleListSkills)
	api.HandleFunc("GET /api/v1/skill-sources", s.handleListSkillSources)
	api.HandleFunc("POST /api/v1/skill-sources", s.handleCreateSkillSource)
	api.HandleFunc("PUT /api/v1/skill-sources/{sourceID}", s.handleUpdateSkillSource)
	api.HandleFunc("POST /api/v1/skill-sources/{sourceID}/refresh", s.handleRefreshSkillSource)
	api.HandleFunc("POST /api/v1/skill-sources/{sourceID}/restore", s.handleRestoreSkillSource)
	api.HandleFunc("DELETE /api/v1/skill-sources/{sourceID}", s.handleDeleteSkillSource)

	mux.Handle("/api/v1/", s.auth.require(api))
	mux.HandleFunc("/", s.handleSPA)
}

func (s *Server) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			s.cleanupOrphans(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Server) cleanupOrphans(ctx context.Context) {
	attachments, err := s.store.OrphanAttachments(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		s.log.Warn("list orphan attachments", zap.Error(err))
		return
	}
	for _, attachment := range attachments {
		if err := s.objects.Delete(ctx, attachment.ObjectKey); err != nil {
			s.log.Warn("delete orphan object", zap.String("attachment_id", attachment.ID), zap.Error(err))
			continue
		}
		if _, err := s.store.DeletePendingAttachment(ctx, s.userID, attachment.ID); err != nil {
			s.log.Warn("mark orphan attachment deleted", zap.String("attachment_id", attachment.ID), zap.Error(err))
		}
	}
}

func (s *Server) handleAuthSession(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "." || clean == "" {
		clean = "index.html"
	}
	if strings.HasPrefix(clean, "api/") || strings.HasPrefix(clean, "debug/") {
		http.NotFound(w, r)
		return
	}
	assets := os.DirFS(s.assetsDir)
	if info, err := fs.Stat(assets, clean); err == nil && !info.IsDir() {
		http.FileServer(http.Dir(s.assetsDir)).ServeHTTP(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.assetsDir, "index.html"))
}

func parseID(r *http.Request, key string) (int64, error) {
	value, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New("invalid id")
	}
	return value, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if errors.Is(err, ErrProviderInUse) || errors.Is(err, ErrAgentTelegramBound) || errors.Is(err, ErrProviderUnavailable) {
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "request failed")
}

func validateHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
