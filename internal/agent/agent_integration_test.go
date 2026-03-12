//go:build integration

package agent_test

import (
	"context"
	"testing"

	"github.com/YangKeao/haro-bot/internal/agent"
	agentdefaults "github.com/YangKeao/haro-bot/internal/agent/defaults"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

func TestAgentStoresAssistantResponse(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	guidelinesMgr := guidelines.NewManager(gdb)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, t.TempDir(), 4, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})
	agentSvc.SetMiddleware(agentdefaults.New(guidelinesMgr, store, nil, client, llm.ContextConfig{}, agentSvc.SessionStatusWriter()))

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "telegram", "3001")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err = agentSvc.Handle(ctx, userID, "chan-tool-loop", "Say hello in one short sentence.")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	sessionID, err := store.GetOrCreateSession(ctx, userID, "chan-tool-loop")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	var msgs []db.Message
	if err := gdb.Where("session_id = ?", sessionID).Order("id asc").Find(&msgs).Error; err != nil {
		t.Fatalf("load messages: %v", err)
	}
	foundAssistant := false
	for _, m := range msgs {
		if m.Role == "assistant" && m.Content != "" {
			foundAssistant = true
			break
		}
	}
	if !foundAssistant {
		t.Fatalf("expected assistant message in session")
	}
}
