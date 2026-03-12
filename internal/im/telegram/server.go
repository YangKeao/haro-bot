package telegram

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/go-telegram/bot"
)

type Server struct {
	cfg      config.Config
	agent    *agent.Agent
	store    memory.StoreAPI
	telegram *bot.Bot
}

func New(cfg config.Config, agent *agent.Agent, store memory.StoreAPI) *Server {
	return &Server{
		cfg:   cfg,
		agent: agent,
		store: store,
	}
}

func (s *Server) Start(ctx context.Context) {
	s.StartTelegramPolling(ctx)
}
