//go:build integration

package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/config"
)

// EnsureIntegrationEnv loads integration settings from config.toml when env vars are missing.
// Env vars already set by the caller are preserved.
func EnsureIntegrationEnv(t *testing.T) config.Config {
	t.Helper()

	cfg, ok := loadIntegrationConfig(t)
	if !ok {
		return config.Config{}
	}

	if os.Getenv("TIDB_DSN") == "" && strings.TrimSpace(cfg.TiDBDSN) != "" {
		t.Setenv("TIDB_DSN", cfg.TiDBDSN)
	}
	if os.Getenv("LLM_BASE_URL") == "" && strings.TrimSpace(cfg.LLMBaseURL) != "" {
		t.Setenv("LLM_BASE_URL", cfg.LLMBaseURL)
	}
	if os.Getenv("LLM_API_KEY") == "" && strings.TrimSpace(cfg.LLMAPIKey) != "" {
		t.Setenv("LLM_API_KEY", cfg.LLMAPIKey)
	}
	if os.Getenv("LLM_MODEL") == "" && strings.TrimSpace(cfg.LLMModel) != "" {
		t.Setenv("LLM_MODEL", cfg.LLMModel)
	}
	return cfg
}

func loadIntegrationConfig(t *testing.T) (config.Config, bool) {
	t.Helper()

	path := strings.TrimSpace(os.Getenv("HARO_CONFIG_PATH"))
	if path == "" {
		path = findConfigPath("config.toml")
		if path == "" {
			return config.Config{}, false
		}
	} else {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("HARO_CONFIG_PATH=%q not readable: %v", path, err)
		}
	}

	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load integration config %q: %v", path, err)
	}
	return cfg, true
}

func findConfigPath(name string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(cwd, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}
	return ""
}
