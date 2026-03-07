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
	if update == nil || update.Message == nil || update.Message.Text == "" {
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
	threadID := update.Message.MessageThreadID
	businessConnID := update.Message.BusinessConnectionID
	progress := newTelegramProgress(b, update.Message.Chat.ID, threadID, businessConnID)
	defer progress.Stop()
	output, err := s.agent.HandleWithObserver(ctx, uid, "telegram", update.Message.Text, "", progress)
	if err != nil {
		log.Error("telegram agent error", zap.Error(err))
		return
	}
	directTopicID := 0
	if update.Message.DirectMessagesTopic != nil {
		directTopicID = update.Message.DirectMessagesTopic.TopicID
	}
	for _, chunk := range splitTelegramMessage(output) {
		params := &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   chunk,
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
		if _, err := b.SendMessage(ctx, params); err != nil {
			log.Error("telegram send error", zap.Error(err))
			return
		}
	}
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
