package server

import (
	"context"

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
	output, err := s.agent.Handle(ctx, uid, "telegram", update.Message.Text)
	if err != nil {
		log.Error("telegram agent error", zap.Error(err))
		return
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   output,
	}); err != nil {
		log.Error("telegram send error", zap.Error(err))
	}
}
