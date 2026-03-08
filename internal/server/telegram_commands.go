package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

func (s *Server) handleTelegramCommand(ctx context.Context, b *bot.Bot, update *models.Update, uid int64, sessionID int64) bool {
	if update.Message == nil || update.Message.Text == "" {
		return false
	}
	text := strings.TrimSpace(update.Message.Text)
	if !strings.HasPrefix(text, "/") {
		return false
	}

	// Parse command - handle both /status and /status@botname formats
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false
	}
	cmd := strings.ToLower(parts[0])
	// Strip bot username if present (e.g., /status@mybot -> /status)
	if idx := strings.Index(cmd, "@"); idx > 0 {
		cmd = cmd[:idx]
	}

	log := logging.L().Named("telegram_cmd")

	switch cmd {
	case "/status":
		s.handleStatusCommand(ctx, b, update, sessionID)
		return true
	case "/help":
		s.handleHelpCommand(ctx, b, update)
		return true
	default:
		// Unknown command, let it pass through to normal handling
		log.Debug("unknown command", zap.String("cmd", cmd))
		return false
	}
}

func (s *Server) handleStatusCommand(ctx context.Context, b *bot.Bot, update *models.Update, sessionID int64) {
	log := logging.L().Named("telegram_cmd")
	if update.Message == nil {
		return
	}

	status := s.agent.GetSessionStatus(sessionID)
	
	var statusText string
	switch status.State {
	case agent.StateIdle:
		statusText = "🟢 Idle — waiting for input"
	case agent.StateWaitingForLLM:
		elapsed := time.Since(status.StartTime)
		statusText = fmt.Sprintf("🟡 Waiting for LLM response\n⏱ Elapsed: %s\n🤖 Model: %s", formatDuration(elapsed), status.LLMModel)
	case agent.StateRunningTools:
		elapsed := time.Since(status.StartTime)
		statusText = fmt.Sprintf("🔧 Running tool: %s\n⏱ Elapsed: %s", status.CurrentTool, formatDuration(elapsed))
	case agent.StateWaitingForApproval:
		elapsed := time.Since(status.StartTime)
		statusText = fmt.Sprintf("⏸ Waiting for approval\n📋 %s\n⏱ Elapsed: %s", status.Message, formatDuration(elapsed))
	default:
		statusText = fmt.Sprintf("❓ Unknown state: %s", status.State)
	}

	params := &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("📊 Session Status\n\n%s", statusText),
		// Don't use Markdown to avoid escaping issues
	}
	if update.Message.MessageThreadID > 0 {
		params.MessageThreadID = update.Message.MessageThreadID
	}
	if update.Message.BusinessConnectionID != "" {
		params.BusinessConnectionID = update.Message.BusinessConnectionID
	}
	if _, err := b.SendMessage(ctx, params); err != nil {
		log.Error("failed to send status", zap.Error(err))
	}
}

func (s *Server) handleHelpCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	log := logging.L().Named("telegram_cmd")
	if update.Message == nil {
		return
	}

	helpText := `📖 Available Commands

/status — Show current session status
/help — Show this help message

📊 Status States
🟢 Idle — Ready to receive input
🟡 Waiting for LLM — LLM is generating response
🔧 Running tools — Executing tool operations
⏸ Waiting for approval — Awaiting user confirmation`

	params := &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   helpText,
		// Don't use Markdown to avoid escaping issues
	}
	if update.Message.MessageThreadID > 0 {
		params.MessageThreadID = update.Message.MessageThreadID
	}
	if update.Message.BusinessConnectionID != "" {
		params.BusinessConnectionID = update.Message.BusinessConnectionID
	}
	if _, err := b.SendMessage(ctx, params); err != nil {
		log.Error("failed to send help", zap.Error(err))
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}
