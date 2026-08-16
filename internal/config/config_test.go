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

func TestLoadFromFileUsesSandboxDefaultsAndEnvironmentKey(t *testing.T) {
	t.Setenv("HARO_SANDBOX_SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("HARO_SANDBOX_ENABLED", "true")
	t.Setenv("HARO_SANDBOX_STORAGE_CLASS", "ssd-nobackup")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[sandbox]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Sandbox.Enabled || cfg.Sandbox.Namespace != "haro-sandboxes" || cfg.Sandbox.RuntimeClass != "gvisor" {
		t.Fatalf("unexpected sandbox defaults: %#v", cfg.Sandbox)
	}
	if cfg.Sandbox.StorageClass != "ssd-nobackup" {
		t.Fatalf("sandbox storage class override was not applied: %#v", cfg.Sandbox)
	}
	if cfg.Sandbox.EncryptionKey != "0123456789abcdef0123456789abcdef" {
		t.Fatal("sandbox encryption key was not read from the environment")
	}
}
