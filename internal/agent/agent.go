package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
	llm            *llm.Client
	model          string
	promptFormat   string
	reasoning      llm.ReasoningConfig
	contextConfig  llm.ContextConfig
	sessions       *sessionManager
}

func New(store memory.StoreAPI, skills *skills.Manager, toolRegistry *tools.Registry, defaultBaseDir string, maxToolTurns int, llmClient *llm.Client, model string, promptFormat string, reasoning llm.ReasoningConfig, contextConfig llm.ContextConfig) *Agent {
	if maxToolTurns <= 0 {
		maxToolTurns = 1024
	}
	promptBuilder := DefaultPromptBuilder{}
	toolRunner := NewToolRunner(toolRegistry, store, skills, promptBuilder)
	estimator, _ := llm.NewTokenEstimator(model)
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
		contextConfig:  contextConfig,
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
		contextConfig:  contextConfig,
		sessions:       newSessionManager(deps),
	}
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
	sessionID, err := a.store.GetOrCreateSession(ctx, userID, channel)
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
func (a *Agent) InterruptSession(ctx context.Context, sessionID int64, userID int64, input string, modelOverride string, storeInSession bool) (string, error) {
	log := logging.L().Named("agent_interrupt")
	if a.sessions == nil {
		log.Error("session manager not configured", zap.Int64("session_id", sessionID))
		return "", errors.New("session manager not configured")
	}
	session := a.sessions.Get(sessionID)
	defer a.sessions.Release(sessionID)
	return session.Interrupt(ctx, userID, input, modelOverride, storeInSession)
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
