package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	"go.uber.org/zap"
)

type Agent struct {
	store          ConversationStore
	skills         *skills.Manager
	toolRegistry   *tools.Registry
	promptBuilder  PromptBuilder
	toolRunner     ToolRunner
	defaultBaseDir string
	maxToolTurns   int
	llm            llm.ChatModel
	model          string
	promptFormat   string
	reasoning      llm.ReasoningConfig
	sessions       *sessionManager
	stateManager   *sessionStateManager
	messenger      SessionMessenger
}

func New(store memory.StoreAPI, memoryEngine *memory.Engine, skills *skills.Manager, toolRegistry *tools.Registry, guidelinesMgr *guidelines.Manager, defaultBaseDir string, maxToolTurns int, llmClient llm.ChatModel, model string, promptFormat string, reasoning llm.ReasoningConfig, contextConfig llm.ContextConfig) *Agent {
	if maxToolTurns <= 0 {
		maxToolTurns = 1024
	}
	promptBuilder := NewDefaultPromptBuilder(guidelinesMgr)
	toolRunner := NewToolRunner(toolRegistry, store, skills, promptBuilder)
	estimator, _ := llm.NewTokenEstimator(model)
	stateMgr := newSessionStateManager()
	deps := &sessionDeps{
		store:          store,
		skills:         skills,
		toolRegistry:   toolRegistry,
		promptBuilder:  promptBuilder,
		toolRunner:     toolRunner,
		defaultBaseDir: defaultBaseDir,
		maxToolTurns:   maxToolTurns,
		llm:            llmClient,
		model:          model,
		promptFormat:   promptFormat,
		reasoning:      reasoning,
		tokenEstimator: estimator,
	}
	return &Agent{
		store:          store,
		skills:         skills,
		toolRegistry:   toolRegistry,
		promptBuilder:  promptBuilder,
		toolRunner:     toolRunner,
		defaultBaseDir: defaultBaseDir,
		maxToolTurns:   maxToolTurns,
		llm:            llmClient,
		model:          model,
		promptFormat:   promptFormat,
		reasoning:      reasoning,
		sessions:       newSessionManager(deps),
		stateManager:   stateMgr,
	}
}

func (a *Agent) SetHooks(hooks HookSet) {
	a.sessions.deps.hooks = hooks
}

func (a *Agent) SessionStatusWriter() SessionStatusWriter {
	return a.stateManager
}

// SetSessionMessenger registers a messenger for out-of-band session notifications (e.g., Telegram).
func (a *Agent) SetSessionMessenger(messenger SessionMessenger) {
	a.messenger = messenger
}

func (a *Agent) Handle(ctx context.Context, userID int64, channel string, input string) (string, error) {
	return a.handleWithHooks(ctx, userID, channel, input, "", HookSet{})
}

func (a *Agent) HandleWithModel(ctx context.Context, userID int64, channel string, input string, modelOverride string) (string, error) {
	return a.handleWithHooks(ctx, userID, channel, input, modelOverride, HookSet{})
}

func (a *Agent) HandleWithHooks(ctx context.Context, userID int64, channel string, input string, modelOverride string, hooks HookSet) (string, error) {
	return a.handleWithHooks(ctx, userID, channel, input, modelOverride, hooks)
}

func (a *Agent) handleWithHooks(ctx context.Context, userID int64, channel string, input string, modelOverride string, hooks HookSet) (string, error) {
	log := logging.L().Named("agent")
	sessionID, err := a.store.GetOrCreateSession(ctx, userID, channel)
	if err != nil {
		log.Error("get session failed", zap.Error(err))
		return "", err
	}
	session := a.sessions.Get(sessionID)
	defer a.sessions.Release(sessionID)
	return session.Handle(ctx, userID, channel, input, modelOverride, hooks)
}

// InterruptSession generates a response from an existing session context without using tools.
// If storeInSession is true, the interrupt message and response are persisted to the session.
func (a *Agent) InterruptSession(ctx context.Context, sessionID int64, userID int64, input string, modelOverride string, storeInSession bool, metadata *memory.MessageMetadata) (string, error) {
	session := a.sessions.Get(sessionID)
	defer a.sessions.Release(sessionID)
	return session.Interrupt(ctx, userID, input, modelOverride, storeInSession, metadata, a.messenger)
}

// GetSessionStatus returns the current status of a session.
func (a *Agent) GetSessionStatus(sessionID int64) *SessionStatus {
	return a.stateManager.GetStatus(sessionID)
}

// CancelSession cancels any ongoing operation for the session.
// Returns true if there was an operation to cancel.
func (a *Agent) CancelSession(sessionID int64) bool {
	return a.sessions.Cancel(sessionID)
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
