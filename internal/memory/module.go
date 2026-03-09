package memory

import (
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/llm"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// Module provides memory storage and engine.
var Module = fx.Module("memory",
	fx.Provide(
		NewStore,
		NewEngineFromConfig,
	),
)

// NewEngineFromConfig creates a memory engine with config.
func NewEngineFromConfig(db *gorm.DB, store StoreAPI, llmClient *llm.Client, cfg *config.Config) (*Engine, error) {
	return NewEngine(db, store, llmClient, cfg.LLMModel, cfg.Memory)
}
