package server

import (
	"context"
	"errors"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// SendSessionMessage implements agent.SessionMessenger for Telegram sessions.
// It delivers the message to the latest known Telegram destination for the session.
func (s *Server) SendSessionMessage(ctx context.Context, sessionID int64, message string) error {
	if s == nil {
		return errors.New("telegram server not configured")
	}
	if message == "" {
		return nil
	}
	if s.telegram == nil {
		return errors.New("telegram not configured")
	}
	dest, ok := s.telegramSessions.Get(sessionID)
	if !ok {
		return errors.New("telegram session not registered")
	}
	log := logging.L().Named("telegram")
	for _, chunk := range splitTelegramMessage(message) {
		params := &bot.SendMessageParams{
			ChatID:    dest.chatID,
			Text:      chunk,
			ParseMode: models.ParseModeMarkdown,
		}
		if dest.threadID > 0 {
			params.MessageThreadID = dest.threadID
		}
		if dest.directTopicID > 0 {
			params.DirectMessagesTopicID = dest.directTopicID
		}
		if dest.businessConnectionID != "" {
			params.BusinessConnectionID = dest.businessConnectionID
		}
		if err := sendTelegramMessage(ctx, log, s.telegram, params); err != nil {
			return err
		}
	}
	return nil
}
