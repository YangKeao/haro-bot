package agent

import (
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
)

func TestBuildSystemPromptIncludesSkillsAndMemories(t *testing.T) {
	memories := []memory.MemoryItem{
		{Type: "note", Content: "remember this"},
	}
	skillsList := []skills.Metadata{
		{Name: "demo", Description: "demo skill", Dir: "/tmp/demo"},
	}
	out := buildSystemPrompt(memories, skillsList, "openai")
	if !strings.Contains(out, "Long-term memory:") {
		t.Fatalf("expected memory section, got: %s", out)
	}
	if !strings.Contains(out, "remember this") {
		t.Fatalf("expected memory content, got: %s", out)
	}
	if !strings.Contains(out, "## Skills") {
		t.Fatalf("expected skills section, got: %s", out)
	}
	if !strings.Contains(out, "demo skill") {
		t.Fatalf("expected skill description, got: %s", out)
	}
	if !strings.Contains(out, "activate_skill") {
		t.Fatalf("expected activate_skill instruction, got: %s", out)
	}
}

func TestBuildSystemPromptClaudeXML(t *testing.T) {
	skillsList := []skills.Metadata{
		{Name: "demo", Description: "demo skill", Dir: "/tmp/demo"},
	}
	out := buildSystemPrompt(nil, skillsList, "claude")
	if !strings.HasPrefix(out, "<available_skills>") {
		t.Fatalf("expected XML prefix, got: %s", out)
	}
	if !strings.Contains(out, "<name>demo</name>") {
		t.Fatalf("expected skill XML, got: %s", out)
	}
	if !strings.Contains(out, "activate_skill") {
		t.Fatalf("expected activate_skill instruction, got: %s", out)
	}
}

func TestBuildInterruptPromptNoSkills(t *testing.T) {
	memories := []memory.MemoryItem{
		{Type: "note", Content: "remember this"},
	}
	out := buildInterruptPrompt(memories, "openai")
	if !strings.Contains(out, "Long-term memory:") {
		t.Fatalf("expected memory section, got: %s", out)
	}
	if !strings.Contains(out, "Do not use tools or skills") {
		t.Fatalf("expected no-tools instruction, got: %s", out)
	}
	if strings.Contains(out, "activate_skill") || strings.Contains(out, "## Skills") {
		t.Fatalf("did not expect skills section, got: %s", out)
	}
}
