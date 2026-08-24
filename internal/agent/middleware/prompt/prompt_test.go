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
	skillsList := []skills.Metadata{{Name: "demo", Description: "demo skill", Hash: strings.Repeat("a", 64)}}
	out := buildPrompt(ctx, nil, skillsList, "openai")
	if !strings.Contains(out, "## Skills") {
		t.Fatalf("expected skills section, got: %s", out)
	}
	if !strings.Contains(out, "demo skill") {
		t.Fatalf("expected skill description, got: %s", out)
	}
	if !strings.Contains(out, "read_skill") || !strings.Contains(out, "skill://demo/"+strings.Repeat("a", 64)) {
		t.Fatalf("expected read_skill package instruction, got: %s", out)
	}
	if strings.Contains(out, "/tmp/demo") {
		t.Fatalf("host path leaked into prompt: %s", out)
	}
}

func TestBuildSystemPromptClaudeXML(t *testing.T) {
	ctx := context.Background()
	skillsList := []skills.Metadata{{Name: "demo", Description: "demo skill", Hash: strings.Repeat("b", 64)}}
	out := buildPrompt(ctx, nil, skillsList, "claude")
	if !strings.HasPrefix(out, "<available_skills>") {
		t.Fatalf("expected XML prefix, got: %s", out)
	}
	if !strings.Contains(out, "<name>demo</name>") {
		t.Fatalf("expected skill XML, got: %s", out)
	}
	if !strings.Contains(out, "read_skill") || !strings.Contains(out, "<package>skill://demo/"+strings.Repeat("b", 64)+"</package>") {
		t.Fatalf("expected read_skill package instruction, got: %s", out)
	}
}

func TestBuildSystemPromptExplicitlyDeniesUnassignedSkills(t *testing.T) {
	for _, format := range []string{"openai", "claude"} {
		t.Run(format, func(t *testing.T) {
			out := buildPrompt(context.Background(), nil, nil, format)
			if !strings.Contains(out, "No skills are assigned to this agent") {
				t.Fatalf("expected explicit empty skill assignment, got: %s", out)
			}
			if !strings.Contains(out, "do not list workspace-installed skills") {
				t.Fatalf("expected global skill-list restriction, got: %s", out)
			}
			if strings.Contains(out, "To use a skill") {
				t.Fatalf("unexpected skill activation instructions: %s", out)
			}
		})
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
