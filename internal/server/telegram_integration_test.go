//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type telegramSendCapture struct {
	Text   string
	ChatID int64
}

func TestTelegramHandlerSendsMessage(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	registry := tools.NewRegistry()
	llmClient, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, skillsMgr, registry, t.TempDir(), 4, llmClient, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})

	srv := New(config.Config{}, agentSvc, store, skillsMgr)

	token := "test-token"
	captureCh := make(chan telegramSendCapture, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/bot"+token+"/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		chatIDStr := r.FormValue("chat_id")
		chatID, _ := strconv.ParseInt(chatIDStr, 10, 64)
		captureCh <- telegramSendCapture{
			Text:   r.FormValue("text"),
			ChatID: chatID,
		}
		resp := map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 1,
				"date":       time.Now().Unix(),
				"chat": map[string]any{
					"id":   chatID,
					"type": "private",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	tg, err := bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithServerURL(ts.URL),
		bot.WithHTTPClient(5*time.Second, ts.Client()),
	)
	if err != nil {
		t.Fatalf("init bot: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	srv.handleTelegramUpdate(ctx, tg, &models.Update{
		Message: &models.Message{
			From: &models.User{ID: 4242},
			Chat: models.Chat{ID: 8686},
			Text: "Say hello in one short sentence.",
		},
	})

	select {
	case got := <-captureCh:
		if got.ChatID != 8686 {
			t.Fatalf("expected chat id 8686, got %d", got.ChatID)
		}
		if strings.TrimSpace(got.Text) == "" {
			t.Fatalf("expected non-empty response")
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("timeout waiting for telegram sendMessage")
	}

	var user dbmodel.User
	if err := gdb.Where("telegram_id = ?", int64(4242)).First(&user).Error; err != nil {
		t.Fatalf("expected telegram user to be created: %v", err)
	}
}
