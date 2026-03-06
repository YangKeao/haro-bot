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
	tokenEstimator *llm.TokenEstimator
}

func New(store *memory.Store, skills *skills.Manager, toolRegistry *tools.Registry, defaultBaseDir string, maxToolTurns int, llmClient *llm.Client, model string, promptFormat string, reasoning llm.ReasoningConfig, contextConfig llm.ContextConfig) *Agent {
	if maxToolTurns <= 0 {
		maxToolTurns = 1024
	}
	promptBuilder := DefaultPromptBuilder{}
	toolRunner := NewToolRunner(toolRegistry, store, skills, promptBuilder)
	estimator, _ := llm.NewTokenEstimator(model)
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
		tokenEstimator: estimator,
	}
}

func (a *Agent) Handle(ctx context.Context, userID int64, channel string, input string) (string, error) {
	return a.HandleWithModel(ctx, userID, channel, input, "")
}

func (a *Agent) HandleWithModel(ctx context.Context, userID int64, channel string, input string, modelOverride string) (string, error) {
	log := logging.L().Named("agent")
	model := a.model
	if modelOverride != "" {
		model = modelOverride
	}
	sessionID, err := a.store.GetOrCreateSession(ctx, userID, channel)
	if err != nil {
		log.Error("get session failed", zap.Error(err))
		return "", err
	}
	log.Info("handle start", zap.Int64("session_id", sessionID), zap.Int64("user_id", userID), zap.String("channel", channel))
	if err := a.store.AddMessage(ctx, sessionID, "user", input, nil); err != nil {
		log.Error("add user message failed", zap.Int64("session_id", sessionID), zap.Error(err))
		return "", err
	}

	memories, err := a.store.LoadLongMemories(ctx, userID, 8)
	if err != nil {
		return "", err
	}
	availableSkills := a.skills.List()
	recent, anchor, err := a.store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		return "", err
	}
	systemPrompt := a.promptBuilder.System(memories, availableSkills, a.promptFormat)
	baseMessages := []llm.Message{{Role: "system", Content: systemPrompt}}
	if anchorMsg := formatAnchorMessage(anchor); anchorMsg != "" {
		baseMessages = append(baseMessages, llm.Message{Role: "system", Content: anchorMsg})
	}
	estimator := a.estimatorForModel(model)
	budget := computeTokenBudget(a.contextConfig)
	baseTokens := estimator.CountMessages(baseMessages)
	available := budget.InputBudget - baseTokens
	if available < 0 {
		available = 0
	}
	selected, selectedTokens := selectMessagesByTokens(recent, estimator, available)
	usage := anchorUsage{TokensUsed: baseTokens + selectedTokens, TokenBudget: budget.AnchorBudget}
	messages := baseMessages
	if hint := anchorHint(selected, usage); hint != "" {
		messages = append(messages, llm.Message{Role: "system", Content: hint})
	}
	messages = append(messages, toLLMMessages(selected)...) // includes user input
	output, err := a.runLoop(ctx, sessionID, userID, messages, model, nil)
	if err != nil {
		log.Error("handle failed", zap.Int64("session_id", sessionID), zap.Error(err))
		return "", err
	}
	log.Info("handle completed", zap.Int64("session_id", sessionID))
	return output, nil
}

// InterruptSession generates a response from an existing session context without using tools.
// If storeInSession is true, the interrupt message and response are persisted to the session.
func (a *Agent) InterruptSession(ctx context.Context, sessionID int64, userID int64, input string, modelOverride string, storeInSession bool) (string, error) {
	log := logging.L().Named("agent_interrupt")
	model := a.model
	if modelOverride != "" {
		model = modelOverride
	}
	if storeInSession {
		if err := a.store.AddMessage(ctx, sessionID, "user", input, nil); err != nil {
			return "", err
		}
	}
	memories, err := a.store.LoadLongMemories(ctx, userID, 8)
	if err != nil {
		return "", err
	}
	systemPrompt := a.promptBuilder.Interrupt(memories, a.promptFormat)
	recent, anchor, err := a.store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		return "", err
	}
	baseMessages := []llm.Message{{Role: "system", Content: systemPrompt}}
	if anchorMsg := formatAnchorMessage(anchor); anchorMsg != "" {
		baseMessages = append(baseMessages, llm.Message{Role: "system", Content: anchorMsg})
	}
	estimator := a.estimatorForModel(model)
	budget := computeTokenBudget(a.contextConfig)
	baseTokens := estimator.CountMessages(baseMessages)
	inputTokens := 0
	if !storeInSession {
		inputTokens = estimator.CountMessage(llm.Message{Role: "user", Content: input})
	}
	available := budget.InputBudget - baseTokens - inputTokens
	if available < 0 {
		available = 0
	}
	selected, selectedTokens := selectMessagesByTokens(recent, estimator, available)
	usage := anchorUsage{TokensUsed: baseTokens + inputTokens + selectedTokens, TokenBudget: budget.AnchorBudget}
	messages := baseMessages
	if hint := anchorHint(selected, usage); hint != "" {
		messages = append(messages, llm.Message{Role: "system", Content: hint})
	}
	messages = append(messages, toLLMMessages(selected)...)
	if !storeInSession {
		messages = append(messages, llm.Message{Role: "user", Content: input})
	}
	resp, err := a.llm.Chat(ctx, llm.ChatRequest{
		Model:            model,
		Messages:         messages,
		ReasoningEnabled: a.reasoning.Enabled,
		ReasoningEffort:  a.reasoning.Effort,
	})
	if err != nil {
		log.Error("interrupt llm error", zap.Int64("session_id", sessionID), zap.Error(err))
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("empty llm response")
	}
	content := resp.Choices[0].Message.Content
	if storeInSession {
		if err := a.store.AddMessage(ctx, sessionID, "assistant", content, nil); err != nil {
			log.Error("interrupt store failed", zap.Int64("session_id", sessionID), zap.Error(err))
			return "", err
		}
	}
	log.Info("interrupt completed", zap.Int64("session_id", sessionID), zap.Bool("stored", storeInSession))
	return content, nil
}

