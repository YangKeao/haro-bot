package agent

import (
	"sync"
	"time"
)

// SessionState represents the current state of a session.
type SessionState string

const (
	StateIdle              SessionState = "idle"
	StateWaitingForLLM     SessionState = "waiting_for_llm"
	StateRunningTools      SessionState = "running_tools"
	StateWaitingForApproval SessionState = "waiting_for_approval"
)

// SessionStatus contains detailed status information about a session.
type SessionStatus struct {
	State       SessionState
	CurrentTool string    // Name of the tool currently running (if any)
	LLMModel    string    // Model being used (if known)
	StartTime   time.Time // When the current operation started
	Message     string    // Additional status message
}

// sessionStateManager tracks the state of active sessions.
type sessionStateManager struct {
	mu     sync.RWMutex
	status map[int64]*SessionStatus
}

func newSessionStateManager() *sessionStateManager {
	return &sessionStateManager{
		status: make(map[int64]*SessionStatus),
	}
}

func (m *sessionStateManager) SetState(sessionID int64, state SessionState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status[sessionID]
	if s == nil {
		s = &SessionStatus{StartTime: time.Now()}
		m.status[sessionID] = s
	}
	s.State = state
	if state == StateIdle {
		s.CurrentTool = ""
		s.Message = ""
	}
}

func (m *sessionStateManager) SetToolRunning(sessionID int64, toolName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status[sessionID]
	if s == nil {
		s = &SessionStatus{StartTime: time.Now()}
		m.status[sessionID] = s
	}
	s.State = StateRunningTools
	s.CurrentTool = toolName
}

func (m *sessionStateManager) SetWaitingForApproval(sessionID int64, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status[sessionID]
	if s == nil {
		s = &SessionStatus{StartTime: time.Now()}
		m.status[sessionID] = s
	}
	s.State = StateWaitingForApproval
	s.Message = message
}

func (m *sessionStateManager) SetLLMModel(sessionID int64, model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status[sessionID]
	if s == nil {
		s = &SessionStatus{StartTime: time.Now()}
		m.status[sessionID] = s
	}
	s.LLMModel = model
}

func (m *sessionStateManager) GetStatus(sessionID int64) *SessionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s := m.status[sessionID]; s != nil {
		result := *s
		return &result
	}
	return &SessionStatus{State: StateIdle}
}

func (m *sessionStateManager) Clear(sessionID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.status, sessionID)
}
