package llm

import (
	"github.com/YangKeao/haro-bot/internal/config"
	"go.uber.org/fx"
)

// Module provides LLM client and config.
var Module = fx.Module("llm",
	fx.Provide(
		NewClientFromConfig,
		NewContextConfigFromConfig,
		NewReasoningConfigFromConfig,
	),
)

// NewClientFromConfig creates an LLM client with config.
func NewClientFromConfig(cfg *config.Config) *Client {
	return NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, WithHTTPDebug(cfg.LLMHTTPDebug))
}

// NewContextConfigFromConfig creates context config from config.
func NewContextConfigFromConfig(cfg *config.Config) ContextConfig {
	return ContextConfig{
		WindowTokens:                  cfg.LLMContextWindow,
		AutoCompactTokenLimit:         cfg.LLMAutoCompactTokenLimit,
		EffectiveContextWindowPercent: cfg.LLMEffectiveContextWindowPercent,
	}
}

// NewReasoningConfigFromConfig creates reasoning config from config.
func NewReasoningConfigFromConfig(cfg *config.Config) ReasoningConfig {
	return ReasoningConfig{Enabled: cfg.LLMReasoningEnabled, Effort: cfg.LLMReasoningEffort}
}
