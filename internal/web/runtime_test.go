package web

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/tools"
)

func TestAgentToolRegistryExcludesSkillSourceManagement(t *testing.T) {
	registry := tools.NewRegistry(
		tools.NewListSkillSourcesTool(nil),
		tools.NewInstallSkillTool(nil),
		tools.NewRefreshSkillsTool(nil),
		tools.NewDeleteSkillSourceTool(nil),
		tools.NewSessionSummaryTool(nil),
	)

	scoped := newAgentToolRegistry(registry)
	for _, name := range []string{"list_skill_sources", "install_skill", "refresh_skills", "delete_skill_source"} {
		if _, ok := scoped.Get(name); ok {
			t.Fatalf("agent registry unexpectedly contains %q", name)
		}
	}
	if _, ok := scoped.Get("session_summary"); !ok {
		t.Fatal("agent registry lost an ordinary runtime tool")
	}
}
