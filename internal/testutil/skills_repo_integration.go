//go:build integration

package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func CreateSkillRepo(t *testing.T, name, description string) string {
	t.Helper()
	if strings.TrimSpace(name) == "" {
		t.Fatalf("skill name required")
	}
	if strings.TrimSpace(description) == "" {
		description = "Demo skill for tests"
	}
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	skillDir := filepath.Join(repoDir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	content := []byte(strings.TrimSpace(`
---
name: `+name+`
description: `+description+`
---
Use this skill only for testing.
`) + "\n")
	if err := os.WriteFile(skillFile, content, 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add(filepath.ToSlash(filepath.Join(name, "SKILL.md"))); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	}); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return repoDir
}
