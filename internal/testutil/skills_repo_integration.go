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

type SkillSpec struct {
	Name        string
	Description string
}

func CreateSkillRepo(t *testing.T, name, description string) string {
	t.Helper()
	return CreateSkillRepoWithSkills(t, SkillSpec{
		Name:        name,
		Description: description,
	})
}

func CreateSkillRepoWithSkills(t *testing.T, specs ...SkillSpec) string {
	t.Helper()
	if len(specs) == 0 {
		t.Fatalf("at least one skill spec required")
	}
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	for _, spec := range specs {
		writeSkillFile(t, repoDir, spec.Name, spec.Description)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := commitRepo(repo, "init"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return repoDir
}

func AddSkillToRepo(t *testing.T, repoDir, name, description string) {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	writeSkillFile(t, repoDir, name, description)
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := commitRepo(repo, "add "+name); err != nil {
		t.Fatalf("git commit: %v", err)
	}
}

func RemoveSkillFromRepo(t *testing.T, repoDir, name string) {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Remove(filepath.ToSlash(filepath.Join(name, "SKILL.md"))); err != nil && !os.IsNotExist(err) {
		t.Fatalf("git remove: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(repoDir, name)); err != nil {
		t.Fatalf("remove skill: %v", err)
	}
	if _, err := wt.Add("."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := commitRepo(repo, "remove "+name); err != nil {
		t.Fatalf("git commit: %v", err)
	}
}

func writeSkillFile(t *testing.T, repoDir, name, description string) {
	t.Helper()
	if strings.TrimSpace(name) == "" {
		t.Fatalf("skill name required")
	}
	if strings.TrimSpace(description) == "" {
		description = "Demo skill for tests"
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
}

func commitRepo(repo *git.Repository, message string) (string, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	hash, err := wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", err
	}
	return hash.String(), nil
}
