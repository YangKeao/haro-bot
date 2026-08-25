//go:build integration

package skills_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
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

func TestManagerReconcilesAgentAssignmentsAfterRefreshAndDelete(t *testing.T) {
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
		SourceType: "git", URL: repoDir, Ref: "master", SkillFilters: []string{"alpha-skill"},
	})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh source: %v", err)
	}

	provider := dbmodel.Provider{Name: "provider", BaseURL: "https://example.test", PromptFormat: "openai"}
	if err := gdb.Create(&provider).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}
	oldUpdatedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	agent := dbmodel.Agent{
		ProviderID: provider.ID, Name: "agent", Icon: "sparkles", Color: "#2563EB", AvatarMode: "icon",
		Model: "model", EffectiveContextWindowPercent: 95, UpdatedAt: oldUpdatedAt,
	}
	if err := gdb.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := gdb.Create(&dbmodel.AgentSkill{AgentID: agent.ID, SkillName: "alpha-skill"}).Error; err != nil {
		t.Fatalf("assign alpha skill: %v", err)
	}
	if err := gdb.Create(&dbmodel.AgentSkill{AgentID: agent.ID, SkillName: "agent-browser"}).Error; err != nil {
		t.Fatalf("assign stale skill: %v", err)
	}
	mgr = skills.NewManager(store, baseDir, nil)
	var staleAssignments int64
	if err := gdb.Model(&dbmodel.AgentSkill{}).Where("agent_id = ? AND skill_name = ?", agent.ID, "agent-browser").Count(&staleAssignments).Error; err != nil {
		t.Fatalf("count stale assignments: %v", err)
	}
	if staleAssignments != 0 {
		t.Fatalf("expected startup reconciliation to remove stale assignment, got %d", staleAssignments)
	}

	if err := mgr.UpdateSource(ctx, sourceID, skills.Source{
		URL: repoDir, Ref: "master", SkillFilters: []string{"beta-skill"},
	}); err != nil {
		t.Fatalf("update source filters: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh filtered source: %v", err)
	}
	var alphaAssignments int64
	if err := gdb.Model(&dbmodel.AgentSkill{}).Where("agent_id = ? AND skill_name = ?", agent.ID, "alpha-skill").Count(&alphaAssignments).Error; err != nil {
		t.Fatalf("count alpha assignments: %v", err)
	}
	if alphaAssignments != 0 {
		t.Fatalf("expected removed alpha assignment, got %d", alphaAssignments)
	}
	var updatedAgent dbmodel.Agent
	if err := gdb.First(&updatedAgent, agent.ID).Error; err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if !updatedAgent.UpdatedAt.After(oldUpdatedAt) {
		t.Fatalf("expected runtime revision bump: old=%v new=%v", oldUpdatedAt, updatedAgent.UpdatedAt)
	}

	if err := gdb.Create(&dbmodel.AgentSkill{AgentID: agent.ID, SkillName: "beta-skill"}).Error; err != nil {
		t.Fatalf("assign beta skill: %v", err)
	}
	if err := mgr.DeleteSource(ctx, sourceID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	var betaAssignments int64
	if err := gdb.Model(&dbmodel.AgentSkill{}).Where("agent_id = ? AND skill_name = ?", agent.ID, "beta-skill").Count(&betaAssignments).Error; err != nil {
		t.Fatalf("count beta assignments: %v", err)
	}
	if betaAssignments != 0 {
		t.Fatalf("expected deleted-source assignment to be removed, got %d", betaAssignments)
	}
}

