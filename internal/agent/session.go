package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"go.uber.org/zap"
)

type Session struct {
	id   int64
	refs int
	deps *sessionDeps
	mu   sync.Mutex

	// For session interruption
	cancelMu   sync.Mutex
	cancelFunc context.CancelFunc
}

func (s *Session) setState(state SessionState) {
	if s != nil && s.deps != nil && s.deps.stateManager != nil {
		s.deps.stateManager.SetState(s.id, state)
	}
}

func (s *Session) setToolRunning(toolName string) {
	if s != nil && s.deps != nil && s.deps.stateManager != nil {
		s.deps.stateManager.SetToolRunning(s.id, toolName)
	}
}

func (s *Session) setWaitingForApproval(message string) {
	if s != nil && s.deps != nil && s.deps.stateManager != nil {
		s.deps.stateManager.SetWaitingForApproval(s.id, message)
	}
}

func (s *Session) setCancelFunc(cancel context.CancelFunc) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.cancelFunc = cancel
}

func (s *Session) clearCancelFunc() {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.cancelFunc = nil
}

func (s *Session) cancel() bool {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if s.cancelFunc != nil {
		s.cancelFunc()
		s.cancelFunc = nil
		return true
	}
	return false
}

func (s *Session) Handle(ctx context.Context, userID int64, channel string, input string, modelOverride string, observer ProgressObserver) (string, error) {
	if s == nil || s.deps == nil {
		return "", errors.New("session not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	log := logging.L().Named("agent")
	model := s.deps.model
	if modelOverride != "" {
		model = modelOverride
	}

	// Create a cancellable context for this session operation
	ctx, cancel := context.WithCancel(ctx)
	s.setCancelFunc(cancel)
	defer func() {
		s.clearCancelFunc()
		cancel()
	}()

	s.setState(StateWaitingForLLM)
	s.deps.stateManager.SetLLMModel(s.id, model)
	defer s.setState(StateIdle)
	log.Info("handle start", zap.Int64("session_id", s.id), zap.Int64("user_id", userID), zap.String("channel", channel))
	if err := s.deps.store.AddMessage(ctx, s.id, "user", input, nil); err != nil {
		log.Error("add user message failed", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}

	memories := s.retrieveMemories(ctx, userID, input)
	availableSkills := s.deps.skills.List()
	recent, summary, err := s.deps.store.LoadViewMessages(ctx, s.id, 0)
	if err != nil {
		return "", err
	}
	systemPrompt := s.deps.promptBuilder.System(memories, availableSkills, s.deps.promptFormat)
	baseMessages := []llm.Message{{Role: "system", Content: systemPrompt}}
	if summaryMsg := formatSummaryMessage(summary); summaryMsg != "" {
		baseMessages = append(baseMessages, llm.Message{Role: "system", Content: summaryMsg})
	}
	estimator := s.estimatorForModel(model)
	budget := computeTokenBudget(s.deps.contextConfig)
	llmMessages := toLLMMessages(recent)
	previewMessages := append(append([]llm.Message{}, baseMessages...), llmMessages...)
	budgeter := NewContextBudgeter(estimator, s.deps.contextConfig)
	_, previewInfo := budgeter.Trim(previewMessages, 1.0)
	usage := summaryUsage{TokensUsed: previewInfo.TokensUsed, TokenBudget: budget.SummaryBudget}
	messages := baseMessages
	if hint := summaryHint(recent, usage); hint != "" {
		log.Debug("summary hint",
			zap.Int64("session_id", s.id),
			zap.Int("tokens_used", usage.TokensUsed),
			zap.Int("token_budget", usage.TokenBudget),
		)
		messages = append(messages, llm.Message{Role: "system", Content: hint})
	}
	messages = append(messages, llmMessages...)
	output, err := s.runLoop(ctx, userID, messages, model, nil, observer)
	if err != nil {
		log.Error("handle failed", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}
	s.ingestMemory(userID)
	log.Info("handle completed", zap.Int64("session_id", s.id))
	return output, nil
}

func (s *Session) Interrupt(ctx context.Context, userID int64, input string, modelOverride string, storeInSession bool, metadata *memory.MessageMetadata, messenger SessionMessenger) (string, error) {
	if s == nil || s.deps == nil {
		return "", errors.New("session not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	log := logging.L().Named("agent_interrupt")
	model := s.deps.model
	if modelOverride != "" {
		model = modelOverride
	}
	if storeInSession {
		if err := s.deps.store.AddMessage(ctx, s.id, "user", input, nil); err != nil {
			return "", err
		}
	}
	memories := s.retrieveMemories(ctx, userID, input)
	systemPrompt := s.deps.promptBuilder.Interrupt(memories, s.deps.promptFormat)
	recent, summary, err := s.deps.store.LoadViewMessages(ctx, s.id, 0)
	if err != nil {
		return "", err
	}
	baseMessages := []llm.Message{{Role: "system", Content: systemPrompt}}
	if summaryMsg := formatSummaryMessage(summary); summaryMsg != "" {
		baseMessages = append(baseMessages, llm.Message{Role: "system", Content: summaryMsg})
	}
	estimator := s.estimatorForModel(model)
	budget := computeTokenBudget(s.deps.contextConfig)
	llmMessages := toLLMMessages(recent)
	if !storeInSession {
		llmMessages = append(llmMessages, llm.Message{Role: "user", Content: input})
	}
	previewMessages := append(append([]llm.Message{}, baseMessages...), llmMessages...)
	budgeter := NewContextBudgeter(estimator, s.deps.contextConfig)
	_, previewInfo := budgeter.Trim(previewMessages, 1.0)
	usage := summaryUsage{TokensUsed: previewInfo.TokensUsed, TokenBudget: budget.SummaryBudget}
	messages := baseMessages
	if hint := summaryHint(recent, usage); hint != "" {
		log.Debug("summary hint",
			zap.Int64("session_id", s.id),
			zap.Int("tokens_used", usage.TokensUsed),
			zap.Int("token_budget", usage.TokenBudget),
		)
		messages = append(messages, llm.Message{Role: "system", Content: hint})
	}
	messages = append(messages, llmMessages...)
	resp, _, err := s.callLLMWithTrim(ctx, log, model, messages, nil, nil)
	if err != nil {
		log.Error("interrupt llm error", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("empty llm response")
	}
	content := resp.Choices[0].Message.Content
	if storeInSession {
		if err := s.deps.store.AddMessage(ctx, s.id, "assistant", content, metadata); err != nil {
			log.Error("interrupt store failed", zap.Int64("session_id", s.id), zap.Error(err))
			return "", err
		}
		if messenger != nil {
			if err := messenger.SendSessionMessage(ctx, s.id, content); err != nil {
				log.Warn("interrupt send failed", zap.Int64("session_id", s.id), zap.Error(err))
			}
		}
	}
	log.Info("interrupt completed", zap.Int64("session_id", s.id), zap.Bool("stored", storeInSession))
	return content, nil
}

func (s *Session) runLoop(ctx context.Context, userID int64, messages []llm.Message, model string, activeSkill *skills.Skill, observer ProgressObserver) (string, error) {
	log := logging.L().Named("agent_loop")
	maxTurns := s.deps.maxToolTurns
	for i := 0; i < maxTurns; i++ {
		log.Debug("loop turn",
			zap.Int("turn", i+1),
			zap.Int("max_turns", maxTurns),
			zap.Int("message_count", len(messages)),
			zap.String("model", model),
		)
		tools := s.toolsFor()
		log.Debug("tools prepared", zap.Int("count", len(tools)))
		s.setState(StateWaitingForLLM)
		resp, trimmed, err := s.callLLMWithTrim(ctx, log, model, messages, tools, observer)
		if err != nil {
			log.Error("llm chat error", zap.Int64("session_id", s.id), zap.Error(err))
			return "", err
		}
		messages = trimmed
		if len(resp.Choices) == 0 {
			return "", errors.New("empty llm response")
		}
		msg := resp.Choices[0].Message
		log.Debug("llm response received",
			zap.Int("choices", len(resp.Choices)),
			zap.Int("tool_calls", len(msg.ToolCalls)),
		)
		if len(msg.ToolCalls) == 0 {
			if err := s.deps.store.AddMessage(ctx, s.id, "assistant", msg.Content, nil); err != nil {
				log.Error("store assistant failed", zap.Int64("session_id", s.id), zap.Error(err))
				return "", err
			}
			log.Debug("assistant response stored", zap.Int64("session_id", s.id))
			return msg.Content, nil
		}

		log.Debug("tool calls received", zap.Int("count", len(msg.ToolCalls)), zap.Int64("session_id", s.id))
		if observer != nil {
			observer.OnToolCalls(ctx, msg.ToolCalls, msg.Content)
		}
		if err := s.deps.store.AddMessage(ctx, s.id, "assistant", msg.Content, &memory.MessageMetadata{ToolCalls: msg.ToolCalls}); err != nil {
			return "", err
		}
		log.Debug("assistant tool-call message stored", zap.Int64("session_id", s.id))

		// Set state for each tool being run
		for _, tc := range msg.ToolCalls {
			s.setToolRunning(tc.Function.Name)
		}
		toolMsgs, updatedSkill, err := s.deps.toolRunner.Run(ctx, s.id, userID, s.deps.defaultBaseDir, activeSkill, msg.ToolCalls)
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

func (s *Session) toolsFor() []llm.Tool {
	if s == nil || s.deps == nil || s.deps.toolRegistry == nil {
		return nil
	}
	var tools []llm.Tool
	for _, t := range s.deps.toolRegistry.List() {
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

func (s *Session) estimatorForModel(model string) *llm.TokenEstimator {
	if s == nil || s.deps == nil {
		return nil
	}
	if model == "" || model == s.deps.model {
		if s.deps.tokenEstimator != nil {
			return s.deps.tokenEstimator
		}
		estimator, err := llm.NewTokenEstimator(s.deps.model)
		if err != nil {
			return nil
		}
		return estimator
	}
	estimator, err := llm.NewTokenEstimator(model)
	if err != nil {
		return s.deps.tokenEstimator
	}
	return estimator
}

func (s *Session) retrieveMemories(ctx context.Context, userID int64, query string) []memory.MemoryItem {
	if s == nil || s.deps == nil || s.deps.memoryEngine == nil || !s.deps.memoryEngine.Enabled() {
		return nil
	}
	log := logging.L().Named("memory")
	items, err := s.deps.memoryEngine.Retrieve(ctx, userID, s.id, query, 0)
	if err != nil {
		log.Warn("memory retrieve failed", zap.Error(err))
		return nil
	}
	return items
}

func (s *Session) ingestMemory(userID int64) {
	if s == nil || s.deps == nil || s.deps.memoryEngine == nil || !s.deps.memoryEngine.Enabled() {
		return
	}
	s.deps.memoryEngine.IngestAsync(userID, s.id)
}
