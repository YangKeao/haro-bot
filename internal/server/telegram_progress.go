package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

const (
	draftMinInterval      = 500 * time.Millisecond
	draftMinDeltaRunes    = 40
	typingActionInterval  = 4 * time.Second
	toolCallPreviewHeader = "Running tools:"
	maxToolArgsKeys       = 4
	maxToolArgRunes       = 120
	maxToolRawRunes       = 160
)

type telegramProgress struct {
	bot                  *bot.Bot
	chatID               int64
	threadID             int
	businessConnectionID string
	log                  *zap.Logger

	mu            sync.Mutex
	typingCancel  context.CancelFunc
	streamBaseID  int64
	streamText    string
	lastSentRunes int
	lastDraftSent time.Time
	sequence      int64
}

func newTelegramProgress(b *bot.Bot, chatID int64, threadID int, businessConnectionID string) *telegramProgress {
	return &telegramProgress{
		bot:                  b,
		chatID:               chatID,
		threadID:             threadID,
		businessConnectionID: strings.TrimSpace(businessConnectionID),
		log:                  logging.L().Named("telegram"),
	}
}

func (p *telegramProgress) Stop() {
	p.mu.Lock()
	cancel := p.typingCancel
	p.typingCancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *telegramProgress) OnLLMStart(ctx context.Context, _ agent.LLMStartInfo) {
	p.ensureTyping(ctx)
	p.resetStream()
}

func (p *telegramProgress) OnLLMStreamDelta(ctx context.Context, delta string) {
	if p == nil || delta == "" {
		return
	}
	p.mu.Lock()
	p.streamText += delta
	currentRunes := utf8.RuneCountInString(p.streamText)
	runesDelta := currentRunes - p.lastSentRunes
	elapsed := time.Since(p.lastDraftSent)
	if runesDelta < draftMinDeltaRunes && elapsed < draftMinInterval {
		p.mu.Unlock()
		return
	}
	text := p.streamText
	baseID := p.streamBaseID
	p.lastSentRunes = currentRunes
	p.lastDraftSent = time.Now()
	p.mu.Unlock()

	p.sendDraft(ctx, baseID, text)
}

func (p *telegramProgress) OnToolCalls(ctx context.Context, calls []llm.ToolCall) {
	if p == nil || len(calls) == 0 {
		return
	}
	text := formatToolCalls(calls)
	p.sendDraft(ctx, p.currentDraftBase(), text)
}

func (p *telegramProgress) ensureTyping(ctx context.Context) {
	if p == nil || p.bot == nil {
		return
	}
	p.mu.Lock()
	if p.typingCancel != nil {
		p.mu.Unlock()
		return
	}
	typingCtx, cancel := context.WithCancel(ctx)
	p.typingCancel = cancel
	p.mu.Unlock()

	p.sendTyping(typingCtx)
	go func() {
		ticker := time.NewTicker(typingActionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				p.sendTyping(typingCtx)
			}
		}
	}()
}

func (p *telegramProgress) sendTyping(ctx context.Context) {
	if p == nil || p.bot == nil {
		return
	}
	params := &bot.SendChatActionParams{
		ChatID: p.chatID,
		Action: models.ChatActionTyping,
	}
	if p.threadID > 0 {
		params.MessageThreadID = p.threadID
	}
	if p.businessConnectionID != "" {
		params.BusinessConnectionID = p.businessConnectionID
	}
	if err := withTelegramRetry(ctx, p.log, "sendChatAction", func(ctx context.Context) error {
		_, err := p.bot.SendChatAction(ctx, params)
		return err
	}); err != nil && p.log != nil {
		p.log.Debug("telegram sendChatAction failed", zap.Error(err))
	}
}

func (p *telegramProgress) resetStream() {
	p.mu.Lock()
	p.streamBaseID = p.nextDraftBase()
	p.streamText = ""
	p.lastSentRunes = 0
	p.lastDraftSent = time.Time{}
	p.mu.Unlock()
}

func (p *telegramProgress) nextDraftBase() int64 {
	return time.Now().UnixNano() + atomic.AddInt64(&p.sequence, 1)
}

func (p *telegramProgress) currentDraftBase() int64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	if p.streamBaseID == 0 {
		p.streamBaseID = p.nextDraftBase()
	}
	base := p.streamBaseID
	p.mu.Unlock()
	return base
}

func (p *telegramProgress) sendDraft(ctx context.Context, baseID int64, text string) {
	if p == nil || p.bot == nil {
		return
	}
	parts := splitTelegramMessage(text)
	if len(parts) == 0 {
		return
	}
	for i, part := range parts {
		params := &bot.SendMessageDraftParams{
			ChatID:    p.chatID,
			DraftID:   strconv.FormatInt(baseID+int64(i+1), 10),
			Text:      part,
			ParseMode: models.ParseModeMarkdown,
		}
		if p.threadID > 0 {
			params.MessageThreadID = p.threadID
		}
		if p.businessConnectionID != "" {
			params.BusinessConnectionID = p.businessConnectionID
		}
		if err := sendTelegramDraft(ctx, p.log, p.bot, params); err != nil && p.log != nil {
			p.log.Debug("telegram sendMessageDraft failed", zap.Error(err))
			return
		}
	}
}

func sendTelegramDraft(ctx context.Context, log *zap.Logger, b *bot.Bot, params *bot.SendMessageDraftParams) error {
	if err := withTelegramRetry(ctx, log, "sendMessageDraft", func(ctx context.Context) error {
		_, err := b.SendMessageDraft(ctx, params)
		return err
	}); err == nil {
		return nil
	}
	params.ParseMode = ""
	return withTelegramRetry(ctx, log, "sendMessageDraft_plain", func(ctx context.Context) error {
		_, err := b.SendMessageDraft(ctx, params)
		return err
	})
}

func formatToolCalls(calls []llm.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(toolCallPreviewHeader)
	b.WriteString("\n")
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			name = "tool"
		}
		b.WriteString("- ")
		b.WriteString(name)
		if args := formatToolArgs(name, call.Function.Arguments); args != "" {
			b.WriteString(" ")
			b.WriteString(args)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func formatToolArgs(name, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return truncateRunes(raw, maxToolRawRunes)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, maxToolArgsKeys)
	for _, key := range keys {
		if len(parts) >= maxToolArgsKeys {
			break
		}
		val := summarizeArgValue(key, obj[key], name)
		if val == "" {
			continue
		}
		parts = append(parts, key+"="+val)
	}
	if len(parts) == 0 {
		return truncateRunes(raw, maxToolRawRunes)
	}
	return strings.Join(parts, " ")
}

func summarizeArgValue(key string, val any, toolName string) string {
	if key == "" {
		return ""
	}
	lowerKey := strings.ToLower(key)
	if strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "secret") || strings.Contains(lowerKey, "password") || strings.Contains(lowerKey, "key") {
		return "***"
	}
	switch v := val.(type) {
	case string:
		if strings.EqualFold(key, "content") || strings.EqualFold(key, "text") || strings.EqualFold(key, "code") {
			return strconv.Itoa(utf8.RuneCountInString(v)) + " chars"
		}
		return truncateRunes(v, maxToolArgRunes)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []any:
		return strconv.Itoa(len(v)) + " items"
	case map[string]any:
		return strconv.Itoa(len(v)) + " keys"
	default:
		return truncateRunes(strings.TrimSpace(fmt.Sprint(v)), maxToolArgRunes)
	}
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "..."
}
