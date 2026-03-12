package telegram

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/go-telegram/bot"
)

type Server struct {
	cfg      config.Config
	agent    *agent.Agent
	store    memory.StoreAPI
	skills   *skills.Manager
	telegram *bot.Bot

	telegramSessions *telegramSessionRegistry
}

func New(cfg config.Config, agent *agent.Agent, store memory.StoreAPI, skillsMgr *skills.Manager) *Server {
	return &Server{
		cfg:              cfg,
		agent:            agent,
		store:            store,
		skills:           skillsMgr,
		telegramSessions: newTelegramSessionRegistry(),
	}
}

func (s *Server) Start(ctx context.Context) {
	s.StartTelegramPolling(ctx)
}

func (s *Server) SessionMessenger() agent.SessionMessenger {
	return s
}
