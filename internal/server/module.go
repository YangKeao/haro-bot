package server

import (
	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"go.uber.org/fx"
)

// Module provides HTTP and Telegram server.
var Module = fx.Module("server",
	fx.Provide(NewServerFromDeps),
)

// ServerParams contains dependencies for creating Server.
type ServerParams struct {
	fx.In

	Cfg          *config.Config
	Agent        *agent.Agent
	Store        memory.StoreAPI
	SkillsMgr    *skills.Manager
	MemoryEngine *memory.Engine
}

// NewServerFromDeps creates a server with dependencies.
func NewServerFromDeps(p ServerParams) *Server {
	return New(*p.Cfg, p.Agent, p.Store, p.SkillsMgr, p.MemoryEngine)
}
