package telegram

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	"github.com/go-telegram/bot"
)

type Server struct {
	cfg      config.Config
	agent    *agent.Agent
	store    memory.StoreAPI
	skills   *skills.Manager
	telegram *bot.Bot

	telegramSessions *telegramSessionRegistry
	toolApprovals    *toolApprovalManager
	auditModel       *auditModel
	memoryEngine     *memory.Engine
}

func New(cfg config.Config, agent *agent.Agent, store memory.StoreAPI, skillsMgr *skills.Manager, memoryEngine *memory.Engine) *Server {
	return &Server{
		cfg:              cfg,
		agent:            agent,
		store:            store,
		skills:           skillsMgr,
		telegramSessions: newTelegramSessionRegistry(),
		toolApprovals:    newToolApprovalManager(),
		memoryEngine:     memoryEngine,
	}
}

func (s *Server) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.StartTelegramPolling(ctx)
}

func (s *Server) SessionMessenger() agent.SessionMessenger {
	if s == nil {
		return nil
	}
	return s
}

func (s *Server) Approver() tools.Approver {
	if s == nil {
		return nil
	}
	return s
}
