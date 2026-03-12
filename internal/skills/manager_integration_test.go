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

func TestManagerRefreshSourceReconcilesFiltersAndRemovals(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	baseDir := t.TempDir()
	repoDir := testutil.CreateSkillRepoWithSkills(t,
		testutil.SkillSpec{Name: "alpha-skill", Description: "Alpha skill"},
		testutil.SkillSpec{Name: "beta-skill", Description: "Beta skill"},
	)
	store := skills.NewStore(gdb)
	mgr := skills.NewManager(store, baseDir, nil)
	ctx := context.Background()

	sourceID, err := mgr.RegisterSource(ctx, skills.Source{
		SourceType:   "git",
		URL:          repoDir,
		Ref:          "master",
		SkillFilters: []string{"alpha-skill"},
	})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh source: %v", err)
	}
	if got := mgr.List(); len(got) != 1 || got[0].Name != "alpha-skill" {
		t.Fatalf("expected only alpha-skill after filtered refresh, got %#v", got)
	}

	if _, err := mgr.RegisterSource(ctx, skills.Source{
		SourceType:   "git",
		URL:          repoDir,
		Ref:          "master",
		SkillFilters: []string{"beta-skill"},
	}); err != nil {
		t.Fatalf("update source filters: %v", err)
	}
	testutil.RemoveSkillFromRepo(t, repoDir, "alpha-skill")
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh source after repo change: %v", err)
	}

	got := mgr.List()
	if len(got) != 1 || got[0].Name != "beta-skill" {
		t.Fatalf("expected only beta-skill after reconciliation, got %#v", got)
	}
	entries, err := store.ListSkillsBySource(ctx, sourceID)
	if err != nil {
		t.Fatalf("list skills by source: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "beta-skill" {
		t.Fatalf("expected registry to contain only beta-skill, got %#v", entries)
	}
}

func TestManagerDeleteSourceRemovesSkills(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	baseDir := t.TempDir()
	repoDir := testutil.CreateSkillRepo(t, "demo-skill", "Demo skill")
	store := skills.NewStore(gdb)
	mgr := skills.NewManager(store, baseDir, nil)
	ctx := context.Background()

	sourceID, err := mgr.RegisterSource(ctx, skills.Source{
		SourceType: "git",
		URL:        repoDir,
		Ref:        "master",
	})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh source: %v", err)
	}
	if err := mgr.DeleteSource(ctx, sourceID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	if got := mgr.List(); len(got) != 0 {
		t.Fatalf("expected no loaded skills after delete, got %#v", got)
	}
	sources, err := mgr.ListSources(ctx, true)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 || sources[0].Status != "deleted" {
		t.Fatalf("expected deleted source status, got %#v", sources)
	}
	entries, err := store.ListSkillsBySource(ctx, sourceID)
	if err != nil {
		t.Fatalf("list skills by source: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected source registry entries removed, got %#v", entries)
	}
}
