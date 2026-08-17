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
	t.Setenv("HARO_SANDBOX_BACKGROUND_TERMINAL_MAX_TIMEOUT_MS", "420000")
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
	if cfg.Sandbox.BackgroundTerminalMaxTimeoutMS != 420000 {
		t.Fatalf("unexpected background terminal maximum: %d", cfg.Sandbox.BackgroundTerminalMaxTimeoutMS)
	}
}

func TestLoadFromFileUsesWebEnvironmentOverrides(t *testing.T) {
	t.Setenv("HARO_SERVER_ADDR", ":6060")
	t.Setenv("HARO_WEB_ACCESS_TOKEN", "web-token")
	t.Setenv("HARO_WEB_OBJECT_STORAGE_ENDPOINT", "seaweedfs-s3.seaweedfs.svc:8333")
	t.Setenv("HARO_WEB_OBJECT_STORAGE_REGION", "us-east-1")
	t.Setenv("HARO_WEB_OBJECT_STORAGE_BUCKET", "haro-bot")
	t.Setenv("HARO_WEB_OBJECT_STORAGE_ACCESS_KEY", "access-key")
	t.Setenv("HARO_WEB_OBJECT_STORAGE_SECRET_KEY", "secret-key")
	t.Setenv("HARO_WEB_OBJECT_STORAGE_USE_SSL", "false")
	t.Setenv("HARO_WEB_OBJECT_STORAGE_FORCE_PATH_STYLE", "true")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[web.object_storage]\nuse_ssl = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerAddr != ":6060" || cfg.Web.AccessToken != "web-token" {
		t.Fatalf("unexpected web environment overrides: %#v", cfg.Web)
	}
	want := ObjectStorageConfig{
		Endpoint:       "seaweedfs-s3.seaweedfs.svc:8333",
		Region:         "us-east-1",
		Bucket:         "haro-bot",
		AccessKey:      "access-key",
		SecretKey:      "secret-key",
		UseSSL:         false,
		ForcePathStyle: true,
	}
	if cfg.Web.ObjectStorage != want {
		t.Fatalf("unexpected object storage environment overrides: got %#v want %#v", cfg.Web.ObjectStorage, want)
	}
}
