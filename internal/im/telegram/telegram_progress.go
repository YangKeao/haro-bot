package telegram

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
	"golang.org/x/time/rate"
)

const (
	draftMinInterval      = 500 * time.Millisecond
	draftMinDeltaRunes    = 40
	typingActionInterval  = 4 * time.Second
	toolCallPreviewHeader = "Running tools:"
	maxToolArgsKeys       = 4
	maxToolArgRunes       = 120
	maxToolRawRunes       = 160
	// Rate limit for Telegram draft messages: 2 messages per second
	telegramDraftRateLimit = 2
	telegramDraftBurst      = 2
)

var (
	// Global rate limiters per bot instance
	globalRateLimiters sync.Map // map[*bot.Bot]*rate.Limiter
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
	reasoningText string
	lastSentBytes int
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

// getRateLimiter returns the rate limiter for a bot instance.
// Each bot instance shares the same rate limiter across all sessions.
func getRateLimiter(b *bot.Bot) *rate.Limiter {
	if limiter, ok := globalRateLimiters.Load(b); ok {
		return limiter.(*rate.Limiter)
	}
	
	// Create a new rate limiter: 2 messages per second, burst of 2
	limiter := rate.NewLimiter(rate.Limit(telegramDraftRateLimit), telegramDraftBurst)
	actual, _ := globalRateLimiters.LoadOrStore(b, limiter)
	return actual.(*rate.Limiter)
}

// allowRateLimit checks if we can send a draft message now.
// Returns true if allowed, false if rate limited (should skip this draft).
// This is non-blocking - it won't wait, just check and return immediately.
func (p *telegramProgress) allowRateLimit() bool {
	limiter := getRateLimiter(p.bot)
	return limiter.Allow()
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
	defer p.mu.Unlock()

	p.streamText += delta
	p.maybeSendDraftLocked(ctx)
}

func (p *telegramProgress) OnLLMReasoningDelta(ctx context.Context, delta string) {
	if p == nil || delta == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.reasoningText += delta
	p.maybeSendDraftLocked(ctx)
}

// maybeSendDraftLocked sends a draft if enough time or content has accumulated.
// Must be called with p.mu held.
func (p *telegramProgress) maybeSendDraftLocked(ctx context.Context) {
	currentBytes := len(p.streamText) + len(p.reasoningText)
	bytesDelta := currentBytes - p.lastSentBytes
	elapsed := time.Since(p.lastDraftSent)
	if bytesDelta < draftMinDeltaRunes && elapsed < draftMinInterval {
		return
	}
	baseID := p.streamBaseID
	text := p.buildDraftText()
	lastSent := p.lastSentBytes
	if currentBytes <= lastSent {
		return
	}
	
	// Check rate limit - if limited, skip this draft (non-blocking)
	if !p.allowRateLimit() {
		p.log.Debug("draft rate limited, skipping")
		return
	}
	
	if err := p.sendDraftOnce(ctx, baseID, text); err != nil {
		p.log.Debug("send draft failed", zap.Error(err))
		return
	}
	
	p.lastSentBytes = currentBytes
	p.lastDraftSent = time.Now()
}

// buildDraftText constructs the draft text with reasoning in italic format.
// Must be called with p.mu held.
func (p *telegramProgress) buildDraftText() string {
	var parts []string

	// Add reasoning text first (in italic format)
	if p.reasoningText != "" {
		// Use italic for reasoning content
		parts = append(parts, "_"+escapeMarkdown(p.reasoningText)+"_")
	}

	// Add regular content
	if p.streamText != "" {
		parts = append(parts, p.streamText)
	}

	return strings.Join(parts, "\n\n")
}

// escapeMarkdown escapes special markdown characters
func escapeMarkdown(text string) string {
	// Characters that need escaping in MarkdownV1
	specialChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	result := text
	for _, char := range specialChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}
	return result
}

