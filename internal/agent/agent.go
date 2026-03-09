package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Agent is the main conversation agent that handles user interactions.
type Agent struct {
	deps      *Deps
	sessions  *sessionManager
	messenger SessionMessenger
}

// Params contains all dependencies for creating an Agent.
// This struct is designed for fx dependency injection via fx.Annotate or struct tags.
type Params struct {
	fx.In

	Store          ConversationStore
	MemoryEngine   *memory.Engine
	Skills         *skills.Manager
	ToolRegistry   *tools.Registry
	GuidelinesMgr  *guidelines.Manager
	DefaultBaseDir string `name:"default_base_dir"`
	MaxToolTurns   int    `name:"max_tool_turns"`
	LLMClient      *llm.Client
	Model          string `name:"llm_model"`
	PromptFormat   string `name:"prompt_format"`
	Reasoning      llm.ReasoningConfig
	ContextConfig  llm.ContextConfig
}

// New creates a new Agent with the given dependencies.
// It initializes all internal components using fx-style dependency injection.
func New(p Params) *Agent {
	maxToolTurns := p.MaxToolTurns
	if maxToolTurns <= 0 {
		maxToolTurns = 1024
	}

	promptBuilder := NewDefaultPromptBuilder(p.GuidelinesMgr)
	estimator, _ := llm.NewTokenEstimator(p.Model)
	stateMgr := newSessionStateManager()
	toolRunner := NewToolRunner(p.ToolRegistry, p.Store, p.Skills, promptBuilder)

	deps := &Deps{
		Store:          p.Store,
		MemoryEngine:   p.MemoryEngine,
		Skills:         p.Skills,
		ToolRegistry:   p.ToolRegistry,
		PromptBuilder:  promptBuilder,
		ToolRunner:     toolRunner,
		DefaultBaseDir: p.DefaultBaseDir,
		MaxToolTurns:   maxToolTurns,
		LLM:            p.LLMClient,
		Model:          p.Model,
		PromptFormat:   p.PromptFormat,
		Reasoning:      p.Reasoning,
		ContextConfig:  p.ContextConfig,
		TokenEstimator: estimator,
		StateManager:   stateMgr,
	}

	return &Agent{
		deps:     deps,
		sessions: newSessionManager(deps),
	}
}

// SetSessionMessenger registers a messenger for out-of-band session notifications (e.g., Telegram).
func (a *Agent) SetSessionMessenger(messenger SessionMessenger) {
	if a == nil {
		return
	}
	a.messenger = messenger
}

func (a *Agent) Handle(ctx context.Context, userID int64, channel string, input string) (string, error) {
	return a.handleWithObserver(ctx, userID, channel, input, "", nil)
}

func (a *Agent) HandleWithModel(ctx context.Context, userID int64, channel string, input string, modelOverride string) (string, error) {
	return a.handleWithObserver(ctx, userID, channel, input, modelOverride, nil)
}

func (a *Agent) HandleWithObserver(ctx context.Context, userID int64, channel string, input string, modelOverride string, observer ProgressObserver) (string, error) {
	return a.handleWithObserver(ctx, userID, channel, input, modelOverride, observer)
}

func (a *Agent) handleWithObserver(ctx context.Context, userID int64, channel string, input string, modelOverride string, observer ProgressObserver) (string, error) {
	log := logging.L().Named("agent")
	sessionID, err := a.deps.Store.GetOrCreateSession(ctx, userID, channel)
	if err != nil {
		log.Error("get session failed", zap.Error(err))
		return "", err
	}
	if a.sessions == nil {
		log.Error("session manager not configured", zap.Int64("session_id", sessionID))
		return "", errors.New("session manager not configured")
	}
	session := a.sessions.Get(sessionID)
	defer a.sessions.Release(sessionID)
	return session.Handle(ctx, userID, channel, input, modelOverride, observer)
}

// InterruptSession generates a response from an existing session context without using tools.
// If storeInSession is true, the interrupt message and response are persisted to the session.
func (a *Agent) InterruptSession(ctx context.Context, sessionID int64, userID int64, input string, modelOverride string, storeInSession bool, metadata *memory.MessageMetadata) (string, error) {
	log := logging.L().Named("agent_interrupt")
	if a.sessions == nil {
		log.Error("session manager not configured", zap.Int64("session_id", sessionID))
		return "", errors.New("session manager not configured")
	}
	session := a.sessions.Get(sessionID)
	defer a.sessions.Release(sessionID)
	return session.Interrupt(ctx, userID, input, modelOverride, storeInSession, metadata, a.messenger)
}

// GetSessionStatus returns the current status of a session.
func (a *Agent) GetSessionStatus(sessionID int64) *SessionStatus {
	if a == nil || a.deps == nil || a.deps.StateManager == nil {
		return &SessionStatus{State: StateIdle}
	}
	return a.deps.StateManager.GetStatus(sessionID)
}

// SetSessionState updates the state of a session.
func (a *Agent) SetSessionState(sessionID int64, state SessionState) {
	if a != nil && a.deps != nil && a.deps.StateManager != nil {
		a.deps.StateManager.SetState(sessionID, state)
	}
}

// SetSessionToolRunning marks a session as running a specific tool.
func (a *Agent) SetSessionToolRunning(sessionID int64, toolName string) {
	if a != nil && a.deps != nil && a.deps.StateManager != nil {
		a.deps.StateManager.SetToolRunning(sessionID, toolName)
	}
}

// SetSessionWaitingForApproval marks a session as waiting for user approval.
func (a *Agent) SetSessionWaitingForApproval(sessionID int64, message string) {
	if a != nil && a.deps != nil && a.deps.StateManager != nil {
		a.deps.StateManager.SetWaitingForApproval(sessionID, message)
	}
}

// CancelSession cancels any ongoing operation for the session.
// Returns true if there was an operation to cancel.
func (a *Agent) CancelSession(sessionID int64) bool {
	if a == nil || a.deps == nil || a.sessions == nil {
		return false
	}
	return a.sessions.Cancel(sessionID)
}

func toLLMMessages(msgs []memory.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, toLLMMessage(m))
	}
	return out
}

func toLLMMessage(m memory.Message) llm.Message {
	llmMsg := llm.Message{Role: m.Role, Content: m.Content}
	if m.Metadata != nil {
		if m.Metadata.ToolCallID != "" {
			llmMsg.ToolCallID = m.Metadata.ToolCallID
		}
		if len(m.Metadata.ToolCalls) > 0 {
			llmMsg.ToolCalls = m.Metadata.ToolCalls
		}
		if m.Metadata.ReasoningContent != "" {
			llmMsg.ReasoningContent = m.Metadata.ReasoningContent
		}
	}
	return llmMsg
}

func formatSummaryMessage(summary *memory.Summary) string {
	if summary == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Session summary")
	if summary.Phase != "" {
		b.WriteString(" (phase: ")
		b.WriteString(summary.Phase)
		b.WriteString(")")
	}
	b.WriteString(":\n")
	if summary.Summary != "" {
		b.WriteString(summary.Summary)
	} else if len(summary.State) > 0 {
		if data, err := json.Marshal(summary.State); err == nil {
			b.WriteString(string(data))
		}
	}
	return strings.TrimSpace(b.String())
}
