//go:build integration

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

func TestStoreAgentSessionLifecycle(t *testing.T) {
	database, cleanup := testutil.NewTestDBWithMigrations(t)
	defer cleanup()

	ctx := context.Background()
	store := NewStore(database)
	if err := database.Create(&dbmodel.User{ID: 42}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	provider, err := store.CreateProvider(ctx, ProviderWrite{Name: "Test provider", BaseURL: "https://example.com/v1", APIKey: "database-secret", PromptFormat: "openai"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	provider, err = store.SaveProviderCatalog(ctx, provider.ID, []ModelCapability{{ID: "example-model", ContextWindow: 128000, AutoCompactTokenLimit: 110000, DefaultReasoningEffort: "medium"}}, time.Now())
	if err != nil {
		t.Fatalf("save provider models: %v", err)
	}
	agent, err := store.CreateAgent(ctx, AgentWrite{
		ProviderID: provider.ID,
		Name:       "Research Atlas", Description: "Research specialist", Icon: "research", Color: "#7c3aed",
		Instructions: "Be precise.", Model: "example-model",
		EffectiveContextWindowPercent: 90, SkillNames: []string{"search", "search", "analysis"},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if agent.APIKey != "database-secret" || agent.ProviderName != "Test provider" || agent.ResolvedContextWindow != 128000 {
		t.Fatalf("stored API key state was not preserved")
	}
	if len(agent.SkillNames) != 2 || agent.SkillNames[0] != "analysis" || agent.SkillNames[1] != "search" {
		t.Fatalf("unexpected normalized skills: %#v", agent.SkillNames)
	}
	encoded, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal agent: %v", err)
	}
	if bytes.Contains(encoded, []byte("database-secret")) {
		t.Fatalf("agent API key leaked through JSON: %s", encoded)
	}
	if agent.AvatarMode != "icon" {
		t.Fatalf("expected default icon avatar mode, got %q", agent.AvatarMode)
	}

	otherAgent, err := store.CreateAgent(ctx, AgentWrite{
		ProviderID: provider.ID, Name: "Other agent", Icon: "bot",
		Model: "example-model", EffectiveContextWindowPercent: 90,
	})
	if err != nil {
		t.Fatalf("create other agent: %v", err)
	}
	if otherAgent.Color != "#2563EB" || otherAgent.AvatarMode != "icon" {
		t.Fatalf("unexpected default identity: color=%q avatar_mode=%q", otherAgent.Color, otherAgent.AvatarMode)
	}
	invalidated := false
	selfTool := &updateOwnProfileTool{agentID: agent.ID, store: store, invalidate: func() { invalidated = true }}
	if _, err := selfTool.Execute(ctx, tools.ToolContext{}, json.RawMessage(`{"name":"Renamed by self"}`)); err != nil {
		t.Fatalf("self-update agent: %v", err)
	}
	updatedAgent, err := store.GetAgent(ctx, agent.ID)
	if err != nil || updatedAgent.Name != "Renamed by self" || !invalidated {
		t.Fatalf("unexpected self-update result: %#v, invalidated=%v, err=%v", updatedAgent, invalidated, err)
	}
	chefPatch := json.RawMessage(`{
		"description":"Manages fridge inventory and nutrition.",
		"instructions":"Track ingredients and provide balanced meal advice.",
		"avatar_mode":"icon",
		"avatar_url":"",
		"remove_avatar_image":false
	}`)
	if _, err := selfTool.Execute(ctx, tools.ToolContext{}, chefPatch); err != nil {
		t.Fatalf("partial self-update with empty avatar fields: %v", err)
	}
	updatedAgent, err = store.GetAgent(ctx, agent.ID)
	if err != nil || updatedAgent.Description != "Manages fridge inventory and nutrition." ||
		updatedAgent.Instructions != "Track ingredients and provide balanced meal advice." ||
		updatedAgent.AvatarMode != "icon" || updatedAgent.AvatarObjectKey != "" {
		t.Fatalf("partial self-update unexpectedly changed avatar: %#v, err=%v", updatedAgent, err)
	}
	unchangedOther, err := store.GetAgent(ctx, otherAgent.ID)
	if err != nil || unchangedOther.Name != "Other agent" {
		t.Fatalf("self-update affected another agent: %#v, err=%v", unchangedOther, err)
	}

	session, err := store.CreateSession(ctx, 42, agent.ID, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.Title != "New session" || session.Channel == "" {
		t.Fatalf("unexpected new session: %#v", session)
	}
	if err := store.AutoTitleSession(ctx, 42, session.ID, `请你调查 tidb\_enable\_check\_constraint 的行为`); err != nil {
		t.Fatalf("auto-title session: %v", err)
	}
	autoTitled, err := store.GetSession(ctx, 42, session.ID)
	if err != nil {
		t.Fatalf("get auto-titled session: %v", err)
	}
	if autoTitled.Title != `请你调查 tidb\_enable\_check\_constraint 的行为` {
		t.Fatalf("CommonMark source was not preserved in session title: %q", autoTitled.Title)
	}
	var storedSession dbmodel.Session
	if err := database.First(&storedSession, session.ID).Error; err != nil {
		t.Fatalf("read stored session title: %v", err)
	}
	if storedSession.Title != autoTitled.Title {
		t.Fatalf("database title = %q, want %q", storedSession.Title, autoTitled.Title)
	}
	if _, err := store.GetSession(ctx, 7, session.ID); err == nil {
		t.Fatal("another user unexpectedly accessed the session")
	}

	attachment, err := store.CreateAttachment(ctx, 42, session.ID, "chart.png", "image/png", "attachments/chart.png", 128, strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	if _, err := store.GetAttachment(ctx, 7, attachment.ID); err == nil {
		t.Fatal("another user unexpectedly accessed the attachment")
	}
	published, err := store.CreateAttachmentForAgent(ctx, agent.ID, session.ID, "report.zip", "application/zip", "published-artifacts/report.zip", 256, strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("create agent-published attachment: %v", err)
	}
	if published.SessionID != session.ID || published.OriginalName != "report.zip" {
		t.Fatalf("unexpected agent-published attachment: %#v", published)
	}
	if _, err := store.CreateAttachmentForAgent(ctx, otherAgent.ID, session.ID, "forbidden.txt", "text/plain", "published-artifacts/forbidden.txt", 1, strings.Repeat("c", 64)); err == nil {
		t.Fatal("another agent unexpectedly published to the session")
	}

	if err := store.SetSessionArchived(ctx, 42, session.ID, true); err != nil {
		t.Fatalf("archive session: %v", err)
	}
	if _, err := store.CreateAttachmentForAgent(ctx, agent.ID, session.ID, "archived.txt", "text/plain", "published-artifacts/archived.txt", 1, strings.Repeat("d", 64)); err == nil {
		t.Fatal("agent unexpectedly published to an archived session")
	}
	active, err := store.ListSessions(ctx, 42, agent.ID, false)
	if err != nil || len(active) != 0 {
		t.Fatalf("active sessions after archive: %#v, %v", active, err)
	}
	archived, err := store.ListSessions(ctx, 42, agent.ID, true)
	if err != nil || len(archived) != 1 {
		t.Fatalf("archived sessions: %#v, %v", archived, err)
	}
	if err := store.SetSessionArchived(ctx, 42, session.ID, false); err != nil {
		t.Fatalf("restore session: %v", err)
	}

	if err := store.SetAgentArchived(ctx, agent.ID, true); err != nil {
		t.Fatalf("archive agent: %v", err)
	}
	if _, err := store.CreateSession(ctx, 42, agent.ID, "blocked"); err == nil {
		t.Fatal("created a session for an archived agent")
	}
	if err := store.SetAgentArchived(ctx, agent.ID, false); err != nil {
		t.Fatalf("restore agent: %v", err)
	}
	if _, err := store.CreateSession(ctx, 42, agent.ID, "restored"); err != nil {
		t.Fatalf("create session after restore: %v", err)
	}
}

func TestStoreProviderAndIntegrationArchiveGuards(t *testing.T) {
	database, cleanup := testutil.NewTestDBWithMigrations(t)
	defer cleanup()

	ctx := context.Background()
	store := NewStore(database)
	provider, err := store.CreateProvider(ctx, ProviderWrite{
		Name: "Shared provider", BaseURL: "https://example.com/v1", PromptFormat: "openai",
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agent, err := store.CreateAgent(ctx, AgentWrite{
		ProviderID: provider.ID, Name: "Telegram agent", Model: "example-model", EffectiveContextWindowPercent: 90,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if err := store.SetProviderArchived(ctx, provider.ID, true); !errors.Is(err, ErrProviderInUse) {
		t.Fatalf("archive active provider: got %v, want %v", err, ErrProviderInUse)
	}
	if err := store.SetTelegramAgentID(ctx, &agent.ID); err != nil {
		t.Fatalf("bind Telegram agent: %v", err)
	}
	bound, err := store.GetTelegramAgentID(ctx)
	if err != nil || bound == nil || *bound != agent.ID {
		t.Fatalf("unexpected Telegram binding: id=%v err=%v", bound, err)
	}
	if err := store.SetAgentArchived(ctx, agent.ID, true); !errors.Is(err, ErrAgentTelegramBound) {
		t.Fatalf("archive Telegram-bound agent: got %v, want %v", err, ErrAgentTelegramBound)
	}
	if err := store.SetTelegramAgentID(ctx, nil); err != nil {
		t.Fatalf("clear Telegram binding: %v", err)
	}
	if err := store.SetAgentArchived(ctx, agent.ID, true); err != nil {
		t.Fatalf("archive unbound agent: %v", err)
	}
	if err := store.SetProviderArchived(ctx, provider.ID, true); err != nil {
		t.Fatalf("archive unused provider: %v", err)
	}
	if err := store.SetAgentArchived(ctx, agent.ID, false); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("restore agent with archived provider: got %v, want %v", err, ErrProviderUnavailable)
	}
	if err := store.SetProviderArchived(ctx, provider.ID, false); err != nil {
		t.Fatalf("restore provider: %v", err)
	}
	if err := store.SetAgentArchived(ctx, agent.ID, false); err != nil {
		t.Fatalf("restore agent: %v", err)
	}
}

func TestStoreListRecentSessions(t *testing.T) {
	database, cleanup := testutil.NewTestDBWithMigrations(t)
	defer cleanup()

	ctx := context.Background()
	store := NewStore(database)
	for _, user := range []dbmodel.User{{ID: 42}, {ID: 43}} {
		if err := database.Create(&user).Error; err != nil {
			t.Fatalf("create user %d: %v", user.ID, err)
		}
	}
	provider, err := store.CreateProvider(ctx, ProviderWrite{Name: "Recent provider", BaseURL: "https://example.com/v1", PromptFormat: "openai"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	firstAgent, err := store.CreateAgent(ctx, AgentWrite{
		ProviderID: provider.ID, Name: "Research Atlas", Icon: "search", Color: "#557D78",
		AvatarMode: "image", AvatarObjectKey: "avatars/research.png", AvatarMIMEType: "image/png",
		Model: "example-model", EffectiveContextWindowPercent: 90,
	})
	if err != nil {
		t.Fatalf("create first agent: %v", err)
	}
	secondAgent, err := store.CreateAgent(ctx, AgentWrite{
		ProviderID: provider.ID, Name: "Code Partner", Icon: "code", Color: "#61768E",
		Model: "example-model", EffectiveContextWindowPercent: 90,
	})
	if err != nil {
		t.Fatalf("create second agent: %v", err)
	}
	hiddenAgent, err := store.CreateAgent(ctx, AgentWrite{
		ProviderID: provider.ID, Name: "Archived Agent", Model: "example-model", EffectiveContextWindowPercent: 90,
	})
	if err != nil {
		t.Fatalf("create hidden agent: %v", err)
	}

	older, err := store.CreateSession(ctx, 42, firstAgent.ID, "Older session")
	if err != nil {
		t.Fatalf("create older session: %v", err)
	}
	newer, err := store.CreateSession(ctx, 42, secondAgent.ID, "Newer session")
	if err != nil {
		t.Fatalf("create newer session: %v", err)
	}
	archived, err := store.CreateSession(ctx, 42, firstAgent.ID, "Archived session")
	if err != nil {
		t.Fatalf("create archived session: %v", err)
	}
	if err := store.SetSessionArchived(ctx, 42, archived.ID, true); err != nil {
		t.Fatalf("archive session: %v", err)
	}
	if _, err := store.CreateSession(ctx, 42, hiddenAgent.ID, "Hidden agent session"); err != nil {
		t.Fatalf("create hidden agent session: %v", err)
	}
	if err := store.SetAgentArchived(ctx, hiddenAgent.ID, true); err != nil {
		t.Fatalf("archive hidden agent: %v", err)
	}
	if _, err := store.CreateSession(ctx, 43, firstAgent.ID, "Another user's session"); err != nil {
		t.Fatalf("create another user session: %v", err)
	}

	base := time.Now().Add(-time.Hour)
	if err := database.Model(&dbmodel.Session{}).Where("id = ?", older.ID).UpdateColumn("updated_at", base).Error; err != nil {
		t.Fatalf("set older timestamp: %v", err)
	}
	if err := database.Model(&dbmodel.Session{}).Where("id = ?", newer.ID).UpdateColumn("updated_at", base.Add(30*time.Minute)).Error; err != nil {
		t.Fatalf("set newer timestamp: %v", err)
	}
	recent, err := store.ListRecentSessions(ctx, 42, 20)
	if err != nil {
		t.Fatalf("list recent sessions: %v", err)
	}
	if len(recent) != 2 || recent[0].ID != newer.ID || recent[1].ID != older.ID {
		t.Fatalf("unexpected recent sessions: %#v", recent)
	}
	if recent[1].Agent.Name != firstAgent.Name || recent[1].Agent.AvatarURL == "" {
		t.Fatalf("missing recent agent identity: %#v", recent[1].Agent)
	}
	limited, err := store.ListRecentSessions(ctx, 42, 1)
	if err != nil || len(limited) != 1 || limited[0].ID != newer.ID {
		t.Fatalf("unexpected limited recent sessions: %#v err=%v", limited, err)
	}
}
