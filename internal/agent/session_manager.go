package agent

import (
	"sync"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
)

type sessionDeps struct {
	store          ConversationStore
	memoryEngine   *memory.Engine
	skills         *skills.Manager
	toolRegistry   *tools.Registry
	promptBuilder  PromptBuilder
	toolRunner     ToolRunner
	defaultBaseDir string
	maxToolTurns   int
	llm            llm.ChatClient
	model          string
	promptFormat   string
	reasoning      llm.ReasoningConfig
	contextConfig  llm.ContextConfig
	tokenEstimator *llm.TokenEstimator
	stateManager   *sessionStateManager
}

type sessionManager struct {
	mu       sync.Mutex
	sessions map[int64]*Session
	deps     *sessionDeps
}

func newSessionManager(deps *sessionDeps) *sessionManager {
	return &sessionManager{
		sessions: make(map[int64]*Session),
		deps:     deps,
	}
}

func (m *sessionManager) Get(sessionID int64) *Session {
	if m == nil || sessionID == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[sessionID]
	if session == nil {
		session = &Session{id: sessionID, deps: m.deps}
		m.sessions[sessionID] = session
	}
	session.refs++
	return session
}

func (m *sessionManager) Release(sessionID int64) {
	if m == nil || sessionID == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[sessionID]
	if session == nil {
		return
	}
	session.refs--
	if session.refs <= 0 {
		delete(m.sessions, sessionID)
	}
}

// Cancel cancels any ongoing operation for the session.
// It returns true if there was an active operation to cancel.
func (m *sessionManager) Cancel(sessionID int64) bool {
	if m == nil || sessionID == 0 {
		return false
	}
	m.mu.Lock()
	session := m.sessions[sessionID]
	m.mu.Unlock()
	if session == nil {
		return false
	}
	return session.cancel()
}
