//go:build integration

package config

import (
	"context"
	"testing"

	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestLoadFromDBNormalizesPromptFormat(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	rec := defaultRecord()
	rec.LLMPromptFormat = "bogus-format"
	if err := saveRecord(ctx, gdb, rec); err != nil {
		t.Fatalf("save record: %v", err)
	}

	cfg, err := LoadFromDB(ctx, gdb, LoadBase())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LLMPromptFormat != PromptFormatOpenAI {
		t.Fatalf("expected prompt format to normalize to openai, got: %v", cfg.LLMPromptFormat)
	}
}
