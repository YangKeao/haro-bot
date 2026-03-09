package server

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

const telegramMessageLimit = 4096
// Reserve some space for message prefix (e.g., "🧵 Goroutines (1/10)\n\n")
const telegramMessageReserve = 100


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
	case "/goroutines":
		s.handleGoroutinesCommand(ctx, b, update)
		return true
	case "/exit":
		s.handleExitCommand(ctx, b, update)
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
/goroutines — Show goroutine stack traces (debug)
/exit — Force exit the process (danger!)
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

func (s *Server) handleGoroutinesCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	log := logging.L().Named("telegram_cmd")
	if update.Message == nil {
		return
	}

	// Get goroutine profiles
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		log.Error("failed to get goroutine profile", zap.Error(err))
		s.sendErrorMessage(ctx, b, update, "Failed to get goroutine profile")
		return
	}

	content := buf.String()
	
	// Send in multiple messages if content exceeds limit
	chunks := splitMessage(content, telegramMessageLimit-telegramMessageReserve)
	for i, chunk := range chunks {
		var text string
		if len(chunks) == 1 {
			text = fmt.Sprintf("🧵 Goroutines\n\n%s", chunk)
		} else {
			text = fmt.Sprintf("🧵 Goroutines (%d/%d)\n\n%s", i+1, len(chunks), chunk)
		}

		params := &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   text,
		}
		if update.Message.MessageThreadID > 0 {
			params.MessageThreadID = update.Message.MessageThreadID
		}
		if update.Message.BusinessConnectionID != "" {
			params.BusinessConnectionID = update.Message.BusinessConnectionID
		}
		if _, err := b.SendMessage(ctx, params); err != nil {
			log.Error("failed to send goroutines", zap.Error(err), zap.Int("chunk", i+1))
			break
		}
	}
}

func (s *Server) handleExitCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	log := logging.L().Named("telegram_cmd")
	if update.Message == nil {
		return
	}

	// Send goodbye message before exiting
	params := &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "💥 Exiting process...",
	}
	if update.Message.MessageThreadID > 0 {
		params.MessageThreadID = update.Message.MessageThreadID
	}
	if update.Message.BusinessConnectionID != "" {
		params.BusinessConnectionID = update.Message.BusinessConnectionID
	}
	_, _ = b.SendMessage(ctx, params)

	log.Warn("force exit triggered via Telegram command", zap.Int64("chat_id", update.Message.Chat.ID))
	os.Exit(1)
}

func splitMessage(content string, limit int) []string {
	if len(content) <= limit {
		return []string{content}
	}

	var chunks []string
	for len(content) > limit {
		// Try to find a good split point (newline) near the limit
		splitIdx := limit
		if idx := strings.LastIndex(content[:limit], "\n\n"); idx > limit/2 {
			splitIdx = idx + 2 // include the double newline
		} else if idx := strings.LastIndex(content[:limit], "\n"); idx > limit/2 {
			splitIdx = idx + 1 // include the newline
		}
		chunks = append(chunks, content[:splitIdx])
		content = content[splitIdx:]
	}
	if content != "" {
		chunks = append(chunks, content)
	}
	return chunks
}

func (s *Server) sendErrorMessage(ctx context.Context, b *bot.Bot, update *models.Update, msg string) {
	if update.Message == nil {
		return
	}
	params := &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("❌ %s", msg),
	}
	if update.Message.MessageThreadID > 0 {
		params.MessageThreadID = update.Message.MessageThreadID
	}
	if update.Message.BusinessConnectionID != "" {
		params.BusinessConnectionID = update.Message.BusinessConnectionID
	}
	_, _ = b.SendMessage(ctx, params)
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
