package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFileReadsLLMHTTPDebugFromLogConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`[log]
llm_http_debug = true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.LLMHTTPDebug {
		t.Fatal("expected log.llm_http_debug to enable LLM HTTP debug logging")
	}
}
