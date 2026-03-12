//go:build integration

package telegram

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
	agentdefaults "github.com/YangKeao/haro-bot/internal/agent/hooks/defaults"
	"github.com/YangKeao/haro-bot/internal/config"
	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/guidelines"
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

func parseTelegramPayload(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode json: %v", err)
		}
		out := make(map[string]string, len(payload))
		for k, v := range payload {
			switch val := v.(type) {
			case string:
				out[k] = val
			case float64:
				out[k] = strconv.FormatInt(int64(val), 10)
			default:
				out[k] = ""
			}
		}
		return out
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
	}
	return map[string]string{
		"chat_id": r.FormValue("chat_id"),
		"text":    r.FormValue("text"),
	}
}

func TestTelegramHandlerSendsMessage(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	guidelinesMgr := guidelines.NewManager(gdb)
	registry := tools.NewRegistry()
	llmClient, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, guidelinesMgr, t.TempDir(), 4, llmClient, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})
	agentSvc.SetHooks(agentdefaults.New(store, nil, llmClient, llm.ContextConfig{}, agentSvc.SessionStatusWriter()))

	srv := New(config.Config{}, agentSvc, store, skillsMgr, nil)

	token := "test-token"
	captureCh := make(chan telegramSendCapture, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		switch r.URL.Path {
		case "/bot" + token + "/sendMessage":
			payload := parseTelegramPayload(t, r)
			chatIDStr := payload["chat_id"]
			chatID, _ := strconv.ParseInt(chatIDStr, 10, 64)
			captureCh <- telegramSendCapture{
				Text:   payload["text"],
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
		case "/bot" + token + "/sendMessageDraft":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		case "/bot" + token + "/sendChatAction":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
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
