//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
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

func TestTelegramHandlerReadFileToolFlow(t *testing.T) {
	testutil.EnsureIntegrationEnv(t)
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	rootDir := t.TempDir()
	token := fmt.Sprintf("TG-E2E-%d", time.Now().UnixNano())
	filePath := filepath.Join(rootDir, "telegram_facts.txt")
	if err := os.WriteFile(filePath, []byte("token="+token+"\n"), 0o644); err != nil {
		t.Fatalf("write telegram facts file: %v", err)
	}

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	guidelinesMgr := guidelines.NewManager(gdb)
	auditStore := tools.NewAuditStore(gdb)
	fsTools := tools.NewFS([]string{rootDir}, auditStore, false)
	registry := tools.NewRegistry(
		tools.NewListDirTool(fsTools),
		tools.NewReadFileTool(fsTools),
	)
	llmClient, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, guidelinesMgr, rootDir, 12, llmClient, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})
	srv := New(config.Config{}, agentSvc, store, skillsMgr, nil)

	tgToken := "test-token"
	captureCh := make(chan telegramSendCapture, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		switch r.URL.Path {
		case "/bot" + tgToken + "/sendMessage":
			payload := parseTelegramPayload(t, r)
			chatID, _ := strconv.ParseInt(payload["chat_id"], 10, 64)
			captureCh <- telegramSendCapture{
				Text:   payload["text"],
				ChatID: chatID,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"message_id": 1,
					"date":       time.Now().Unix(),
					"chat": map[string]any{
						"id":   chatID,
						"type": "private",
					},
				},
			})
		case "/bot" + tgToken + "/sendMessageDraft":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		case "/bot" + tgToken + "/sendChatAction":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(ts.Close)

	tg, err := bot.New(
		tgToken,
		bot.WithSkipGetMe(),
		bot.WithServerURL(ts.URL),
		bot.WithHTTPClient(5*time.Second, ts.Client()),
	)
	if err != nil {
		t.Fatalf("init bot: %v", err)
	}

	userMessage := fmt.Sprintf(
		"Use tools to read file %s and reply with only the token value after token=.",
		filePath,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	srv.handleTelegramUpdate(ctx, tg, &models.Update{
		Message: &models.Message{
			From: &models.User{ID: 9898},
			Chat: models.Chat{ID: 7878},
			Text: userMessage,
		},
	})

	select {
	case got := <-captureCh:
		if got.ChatID != 7878 {
			t.Fatalf("expected chat id 7878, got %d", got.ChatID)
		}
		if !strings.Contains(strings.ToLower(got.Text), strings.ToLower(token)) {
			t.Fatalf("expected telegram response to contain %q, got: %s", token, got.Text)
		}
	case <-time.After(40 * time.Second):
		t.Fatalf("timeout waiting for telegram sendMessage")
	}

	var audits []dbmodel.ToolAudit
	if err := gdb.Where("tool = ? AND status = ?", "read_file", "ok").Find(&audits).Error; err != nil {
		t.Fatalf("query tool audit: %v", err)
	}
	if len(audits) == 0 {
		t.Fatalf("expected read_file audit record")
	}
}