func (p *telegramProgress) OnToolCalls(ctx context.Context, calls []llm.ToolCall, content string) {
	if p == nil || len(calls) == 0 {
		return
	}
	toolText := formatToolCalls(calls)
	if toolText == "" {
		return
	}
	// Prepend message content if present
	text := toolText
	if content = strings.TrimSpace(content); content != "" {
		text = content + "\n\n" + toolText
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	
	baseID := p.currentDraftBaseLocked()
	
	// Check rate limit - if limited, skip this draft (non-blocking)
	if !p.allowRateLimit() {
		p.log.Debug("draft rate limited, skipping")
		return
	}
	
	if err := p.sendDraftOnce(ctx, baseID, text); err != nil {
		p.log.Debug("send draft failed", zap.Error(err))
	}
}

// ClearDraft clears the draft message by sending an empty draft with the same draftId.
// This should be called before sending the final message to avoid the draft being displayed
// simultaneously with the final message.
func (p *telegramProgress) ClearDraft(ctx context.Context) {
	if p == nil || p.bot == nil {
		return
	}
	p.mu.Lock()
	baseID := p.streamBaseID
	p.mu.Unlock()
	if baseID == 0 {
		return
	}
	params := &bot.SendMessageDraftParams{
		ChatID:  p.chatID,
		DraftID: strconv.FormatInt(baseID+1, 10),
		Text:    "",
	}
	if p.threadID > 0 {
		params.MessageThreadID = p.threadID
	}
	if p.businessConnectionID != "" {
		params.BusinessConnectionID = p.businessConnectionID
	}
	_, _ = p.bot.SendMessageDraft(ctx, params)
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
	p.reasoningText = ""
	p.lastSentBytes = 0
	p.lastDraftSent = time.Time{}
	p.mu.Unlock()
}

func (p *telegramProgress) nextDraftBase() int64 {
	return time.Now().UnixNano() + atomic.AddInt64(&p.sequence, 1)
}

func (p *telegramProgress) currentDraftBaseLocked() int64 {
	if p.streamBaseID == 0 {
		p.streamBaseID = p.nextDraftBase()
	}
	return p.streamBaseID
}

func (p *telegramProgress) sendDraftOnce(ctx context.Context, baseID int64, text string) error {
	if p == nil || p.bot == nil {
		return nil
	}
	if text == "" {
		return nil
	}
	// For draft messages, truncate to safe byte limit instead of splitting.
	// We keep the last part because the most recent content is at the end.
	if len(text) > telegramSafeMessageBytes {
		// Truncate from the beginning, keeping valid UTF-8
		text = truncateToByteLimit(text, telegramSafeMessageBytes)
	}
	params := &bot.SendMessageDraftParams{
		ChatID:    p.chatID,
		DraftID:   strconv.FormatInt(baseID+1, 10),
		Text:      text,
		ParseMode: models.ParseModeMarkdown,
	}
	if p.threadID > 0 {
		params.MessageThreadID = p.threadID
	}
	if p.businessConnectionID != "" {
		params.BusinessConnectionID = p.businessConnectionID
	}
	return sendTelegramDraftOnce(ctx, p.log, p.bot, params)
}

// truncateToByteLimit truncates text to fit within maxBytes, keeping the end portion.
// It ensures valid UTF-8 by not cutting in the middle of a multibyte character.
func truncateToByteLimit(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}

	// Find the starting position that gives us maxBytes
	// Start from the end and work backwards
	runes := []rune(text)
	startByte := len(text) - maxBytes

	// Find the rune index that corresponds to startByte
	bytePos := 0
	startRune := 0
	for i, r := range runes {
		if bytePos >= startByte {
			startRune = i
			break
		}
		bytePos += utf8.RuneLen(r)
	}

	// Return from startRune to end
	return string(runes[startRune:])
}

func sendTelegramDraftOnce(ctx context.Context, log *zap.Logger, b *bot.Bot, params *bot.SendMessageDraftParams) error {
	_, err := b.SendMessageDraft(ctx, params)
	if err != nil {
		// Try without parse mode if markdown fails
		params.ParseMode = ""
		_, err = b.SendMessageDraft(ctx, params)
		if err != nil && log != nil {
			log.Debug("telegram sendMessageDraft failed", zap.Error(err))
		}
	}
	return err
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
