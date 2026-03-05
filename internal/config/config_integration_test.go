//go:build integration

package config_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/db"
	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestLoadFromDBSeedsConfig(t *testing.T) {
	t.Setenv("LLM_MODEL", "test-model")
	t.Setenv("SKILLS_SYNC_INTERVAL", "5m")

	gdb, cleanup := testutil.NewTestDB(t)
	t.Cleanup(cleanup)

	if err := db.ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	base := config.LoadBase()
	ctx := context.Background()
	cfg, err := config.LoadFromDB(ctx, gdb, base)
	if err != nil {
		t.Fatalf("load from db: %v", err)
	}
	if cfg.LLMModel != "test-model" {
		t.Fatalf("expected LLMModel to be overridden, got %q", cfg.LLMModel)
	}
	if cfg.SkillsSyncInterval.String() != "5m0s" {
		t.Fatalf("unexpected sync interval: %s", cfg.SkillsSyncInterval)
	}

	var row dbmodel.AppConfig
	if err := gdb.First(&row).Error; err != nil {
		t.Fatalf("app_config not saved: %v", err)
	}
	if len(row.ConfigJSON) == 0 {
		t.Fatalf("config_json empty")
	}
	var rec map[string]any
	if err := json.Unmarshal(row.ConfigJSON, &rec); err != nil {
		t.Fatalf("decode config_json: %v", err)
	}
	if rec["llm_model"] != "test-model" {
		t.Fatalf("config_json llm_model missing or wrong: %v", rec["llm_model"])
	}
}