func TestManagerKeepsAssignmentWhenAnotherActiveSourceProvidesName(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := skills.NewStore(gdb)
	mgr := skills.NewManager(store, t.TempDir(), nil)
	ctx := context.Background()
	firstRepo := testutil.CreateSkillRepo(t, "shared-skill", "First shared skill")
	secondRepo := testutil.CreateSkillRepo(t, "shared-skill", "Second shared skill")
	firstID, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: firstRepo, Ref: "master"})
	if err != nil {
		t.Fatalf("register first source: %v", err)
	}
	secondID, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: secondRepo, Ref: "master"})
	if err != nil {
		t.Fatalf("register second source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, firstID); err != nil {
		t.Fatalf("refresh first source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, secondID); err != nil {
		t.Fatalf("refresh second source: %v", err)
	}

	provider := dbmodel.Provider{Name: "provider", BaseURL: "https://example.test", PromptFormat: "openai"}
	if err := gdb.Create(&provider).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agent := dbmodel.Agent{ProviderID: provider.ID, Name: "agent", Icon: "sparkles", Color: "#2563EB", AvatarMode: "icon", Model: "model", EffectiveContextWindowPercent: 95}
	if err := gdb.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := gdb.Create(&dbmodel.AgentSkill{AgentID: agent.ID, SkillName: "shared-skill"}).Error; err != nil {
		t.Fatalf("assign shared skill: %v", err)
	}
	if err := mgr.DeleteSource(ctx, firstID); err != nil {
		t.Fatalf("delete first source: %v", err)
	}
	var assignments int64
	if err := gdb.Model(&dbmodel.AgentSkill{}).Where("agent_id = ? AND skill_name = ?", agent.ID, "shared-skill").Count(&assignments).Error; err != nil {
		t.Fatalf("count assignments: %v", err)
	}
	if assignments != 1 {
		t.Fatalf("expected assignment preserved by second active source, got %d", assignments)
	}
}

func TestManagerUpdateSourceChangesRepositoryAndFilters(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	firstRepo := testutil.CreateSkillRepo(t, "alpha-skill", "Alpha skill")
	secondRepo := testutil.CreateSkillRepoWithSkills(t,
		testutil.SkillSpec{Name: "beta-skill", Description: "Beta skill"},
		testutil.SkillSpec{Name: "gamma-skill", Description: "Gamma skill"},
	)
	mgr := skills.NewManager(skills.NewStore(gdb), t.TempDir(), nil)
	ctx := context.Background()
	sourceID, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: firstRepo, Ref: "master"})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh first source: %v", err)
	}

	if err := mgr.UpdateSource(ctx, sourceID, skills.Source{
		URL:          secondRepo,
		Ref:          " master ",
		SkillFilters: []string{" beta-skill ", "beta-skill"},
	}); err != nil {
		t.Fatalf("update source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh updated source: %v", err)
	}

	updated, err := mgr.GetSource(ctx, sourceID)
	if err != nil {
		t.Fatalf("get updated source: %v", err)
	}
	if updated.ID != sourceID || updated.URL != secondRepo || updated.Ref != "master" || len(updated.SkillFilters) != 1 || updated.SkillFilters[0] != "beta-skill" {
		t.Fatalf("unexpected updated source: %#v", updated)
	}
	loaded := mgr.List()
	if len(loaded) != 1 || loaded[0].Name != "beta-skill" {
		t.Fatalf("expected only beta-skill after update, got %#v", loaded)
	}
}

func TestManagerUpdateSourceRejectsDeletedIdentityConflict(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	firstRepo := testutil.CreateSkillRepo(t, "alpha-skill", "Alpha skill")
	secondRepo := testutil.CreateSkillRepo(t, "beta-skill", "Beta skill")
	mgr := skills.NewManager(skills.NewStore(gdb), t.TempDir(), nil)
	ctx := context.Background()
	firstID, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: firstRepo, Ref: "master"})
	if err != nil {
		t.Fatalf("register first source: %v", err)
	}
	secondID, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: secondRepo, Ref: "master"})
	if err != nil {
		t.Fatalf("register second source: %v", err)
	}
	if err := mgr.DeleteSource(ctx, secondID); err != nil {
		t.Fatalf("delete second source: %v", err)
	}

	err = mgr.UpdateSource(ctx, firstID, skills.Source{URL: secondRepo, Ref: "master"})
	if !errors.Is(err, skills.ErrSourceConflict) {
		t.Fatalf("expected source conflict, got %v", err)
	}
}

func TestManagerRestoreSourceRefreshesDeletedSource(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	repoDir := testutil.CreateSkillRepo(t, "restored-skill", "Restored skill")
	mgr := skills.NewManager(skills.NewStore(gdb), t.TempDir(), nil)
	ctx := context.Background()
	sourceID, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: repoDir, Ref: "master"})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh source: %v", err)
	}
	if err := mgr.DeleteSource(ctx, sourceID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	if err := mgr.RestoreSource(ctx, sourceID); err != nil {
		t.Fatalf("restore source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh restored source: %v", err)
	}

	restored, err := mgr.GetSource(ctx, sourceID)
	if err != nil {
		t.Fatalf("get restored source: %v", err)
	}
	if restored.Status != "active" || len(mgr.List()) != 1 || mgr.List()[0].Name != "restored-skill" {
		t.Fatalf("unexpected restored source or skills: source=%#v skills=%#v", restored, mgr.List())
	}
}

func TestManagerUpdateSourceKeepsLastGoodSkillsWhenRefreshFails(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	repoDir := testutil.CreateSkillRepo(t, "stable-skill", "Stable skill")
	store := skills.NewStore(gdb)
	mgr := skills.NewManager(store, t.TempDir(), nil)
	ctx := context.Background()
	sourceID, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: repoDir, Ref: "master"})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh source: %v", err)
	}

	missingRepo := filepath.Join(t.TempDir(), "missing-repository")
	if err := mgr.UpdateSource(ctx, sourceID, skills.Source{URL: missingRepo, Ref: "master"}); err != nil {
		t.Fatalf("update source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err == nil {
		t.Fatal("expected refresh to fail")
	}
	updated, err := mgr.GetSource(ctx, sourceID)
	if err != nil {
		t.Fatalf("get failed source: %v", err)
	}
	if updated.URL != missingRepo || updated.LastError == "" {
		t.Fatalf("expected failed source configuration and error to persist, got %#v", updated)
	}
	if loaded := mgr.List(); len(loaded) != 1 || loaded[0].Name != "stable-skill" {
		t.Fatalf("expected last good in-memory skill, got %#v", loaded)
	}
	entries, err := store.ListSkillsBySource(ctx, sourceID)
	if err != nil || len(entries) != 1 || entries[0].Name != "stable-skill" {
		t.Fatalf("expected last good registry entry, entries=%#v err=%v", entries, err)
	}
}
