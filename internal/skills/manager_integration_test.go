//go:build integration

package skills_test

import (
	"context"
	"testing"

	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestManagerLoadsSkillsFromDBOnStartup(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	baseDir := t.TempDir()
	repoDir := testutil.CreateSkillRepo(t, "demo-skill", "Demo skill for manager tests")
	store := skills.NewStore(gdb)
	ctx := context.Background()

	mgr1 := skills.NewManager(store, baseDir, nil)
	sourceID, err := mgr1.RegisterSource(ctx, skills.Source{
		SourceType: "git",
		URL:        repoDir,
		Ref:        "master",
	})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	if err := mgr1.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(mgr1.List()) == 0 {
		t.Fatalf("expected skills loaded after refresh")
	}

	mgr2 := skills.NewManager(store, baseDir, nil)
	if len(mgr2.List()) == 0 {
		t.Fatalf("expected skills loaded from db on startup")
	}
}