func (a *Agent) runLoop(ctx context.Context, sessionID int64, userID int64, messages []llm.Message, model string, activeSkill *skills.Skill) (string, error) {
	log := logging.L().Named("agent_loop")
	maxTurns := a.maxToolTurns
	for i := 0; i < maxTurns; i++ {
		log.Debug("loop turn",
			zap.Int("turn", i+1),
			zap.Int("max_turns", maxTurns),
			zap.Int("message_count", len(messages)),
			zap.String("model", model),
		)
		tools := a.toolsFor()
		log.Debug("tools prepared", zap.Int("count", len(tools)))
		resp, err := a.llm.Chat(ctx, llm.ChatRequest{
			Model:            model,
			Messages:         messages,
			Tools:            tools,
			ReasoningEnabled: a.reasoning.Enabled,
			ReasoningEffort:  a.reasoning.Effort,
		})
		if err != nil {
			log.Error("llm chat error", zap.Int64("session_id", sessionID), zap.Error(err))
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("empty llm response")
		}
		msg := resp.Choices[0].Message
		log.Debug("llm response received",
			zap.Int("choices", len(resp.Choices)),
			zap.Int("tool_calls", len(msg.ToolCalls)),
		)
		if len(msg.ToolCalls) == 0 {
			if err := a.store.AddMessage(ctx, sessionID, "assistant", msg.Content, nil); err != nil {
				log.Error("store assistant failed", zap.Int64("session_id", sessionID), zap.Error(err))
				return "", err
			}
			log.Debug("assistant response stored", zap.Int64("session_id", sessionID))
			return msg.Content, nil
		}

		log.Debug("tool calls received", zap.Int("count", len(msg.ToolCalls)), zap.Int64("session_id", sessionID))
		if err := a.store.AddMessage(ctx, sessionID, "assistant", msg.Content, &memory.MessageMetadata{ToolCalls: msg.ToolCalls}); err != nil {
			return "", err
		}
		log.Debug("assistant tool-call message stored", zap.Int64("session_id", sessionID))

		toolMsgs, updatedSkill, err := a.toolRunner.Run(ctx, sessionID, userID, a.defaultBaseDir, activeSkill, msg.ToolCalls)
		if err != nil {
			return "", err
		}
		log.Debug("tool run completed", zap.Int("tool_messages", len(toolMsgs)))
		activeSkill = updatedSkill
		messages = append(messages, msg)
		messages = append(messages, toolMsgs...)
	}
	return "", errors.New("tool loop exceeded")
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

func formatAnchorMessage(anchor *memory.Anchor) string {
	if anchor == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Session anchor")
	if anchor.Phase != "" {
		b.WriteString(" (phase: ")
		b.WriteString(anchor.Phase)
		b.WriteString(")")
	}
	b.WriteString(":\n")
	if anchor.Summary != "" {
		b.WriteString(anchor.Summary)
	} else if len(anchor.State) > 0 {
		if data, err := json.Marshal(anchor.State); err == nil {
			b.WriteString(string(data))
		}
	}
	return strings.TrimSpace(b.String())
}

func (a *Agent) toolsFor() []llm.Tool {
	if a.toolRegistry == nil {
		return nil
	}
	var tools []llm.Tool
	for _, t := range a.toolRegistry.List() {
		tools = append(tools, llm.Tool{
			Type: "function",
			Function: llm.FunctionSpec{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return tools
}

func (a *Agent) estimatorForModel(model string) *llm.TokenEstimator {
	if a == nil {
		return nil
	}
	if model == "" || model == a.model {
		if a.tokenEstimator != nil {
			return a.tokenEstimator
		}
		estimator, err := llm.NewTokenEstimator(a.model)
		if err != nil {
			return nil
		}
		return estimator
	}
	estimator, err := llm.NewTokenEstimator(model)
	if err != nil {
		return a.tokenEstimator
	}
	return estimator
}
