//go:build integration

package testutil

import (
	"os"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

type LLMSettings struct {
	BaseURL string
	APIKey  string
	Model   string
}

func LLMSettingsFromEnv(t *testing.T) LLMSettings {
	t.Helper()
	EnsureIntegrationEnv(t)

	baseURL := os.Getenv("LLM_BASE_URL")
	apiKey := os.Getenv("LLM_API_KEY")
	model := os.Getenv("LLM_MODEL")
	if baseURL == "" {
		t.Fatalf("LLM_BASE_URL required for integration tests")
	}
	if apiKey == "" {
		t.Fatalf("LLM_API_KEY required for integration tests")
	}
	if model == "" {
		t.Fatalf("LLM_MODEL required for integration tests")
	}
	return LLMSettings{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
	}
}

func NewLLMClientFromEnv(t *testing.T) (*llm.OpenAIChatModel, string) {
	t.Helper()
	settings := LLMSettingsFromEnv(t)
	return llm.NewOpenAIChatModel(settings.BaseURL, settings.APIKey), settings.Model
}
