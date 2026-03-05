package server

import (
	"net/http"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
)

type Server struct {
	cfg      config.Config
	agent    *agent.Agent
	store    *memory.Store
	skills   *skills.Manager
	telegram *TelegramClient
}

func New(cfg config.Config, agent *agent.Agent, store *memory.Store, skillsMgr *skills.Manager) *Server {
	var telegram *TelegramClient
	if cfg.TelegramToken != "" {
		telegram = NewTelegramClient(cfg.TelegramToken)
	}
	return &Server{
		cfg:      cfg,
		agent:    agent,
		store:    store,
		skills:   skillsMgr,
		telegram: telegram,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/skills/register", s.handleSkillRegister)
	mux.HandleFunc("/skills/refresh", s.handleSkillRefresh)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
