//go:build integration

package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	agentdefaults "github.com/YangKeao/haro-bot/internal/agent/defaults"
	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
	"gorm.io/gorm"
)

func TestE2EContextCompactKeepsCurrentUserMessageInView(t *testing.T) {
	store, gdb, agentSvc, userID := newCompactTestRig(t, 9201)
	ctx := context.Background()

	sessionID, err := store.GetOrCreateSession(ctx, userID, "e2e-compact-keep-user")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	seedLongHistory(t, store, sessionID, 8)

	marker := fmt.Sprintf("CURRENT-INPUT-%d", time.Now().UnixNano())
	prompt := "Remember this marker for the current turn: " + marker + ". Reply with only ok."
	resp, err := agentSvc.Handle(ctx, userID, "e2e-compact-keep-user", prompt)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if strings.TrimSpace(resp) == "" {
		t.Fatal("empty response")
	}

	summary, err := store.LoadLatestSummary(ctx, sessionID)
	if err != nil {
		t.Fatalf("load summary: %v", err)
	}
	if summary == nil {
		t.Fatal("expected auto compact summary")
	}

	userMsg, err := findMessageByContent(gdb, sessionID, "user", marker)
	if err != nil {
		t.Fatalf("load marker user message: %v", err)
	}
	if userMsg == nil {
		t.Fatalf("expected user marker message to be persisted: %s", marker)
	}
	if summary.EntryID >= userMsg.ID {
		t.Fatalf("summary entry_id should be before current user message: entry_id=%d user_msg_id=%d", summary.EntryID, userMsg.ID)
	}

	view, _, err := store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		t.Fatalf("load view: %v", err)
	}
	if !viewContains(view, marker, "user") {
		t.Fatalf("expected current user message to remain in view after compaction, marker=%s", marker)
	}
}

func TestE2EContextCompactEntryIDMovesForwardAcrossCycles(t *testing.T) {
	store, gdb, agentSvc, userID := newCompactTestRig(t, 9202)
	ctx := context.Background()

	sessionID, err := store.GetOrCreateSession(ctx, userID, "e2e-compact-multi-cycle")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	seedLongHistory(t, store, sessionID, 8)

	marker1 := fmt.Sprintf("CYCLE-ONE-%d", time.Now().UnixNano())
	if _, err := agentSvc.Handle(ctx, userID, "e2e-compact-multi-cycle", "Current marker: "+marker1+". Reply: ok."); err != nil {
		t.Fatalf("cycle 1 handle: %v", err)
	}
	summary1, err := store.LoadLatestSummary(ctx, sessionID)
	if err != nil {
		t.Fatalf("load summary1: %v", err)
	}
	if summary1 == nil {
		t.Fatal("expected summary after cycle 1")
	}
	msg1, err := findMessageByContent(gdb, sessionID, "user", marker1)
	if err != nil {
		t.Fatalf("load marker1: %v", err)
	}
	if msg1 == nil {
		t.Fatalf("missing cycle-1 marker user message: %s", marker1)
	}
	if summary1.EntryID >= msg1.ID {
		t.Fatalf("cycle 1: expected entry_id < marker1 user id, got entry_id=%d user_id=%d", summary1.EntryID, msg1.ID)
	}

	seedLongHistory(t, store, sessionID, 7)

	marker2 := fmt.Sprintf("CYCLE-TWO-%d", time.Now().UnixNano())
	if _, err := agentSvc.Handle(ctx, userID, "e2e-compact-multi-cycle", "Current marker: "+marker2+". Reply: ok."); err != nil {
		t.Fatalf("cycle 2 handle: %v", err)
	}
	summary2, err := store.LoadLatestSummary(ctx, sessionID)
	if err != nil {
		t.Fatalf("load summary2: %v", err)
	}
	if summary2 == nil {
		t.Fatal("expected summary after cycle 2")
	}
	if summary2.ID <= summary1.ID {
		t.Fatalf("expected newer summary id after cycle 2: summary1=%d summary2=%d", summary1.ID, summary2.ID)
	}
	if summary2.EntryID <= summary1.EntryID {
		t.Fatalf("expected entry_id to move forward after cycle 2: summary1=%d summary2=%d", summary1.EntryID, summary2.EntryID)
	}

	msg2, err := findMessageByContent(gdb, sessionID, "user", marker2)
	if err != nil {
		t.Fatalf("load marker2: %v", err)
	}
	if msg2 == nil {
		t.Fatalf("missing cycle-2 marker user message: %s", marker2)
	}
	if summary2.EntryID >= msg2.ID {
		t.Fatalf("cycle 2: expected entry_id < marker2 user id, got entry_id=%d user_id=%d", summary2.EntryID, msg2.ID)
	}

	view, _, err := store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		t.Fatalf("load view after cycle 2: %v", err)
	}
	if !viewContains(view, marker2, "user") {
		t.Fatalf("expected cycle-2 current user message to remain in view, marker=%s", marker2)
	}
}

func newCompactTestRig(t *testing.T, telegramID int64) (memory.StoreAPI, *gorm.DB, *agent.Agent, int64) {
	t.Helper()
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	guidelinesMgr := guidelines.NewManager(gdb)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)

	contextCfg := llm.ContextConfig{
		WindowTokens:                  1400,
		AutoCompactTokenLimit:         1000,
		EffectiveContextWindowPercent: 80,
	}
	agentSvc := agent.New(store, nil, skillsMgr, registry, t.TempDir(), 6, client, model, "openai", llm.ReasoningConfig{}, contextCfg)
	agentSvc.SetMiddleware(agentdefaults.New(guidelinesMgr, store, nil, client, contextCfg, agentSvc.SessionStatusWriter()))

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "telegram", fmt.Sprint(telegramID))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return store, gdb, agentSvc, userID
}

func seedLongHistory(t *testing.T, store memory.StoreAPI, sessionID int64, turns int) {
	t.Helper()
	ctx := context.Background()
	blob := strings.Repeat("Long history to force auto compact. ", 36)
	for i := 0; i < turns; i++ {
		if err := store.AddMessage(ctx, sessionID, "user", fmt.Sprintf("history-user-%d %s", i, blob), nil); err != nil {
			t.Fatalf("seed user %d: %v", i, err)
		}
		if err := store.AddMessage(ctx, sessionID, "assistant", fmt.Sprintf("history-assistant-%d %s", i, blob), nil); err != nil {
			t.Fatalf("seed assistant %d: %v", i, err)
		}
	}
}

func findMessageByContent(gdb *gorm.DB, sessionID int64, role, needle string) (*dbmodel.Message, error) {
	var msg dbmodel.Message
	err := gdb.
		Where("session_id = ? AND role = ? AND content LIKE ?", sessionID, role, "%"+needle+"%").
		Order("id DESC").
		Limit(1).
		Find(&msg).Error
	if err != nil {
		return nil, err
	}
	if msg.ID == 0 {
		return nil, nil
	}
	return &msg, nil
}

func viewContains(view []memory.Message, needle, role string) bool {
	for _, msg := range view {
		if role != "" && msg.Role != role {
			continue
		}
		if strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}
