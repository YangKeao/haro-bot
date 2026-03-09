package llm

import (
	"github.com/YangKeao/haro-bot/internal/config"
	"gorm.io/gorm"
)

// NewLLMClient creates the appropriate LLM client based on configuration.
// If CodexOAuth is enabled, it returns a Codex client, otherwise a standard OpenAI client.
func NewLLMClient(cfg config.Config, db *gorm.DB) ChatClient {
	// If Codex OAuth is enabled, use Codex client
	if cfg.CodexOAuth.Enabled {
		oauthManager := NewCodexOAuthManager(OAuthConfig{
			Enabled:     cfg.CodexOAuth.Enabled,
			AutoRefresh: cfg.CodexOAuth.AutoRefresh,
		}, db)
		model := cfg.CodexOAuth.Model
		if model == "" {
			model = cfg.LLMModel // Fall back to LLM model setting
		}
		return NewCodexClient(oauthManager, model)
	}

	// Otherwise use standard OpenAI client
	return NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, WithHTTPDebug(cfg.LLMHTTPDebug))
}
