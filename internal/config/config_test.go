package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFileNormalizesPromptFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`llm_prompt_format = "bogus-format"\n`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LLMPromptFormat != PromptFormatOpenAI {
		t.Fatalf("expected prompt format to normalize to openai, got: %v", cfg.LLMPromptFormat)
	}
}
