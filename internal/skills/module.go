package skills

import (
	"github.com/YangKeao/haro-bot/internal/config"
	"go.uber.org/fx"
)

// Module provides skills management.
var Module = fx.Module("skills",
	fx.Provide(
		NewStore,
		NewManagerFromConfig,
	),
)

// NewManagerFromConfig creates a skills manager with config.
func NewManagerFromConfig(store *Store, cfg *config.Config) *Manager {
	return NewManager(store, cfg.SkillsDir, cfg.SkillsRepoAllowlist)
}
