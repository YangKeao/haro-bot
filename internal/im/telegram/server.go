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
	resolver AgentResolver
	binding  BindingStore
	store    memory.StoreAPI
	telegram *bot.Bot
}

type AgentResolver interface {
	Resolve(context.Context, int64) (*agent.Agent, error)
}

type BindingStore interface {
	GetTelegramAgentID(context.Context) (*int64, error)
	BindSessionAgent(context.Context, int64, int64) error
}

func New(cfg config.Config, resolver AgentResolver, store memory.StoreAPI, binding BindingStore) *Server {
	return &Server{
		cfg: cfg, resolver: resolver, store: store, binding: binding,
	}
}

func (s *Server) Start(ctx context.Context) {
	s.StartTelegramPolling(ctx)
}
