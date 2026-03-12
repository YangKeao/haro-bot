package telegram

import (
	"context"
	"strconv"
	"unicode/utf8"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

func (s *Server) StartTelegramPolling(ctx context.Context) {
	log := logging.L().Named("telegram")
	if s.cfg.TelegramToken == "" {
		log.Debug("telegram token missing; polling disabled")
		return
	}
	if s.telegram != nil {
		log.Debug("telegram already initialized")
		return
	}
	tg, err := bot.New(
		s.cfg.TelegramToken,
		bot.WithDefaultHandler(s.handleTelegramUpdate),
	)
	if err != nil {
		log.Error("telegram init error", zap.Error(err))
		return
	}
	s.telegram = tg
	log.Info("telegram polling started")
	go tg.Start(ctx)
}

func (s *Server) handleTelegramUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	log := logging.L().Named("telegram")
	if update == nil {
		log.Debug("telegram update ignored (nil)")
		return
	}
	if update.CallbackQuery != nil {
		s.handleTelegramCallback(ctx, b, update.CallbackQuery)
		return
	}
	if update.Message == nil || update.Message.Text == "" {
		log.Debug("telegram update ignored (no text)")
		return
	}
	if update.Message.From == nil {
		log.Warn("telegram update missing sender")
		return
	}
	uid, err := s.store.GetOrCreateUserByExternalID(ctx, "telegram", strconv.FormatInt(update.Message.From.ID, 10))
	if err != nil {
		log.Warn("telegram user error", zap.Error(err))
		return
	}
	sessionID, err := s.store.GetOrCreateSession(ctx, uid, "telegram")
	if err != nil {
		log.Warn("telegram session error", zap.Error(err))
		return
	}
	// Handle commands
	if s.handleTelegramCommand(ctx, b, update, uid, sessionID) {
		return
	}

	// Cancel any previous operation for this session
	if s.agent.CancelSession(sessionID) {
		log.Info("cancelled previous session operation", zap.Int64("session_id", sessionID))
	}

	threadID := update.Message.MessageThreadID
	businessConnID := update.Message.BusinessConnectionID
	directTopicID := 0
	if update.Message.DirectMessagesTopic != nil {
		directTopicID = update.Message.DirectMessagesTopic.TopicID
	}
	progress := newTelegramProgress(b, update.Message.Chat.ID, threadID, businessConnID)
	defer progress.Stop()
	output, err := s.agent.HandleWithMiddleware(ctx, uid, "telegram", update.Message.Text, "", agent.MiddlewareSet{
		LLMMiddleware:     []agent.LLMMiddleware{progress},
		LLMDeltaListeners: []agent.LLMDeltaListener{progress},
		ToolCallListeners: []agent.ToolCallListener{progress},
		OutputListeners:   []agent.OutputListener{progress},
	})
	if err != nil {
		log.Error("telegram agent error", zap.Error(err))
		return
	}
	for _, chunk := range splitTelegramMessage(output) {
		params := &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      chunk,
			ParseMode: models.ParseModeMarkdown,
		}
		if threadID > 0 {
			params.MessageThreadID = threadID
		}
		if directTopicID > 0 {
			params.DirectMessagesTopicID = directTopicID
		}
		if businessConnID != "" {
			params.BusinessConnectionID = businessConnID
		}
		if err := sendTelegramMessage(ctx, log, b, params); err != nil {
			log.Error("telegram send error", zap.Error(err))
			return
		}
	}
}

func (s *Server) handleTelegramCallback(ctx context.Context, b *bot.Bot, query *models.CallbackQuery) {
	log := logging.L().Named("telegram")
	if query == nil {
		return
	}
	if query.From.ID == 0 {
		log.Warn("telegram callback missing sender")
		return
	}
	uid, err := s.store.GetOrCreateUserByExternalID(ctx, "telegram", strconv.FormatInt(query.From.ID, 10))
	if err != nil {
		log.Warn("telegram user error", zap.Error(err))
		return
	}
	_, err = s.store.GetOrCreateSession(ctx, uid, "telegram")
	if err != nil {
		log.Warn("telegram session error", zap.Error(err))
		return
	}
	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
	})
	log.Debug("telegram callback ignored", zap.String("data", query.Data))
}

func sendTelegramMessage(ctx context.Context, log *zap.Logger, b *bot.Bot, params *bot.SendMessageParams) error {
	if err := withTelegramRetry(ctx, log, "sendMessage", func(ctx context.Context) error {
		_, err := b.SendMessage(ctx, params)
		return err
	}); err == nil {
		return nil
	}
	// Fallback to plain text if Markdown parsing fails.
	params.ParseMode = ""
	return withTelegramRetry(ctx, log, "sendMessage_plain", func(ctx context.Context) error {
		_, err := b.SendMessage(ctx, params)
		return err
	})
}

const (
	// Telegram message limit is 4096 bytes (not characters)
	// https://core.telegram.org/bots/api#sendmessage
	telegramMaxMessageBytes  = 4096
	telegramSafeMessageBytes = 3800 // Safe limit to account for overhead
)

// splitTelegramMessage splits text into chunks that fit within Telegram's byte limit.
// It prefers to split on newlines to preserve readability.
func splitTelegramMessage(text string) []string {
	if text == "" {
		return nil
	}
	maxBytes := telegramSafeMessageBytes
	if maxBytes <= 0 || maxBytes > telegramMaxMessageBytes {
		maxBytes = telegramMaxMessageBytes
	}

	// Fast path: if the entire message fits in bytes, return as-is
	if len(text) <= maxBytes {
		return []string{text}
	}

	// Need to split by bytes, preserving valid UTF-8 boundaries
	runes := []rune(text)
	var parts []string

	start := 0
	for start < len(runes) {
		end := len(runes)
		// Calculate byte length of runes[start:end]
		byteLen := 0
		for i := start; i < len(runes); i++ {
			runeBytes := utf8.RuneLen(runes[i])
			if byteLen+runeBytes > maxBytes {
				end = i
				break
			}
			byteLen += runeBytes
		}

		// If we couldn't fit any runes, take at least one
		if end == start {
			end = start + 1
		}

		// If we've consumed all runes, add the last part and break
		if end >= len(runes) {
			parts = append(parts, string(runes[start:]))
			break
		}

		// Try to find a newline to split on (look backwards from end)
		splitPos := end
		for i := end - 1; i > start; i-- {
			if runes[i] == '\n' {
				splitPos = i + 1
				break
			}
		}

		parts = append(parts, string(runes[start:splitPos]))
		start = splitPos
	}

	return parts
}
