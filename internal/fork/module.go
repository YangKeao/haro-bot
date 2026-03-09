package fork

import (
	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/memory"
	"go.uber.org/fx"
)

// Module provides fork management.
var Module = fx.Module("fork",
	fx.Provide(NewManagerFromDeps),
)

// ManagerParams contains dependencies for creating Manager.
type ManagerParams struct {
	fx.In

	Agent *agent.Agent
	Store memory.StoreAPI
}

// NewManagerFromDeps creates a fork manager with dependencies.
func NewManagerFromDeps(p ManagerParams) *Manager {
	return NewManager(p.Agent, p.Store)
}
