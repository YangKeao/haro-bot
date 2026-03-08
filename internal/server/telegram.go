package server

import (
	"context"
	"unicode/utf8"

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
	uid, err := s.store.GetOrCreateUserByTelegramID(ctx, update.Message.From.ID)
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
	s.telegramSessions.Set(sessionID, telegramSessionDestination{
		chatID:               update.Message.Chat.ID,
		threadID:             threadID,
		directTopicID:        directTopicID,
		businessConnectionID: businessConnID,
	})
	progress := newTelegramProgress(b, update.Message.Chat.ID, threadID, businessConnID)
	defer progress.Stop()
	output, err := s.agent.HandleWithObserver(ctx, uid, "telegram", update.Message.Text, "", progress)
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
	uid, err := s.store.GetOrCreateUserByTelegramID(ctx, query.From.ID)
	if err != nil {
		log.Warn("telegram user error", zap.Error(err))
		return
	}
	sessionID, err := s.store.GetOrCreateSession(ctx, uid, "telegram")
	if err != nil {
		log.Warn("telegram session error", zap.Error(err))
		return
	}
	if query.Message.Message != nil {
		msg := query.Message.Message
		threadID := msg.MessageThreadID
		businessConnID := msg.BusinessConnectionID
		directTopicID := 0
		if msg.DirectMessagesTopic != nil {
			directTopicID = msg.DirectMessagesTopic.TopicID
		}
		s.telegramSessions.Set(sessionID, telegramSessionDestination{
			chatID:               msg.Chat.ID,
			threadID:             threadID,
			directTopicID:        directTopicID,
			businessConnectionID: businessConnID,
		})
	}
	handled := false
	if s.toolApprovals != nil {
		handled = s.toolApprovals.handleCallback(ctx, sessionID, uid, query.Data, func(ctx context.Context, msg string) error {
			return s.SendSessionMessage(ctx, sessionID, msg)
		})
	}
	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
	})
	if handled {
		return
	}
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
	telegramMaxMessageRunes  = 4096
	telegramSafeMessageRunes = 3800
)

func splitTelegramMessage(text string) []string {
	if text == "" {
		return nil
	}
	max := telegramSafeMessageRunes
	if max <= 0 || max > telegramMaxMessageRunes {
		max = telegramMaxMessageRunes
	}
	if utf8.RuneCountInString(text) <= max {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= max {
		return []string{text}
	}
	parts := make([]string, 0, (len(runes)/max)+1)
	for start := 0; start < len(runes); {
		end := start + max
		if end >= len(runes) {
			parts = append(parts, string(runes[start:]))
			break
		}
		split := end
		for i := end - 1; i > start; i-- {
			if runes[i] == '\n' {
				split = i + 1
				break
			}
		}
		if split == start {
			split = end
		}
		parts = append(parts, string(runes[start:split]))
		start = split
	}
	return parts
}
