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

type installSkillResult struct {
	SourceID int64 `json:"source_id"`
	Skills   []struct {
		Name string `json:"name"`
	} `json:"skills"`
}

func TestInstallSkillTool(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	repoDir := testutil.CreateSkillRepo(t, "demo-skill", "Demo skill for install tool")
	store := skills.NewStore(gdb)
	mgr := skills.NewManager(store, t.TempDir(), nil)
	tool := tools.NewInstallSkillTool(mgr)

	args, err := json.Marshal(map[string]any{
		"source_type": "git",
		"url":         repoDir,
		"ref":         "master",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	output, err := tool.Execute(context.Background(), tools.ToolContext{}, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var result installSkillResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.SourceID == 0 {
		t.Fatalf("expected source id")
	}
	found := false
	for _, s := range result.Skills {
		if s.Name == "demo-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected demo-skill in installed skills")
	}
}
