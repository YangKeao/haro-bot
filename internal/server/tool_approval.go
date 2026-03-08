package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/tools"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

type toolApprovalWaiter struct {
	req        tools.ApprovalRequest
	decisionCh chan tools.ApprovalDecision
}

type toolApprovalManager struct {
	mu      sync.Mutex
	pending map[int64]*toolApprovalWaiter
}

func newToolApprovalManager() *toolApprovalManager {
	return &toolApprovalManager{
		pending: make(map[int64]*toolApprovalWaiter),
	}
}

func (m *toolApprovalManager) register(req tools.ApprovalRequest) (*toolApprovalWaiter, error) {
	if req.SessionID == 0 {
		return nil, errors.New("session id required for approval")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.pending[req.SessionID]; exists {
		return nil, errors.New("approval already pending")
	}
	waiter := &toolApprovalWaiter{
		req:        req,
		decisionCh: make(chan tools.ApprovalDecision, 1),
	}
	m.pending[req.SessionID] = waiter
	return waiter, nil
}

func (m *toolApprovalManager) resolve(sessionID int64, decision tools.ApprovalDecision) (*toolApprovalWaiter, bool) {
	m.mu.Lock()
	waiter := m.pending[sessionID]
	if waiter != nil {
		delete(m.pending, sessionID)
	}
	m.mu.Unlock()
	if waiter == nil {
		return nil, false
	}
	waiter.decisionCh <- decision
	return waiter, true
}

func (m *toolApprovalManager) wait(ctx context.Context, sessionID int64, waiter *toolApprovalWaiter) (tools.ApprovalDecision, error) {
	select {
	case decision := <-waiter.decisionCh:
		return decision, nil
	case <-ctx.Done():
		m.mu.Lock()
		if cur := m.pending[sessionID]; cur == waiter {
			delete(m.pending, sessionID)
		}
		m.mu.Unlock()
		return tools.ApprovalDeny, ctx.Err()
	}
}

func (m *toolApprovalManager) pendingFor(sessionID int64) *toolApprovalWaiter {
	m.mu.Lock()
	waiter := m.pending[sessionID]
	m.mu.Unlock()
	return waiter
}

func (m *toolApprovalManager) handleCallback(ctx context.Context, sessionID, userID int64, data string, send func(context.Context, string) error) bool {
	waiter := m.pendingFor(sessionID)
	if waiter == nil {
		return false
	}
	if waiter.req.UserID != 0 && userID != 0 && waiter.req.UserID != userID {
		return false
	}
	decision, ok := parseApprovalCallback(data)
	if !ok {
		return false
	}
	_, ok = m.resolve(sessionID, decision)
	if !ok {
		return false
	}
	if decision == tools.ApprovalStop {
		_ = send(ctx, "Stopped.")
	}
	return true
}

const approvalCallbackPrefix = "tool_approval:"

func parseApprovalCallback(data string) (tools.ApprovalDecision, bool) {
	if data == "" {
		return "", false
	}
	if !strings.HasPrefix(data, approvalCallbackPrefix) {
		return "", false
	}
	value := strings.TrimPrefix(data, approvalCallbackPrefix)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow":
		return tools.ApprovalAllow, true
	case "deny":
		return tools.ApprovalDeny, true
	case "stop":
		return tools.ApprovalStop, true
	default:
		return "", false
	}
}

func approvalPrompt(req tools.ApprovalRequest) string {
	lines := []string{
		"Security approval required:",
		fmt.Sprintf("Tool: %s", req.Tool),
		fmt.Sprintf("Path: %s", req.Path),
	}
	if req.Reason != "" {
		lines = append(lines, fmt.Sprintf("Reason: %s", req.Reason))
	}
	lines = append(lines, "Please click: Approve / Deny / Stop")
	return strings.Join(lines, "\n")
}

func approvalKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Approve", CallbackData: approvalCallbackPrefix + "allow"},
				{Text: "Deny", CallbackData: approvalCallbackPrefix + "deny"},
			},
			{
				{Text: "Stop", CallbackData: approvalCallbackPrefix + "stop"},
			},
		},
	}
}

func (s *Server) sendApprovalPrompt(ctx context.Context, req tools.ApprovalRequest) error {
	dest, ok := s.telegramSessions.Get(req.SessionID)
	if !ok {
		return errors.New("telegram session not registered")
	}
	prompt := approvalPrompt(req)
	params := &bot.SendMessageParams{
		ChatID:      dest.chatID,
		Text:        prompt,
		ParseMode:   models.ParseModeMarkdown,
		ReplyMarkup: approvalKeyboard(),
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
	log := logging.L().Named("telegram")
	return sendTelegramMessage(ctx, log, s.telegram, params)
}

// RequestApproval implements tools.Approver for Telegram sessions.
func (s *Server) RequestApproval(ctx context.Context, req tools.ApprovalRequest) (tools.ApprovalDecision, error) {
	log := logging.L().Named("tool_approval")
	if s == nil {
		return tools.ApprovalDeny, errors.New("telegram server not configured")
	}
	if s.telegram == nil {
		return tools.ApprovalDeny, errors.New("telegram not configured")
	}
	if s.toolApprovals == nil {
		return tools.ApprovalDeny, errors.New("approval manager not configured")
	}
	if _, ok := s.telegramSessions.Get(req.SessionID); !ok {
		return tools.ApprovalDeny, errors.New("telegram session not registered")
	}
	waiter, err := s.toolApprovals.register(req)
	if err != nil {
		return tools.ApprovalDeny, err
	}
	if err := s.sendApprovalPrompt(ctx, req); err != nil {
		s.toolApprovals.resolve(req.SessionID, tools.ApprovalDeny)
		return tools.ApprovalDeny, err
	}
	log.Debug("tool approval requested", zap.Int64("session_id", req.SessionID), zap.String("tool", req.Tool), zap.String("path", req.Path))
	decision, err := s.toolApprovals.wait(ctx, req.SessionID, waiter)
	if err != nil {
		log.Warn("tool approval wait failed", zap.Int64("session_id", req.SessionID), zap.Error(err))
		return tools.ApprovalDeny, err
	}
	log.Debug("tool approval resolved", zap.Int64("session_id", req.SessionID), zap.String("decision", string(decision)))
	return decision, nil
}
