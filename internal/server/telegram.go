package server

import (
	"context"
	"log"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (s *Server) StartTelegramPolling(ctx context.Context) {
	if s.cfg.TelegramToken == "" {
		return
	}
	if s.telegram != nil {
		return
	}
	tg, err := bot.New(
		s.cfg.TelegramToken,
		bot.WithDefaultHandler(s.handleTelegramUpdate),
	)
	if err != nil {
		log.Printf("telegram init error: %v", err)
		return
	}
	s.telegram = tg
	go tg.Start(ctx)
}

func (s *Server) handleTelegramUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil || update.Message.Text == "" {
		return
	}
	if update.Message.From == nil {
		return
	}
	uid, err := s.store.GetOrCreateUserByTelegramID(ctx, update.Message.From.ID)
	if err != nil {
		log.Printf("telegram user error: %v", err)
		return
	}
	output, err := s.agent.Handle(ctx, uid, "telegram", update.Message.Text)
	if err != nil {
		log.Printf("telegram agent error: %v", err)
		return
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   output,
	}); err != nil {
		log.Printf("telegram send error: %v", err)
	}
}
