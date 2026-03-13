package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	"go.uber.org/zap"
)

type Agent struct {
	store        memory.StoreAPI
	sessions     *sessionManager
	stateManager *sessionStateManager
}

func New(store memory.StoreAPI, skills *skills.Manager, toolRegistry *tools.Registry, defaultBaseDir string, maxToolTurns int, llmClient llm.ChatModel, model string, promptFormat string, reasoning llm.ReasoningConfig) *Agent {
	if maxToolTurns <= 0 {
		maxToolTurns = 1024
	}
	estimator, _ := llm.NewTokenEstimator(model)
	toolRunner := NewToolRunner(toolRegistry, store, skills, estimator)
	stateMgr := newSessionStateManager()
	deps := &sessionDeps{
		store:          store,
		skills:         skills,
		toolRegistry:   toolRegistry,
		toolRunner:     toolRunner,
		defaultBaseDir: defaultBaseDir,
		maxToolTurns:   maxToolTurns,
		llm:            llmClient,
		model:          model,
		promptFormat:   promptFormat,
		reasoning:      reasoning,
		tokenEstimator: estimator,
		middleware:     MiddlewareSet{},
	}
	return &Agent{
		store:        store,
		sessions:     newSessionManager(deps),
		stateManager: stateMgr,
	}
}

func (a *Agent) SetMiddleware(middleware MiddlewareSet) {
	a.sessions.deps.middleware = middleware
}

func (a *Agent) SessionStatusWriter() SessionStatusWriter {
	return a.stateManager
}

func (a *Agent) Handle(ctx context.Context, userID int64, channel string, input string) (string, error) {
	return a.handleWithMiddleware(ctx, userID, channel, input, "", MiddlewareSet{})
}

func (a *Agent) HandleWithMiddleware(ctx context.Context, userID int64, channel string, input string, modelOverride string, middleware MiddlewareSet) (string, error) {
	return a.handleWithMiddleware(ctx, userID, channel, input, modelOverride, middleware)
}

func (a *Agent) handleWithMiddleware(ctx context.Context, userID int64, channel string, input string, modelOverride string, middleware MiddlewareSet) (string, error) {
	log := logging.L().Named("agent")
	sessionID, err := a.store.GetOrCreateSession(ctx, userID, channel)
	if err != nil {
		log.Error("get session failed", zap.Error(err))
		return "", err
	}
	session := a.sessions.Get(sessionID)
	defer a.sessions.Release(sessionID)
	return session.Handle(ctx, userID, channel, input, modelOverride, middleware)
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

const internalCheckpointPrefix = "<internal_checkpoint>"

func formatSummaryMessage(summary *memory.Summary) string {
	if summary == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(internalCheckpointPrefix)
	b.WriteString("\nvisibility: hidden")
	b.WriteString("\npolicy: do_not_mention_checkpoint_or_compaction_unless_user_asks")
	if summary.Phase != "" && summary.Phase != "auto-compact" {
		b.WriteString("\nphase: ")
		b.WriteString(summary.Phase)
	}
	if summary.Summary != "" {
		b.WriteString("\nsummary:\n")
		b.WriteString(summary.Summary)
	} else if len(summary.State) > 0 {
		b.WriteString("\nstate_json:\n")
		if data, err := json.Marshal(summary.State); err == nil {
			b.WriteString(string(data))
		}
	}
	b.WriteString("\n</internal_checkpoint>")
	return strings.TrimSpace(b.String())
}
