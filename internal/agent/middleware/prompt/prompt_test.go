package prompt

import (
	"context"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/skills"
)

func TestBuildSystemPromptIncludesSkills(t *testing.T) {
	ctx := context.Background()
	skillsList := []skills.Metadata{{Name: "demo", Description: "demo skill", Dir: "/tmp/demo"}}
	out := buildPrompt(ctx, nil, skillsList, "openai")
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
	ctx := context.Background()
	skillsList := []skills.Metadata{{Name: "demo", Description: "demo skill", Dir: "/tmp/demo"}}
	out := buildPrompt(ctx, nil, skillsList, "claude")
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

func TestMiddlewareWithTypedNilGuidelinesLoader(t *testing.T) {
	var gl *guidelines.Manager
	mw := New(gl)
	run := &agent.RunState{PromptFormat: "openai"}
	out, err := mw.HandleRun(context.Background(), run, func(_ context.Context, run *agent.RunState) (string, error) {
		return run.Prompt, nil
	})
	if err != nil {
		t.Fatalf("execute prompt middleware: %v", err)
	}
	if !strings.Contains(out, "You are an assistant.") {
		t.Fatalf("unexpected prompt output: %s", out)
	}
}
