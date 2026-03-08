//go:build integration

package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestTiDBVectorStoreRoundTripWithConfig(t *testing.T) {
	cfg := loadConfig(t)
	if cfg.TiDBDSN == "" {
		t.Skip("tidb dsn missing in config")
	}
	if cfg.Memory.Embedder.Dimensions <= 0 {
		t.Skip("memory embedder dimensions not set")
	}
	prev := os.Getenv("TIDB_DSN")
	_ = os.Setenv("TIDB_DSN", cfg.TiDBDSN)
	t.Cleanup(func() {
		_ = os.Setenv("TIDB_DSN", prev)
	})

	gdb, cleanup := testutil.NewTestDBWithMigrationsConfig(t, cfg.Memory)
	t.Cleanup(cleanup)

	store := memory.NewTiDBVectorStore(gdb, cfg.Memory.Vector.Distance)
	if err := store.EnsureSchema(context.Background(), cfg.Memory); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	userID := time.Now().UnixNano()
	vectorA := make([]float32, cfg.Memory.Embedder.Dimensions)
	vectorA[0] = 1
	vectorB := make([]float32, cfg.Memory.Embedder.Dimensions)
	if cfg.Memory.Embedder.Dimensions > 1 {
		vectorB[1] = 1
	} else {
		vectorB[0] = 0.5
	}

	idA, err := store.Insert(context.Background(), memory.MemoryItem{
		UserID:  userID,
		Type:    "note",
		Content: "alpha",
	}, vectorA)
	if err != nil {
		t.Fatalf("insert a: %v", err)
	}
	idB, err := store.Insert(context.Background(), memory.MemoryItem{
		UserID:  userID,
		Type:    "note",
		Content: "beta",
	}, vectorB)
	if err != nil {
		t.Fatalf("insert b: %v", err)
	}
	defer func() {
		_ = store.Delete(context.Background(), idA)
		_ = store.Delete(context.Background(), idB)
	}()

	results, err := store.Search(context.Background(), userID, nil, vectorA, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected results")
	}
	if results[0].Content != "alpha" {
		t.Fatalf("expected alpha, got %q", results[0].Content)
	}
}

func loadConfig(t *testing.T) config.Config {
	path := os.Getenv("HARO_CONFIG_PATH")
	if path == "" {
		path = findConfigPath("config.toml")
	}
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func findConfigPath(name string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return name
	}
	for i := 0; i < 6; i++ {
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
	return name
}
