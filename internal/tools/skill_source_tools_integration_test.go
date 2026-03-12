//go:build integration

package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

type listSkillSourcesOutput struct {
	Sources []struct {
		ID            int64    `json:"id"`
		IncludeSkills []string `json:"include_skills"`
		Skills        []struct {
			Name string `json:"name"`
		} `json:"skills"`
	} `json:"sources"`
}

type refreshSkillsOutput struct {
	Scope      string `json:"scope"`
	SourceID   int64  `json:"source_id"`
	SkillCount int    `json:"skill_count"`
}

type deleteSkillSourceOutput struct {
	SourceID int64 `json:"source_id"`
	Deleted  bool  `json:"deleted"`
}

func TestSkillSourceTools(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	repoDir := testutil.CreateSkillRepoWithSkills(t,
		testutil.SkillSpec{Name: "alpha-skill", Description: "Alpha skill"},
		testutil.SkillSpec{Name: "beta-skill", Description: "Beta skill"},
	)
	store := skills.NewStore(gdb)
	mgr := skills.NewManager(store, t.TempDir(), nil)
	installTool := tools.NewInstallSkillTool(mgr)
	listTool := tools.NewListSkillSourcesTool(mgr)
	refreshTool := tools.NewRefreshSkillsTool(mgr)
	deleteTool := tools.NewDeleteSkillSourceTool(mgr)

	installArgs, err := json.Marshal(map[string]any{
		"source_type":    "git",
		"url":            repoDir,
		"ref":            "master",
		"include_skills": []string{"alpha-skill"},
	})
	if err != nil {
		t.Fatalf("marshal install args: %v", err)
	}
	installRaw, err := installTool.Execute(context.Background(), tools.ToolContext{}, installArgs)
	if err != nil {
		t.Fatalf("install source: %v", err)
	}
	var installResult installSkillResult
	if err := json.Unmarshal([]byte(installRaw), &installResult); err != nil {
		t.Fatalf("unmarshal install result: %v", err)
	}
	if installResult.SourceID == 0 {
		t.Fatalf("expected source id")
	}

	listRaw, err := listTool.Execute(context.Background(), tools.ToolContext{}, nil)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	var listed listSkillSourcesOutput
	if err := json.Unmarshal([]byte(listRaw), &listed); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listed.Sources) != 1 {
		t.Fatalf("expected one source, got %#v", listed.Sources)
	}
	if len(listed.Sources[0].IncludeSkills) != 1 || listed.Sources[0].IncludeSkills[0] != "alpha-skill" {
		t.Fatalf("expected include_skills to be persisted, got %#v", listed.Sources[0].IncludeSkills)
	}
	if len(listed.Sources[0].Skills) != 1 || listed.Sources[0].Skills[0].Name != "alpha-skill" {
		t.Fatalf("expected only alpha-skill to be listed, got %#v", listed.Sources[0].Skills)
	}

	refreshArgs, err := json.Marshal(map[string]any{"source_id": installResult.SourceID})
	if err != nil {
		t.Fatalf("marshal refresh args: %v", err)
	}
	refreshRaw, err := refreshTool.Execute(context.Background(), tools.ToolContext{}, refreshArgs)
	if err != nil {
		t.Fatalf("refresh source: %v", err)
	}
	var refreshed refreshSkillsOutput
	if err := json.Unmarshal([]byte(refreshRaw), &refreshed); err != nil {
		t.Fatalf("unmarshal refresh result: %v", err)
	}
	if refreshed.Scope != "source" || refreshed.SourceID != installResult.SourceID || refreshed.SkillCount != 1 {
		t.Fatalf("unexpected refresh result: %#v", refreshed)
	}

	deleteArgs, err := json.Marshal(map[string]any{"source_id": installResult.SourceID})
	if err != nil {
		t.Fatalf("marshal delete args: %v", err)
	}
	deleteRaw, err := deleteTool.Execute(context.Background(), tools.ToolContext{}, deleteArgs)
	if err != nil {
		t.Fatalf("delete source: %v", err)
	}
	var deleted deleteSkillSourceOutput
	if err := json.Unmarshal([]byte(deleteRaw), &deleted); err != nil {
		t.Fatalf("unmarshal delete result: %v", err)
	}
	if !deleted.Deleted || deleted.SourceID != installResult.SourceID {
		t.Fatalf("unexpected delete result: %#v", deleted)
	}
	if got := mgr.List(); len(got) != 0 {
		t.Fatalf("expected no loaded skills after delete, got %#v", got)
	}
}
