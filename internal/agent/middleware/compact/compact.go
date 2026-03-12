package compact

import (
	"context"
	"errors"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"go.uber.org/zap"
)

type middleware struct {
	store         memory.StoreAPI
	llm           llm.ChatModel
	contextConfig llm.ContextConfig
}

type tokenBudget struct {
	InputBudget int
}

func New(store memory.StoreAPI, chatModel llm.ChatModel, contextConfig llm.ContextConfig) agent.LLMMiddleware {
	return &middleware{
		store:         store,
		llm:           chatModel,
		contextConfig: contextConfig,
	}
}

func (m *middleware) Name() string {
	return "auto_compact"
}

func (m *middleware) Priority() int {
	return 100
}

func (m *middleware) HandleLLM(ctx context.Context, turn *agent.TurnState, call *agent.LLMCall, next agent.LLMHandler) (llm.ChatResponse, error) {
	budget := computeTokenBudget(m.contextConfig)
	compactor := newCompactor(m.store, m.llm, turn.Estimator, turn.Model)
	log := logging.L().Named("compact_middleware")

	for attempt := 0; attempt < 2; attempt++ {
		if budget.InputBudget > 0 && compactor.shouldCompact(turn.LLMMessages(), budget.InputBudget) {
			log.Info("context approaching limit, triggering preemptive compact",
				zap.Int64("session_id", turn.Run.SessionID),
				zap.Int("turn", turn.Index),
				zap.Int("attempt", attempt),
			)
			if err := reloadTurnContext(ctx, log, m.store, compactor, turn, budget.InputBudget); err != nil {
				return llm.ChatResponse{}, err
			}
		}

		resp, err := next(ctx, turn, call)
		if err == nil {
			return resp, nil
		}
		if attempt == 1 || budget.InputBudget <= 0 || !llm.IsContextWindowExceeded(err) {
			return resp, err
		}

		log.Warn("context window exceeded, triggering compact",
			zap.Int64("session_id", turn.Run.SessionID),
			zap.Int("turn", turn.Index),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		if reloadErr := reloadTurnContext(ctx, log, m.store, compactor, turn, budget.InputBudget); reloadErr != nil {
			return resp, reloadErr
		}
	}

	return llm.ChatResponse{}, nil
}

func computeTokenBudget(cfg llm.ContextConfig) tokenBudget {
	effective := cfg.EffectiveWindowTokens()
	autoCompact := cfg.AutoCompactLimit()
	inputBudget := effective
	if inputBudget == 0 && autoCompact > 0 {
		inputBudget = autoCompact
	}
	return tokenBudget{InputBudget: inputBudget}
}

func compactCutoffEntryID(messages []agent.StoredMessage) (int64, error) {
	if len(messages) == 0 {
		return 0, errors.New("no persisted message found in compaction prefix")
	}
	last := messages[len(messages)-1].EntryID
	if last <= 0 {
		return 0, errors.New("invalid compact cutoff entry id")
	}
	return last, nil
}

func reloadTurnContext(ctx context.Context, log *zap.Logger, store memory.StoreAPI, compactor *compactor, turn *agent.TurnState, budget int) error {
	toCompact, _ := selectCompactionPrefixAndTail(turn.Stored)
	if len(toCompact) == 0 {
		return nil
	}
	cutoffEntryID, err := compactCutoffEntryID(toCompact)
	if err != nil {
		return err
	}
	if _, err := compactor.compact(ctx, turn.Run.SessionID, storedMessagesToLLM(toCompact), budget, cutoffEntryID); err != nil {
		return err
	}
	recent, summary, err := store.LoadViewMessages(ctx, turn.Run.SessionID, 0)
	if err != nil {
		return err
	}
	if err := turn.ReloadContext(recent, summary); err != nil {
		return err
	}
	log.Info("context compacted",
		zap.Int64("session_id", turn.Run.SessionID),
		zap.Int("new_stored_count", len(turn.Stored)),
	)
	return nil
}

func selectCompactionPrefixAndTail(view []agent.StoredMessage) (prefix []agent.StoredMessage, tail []agent.StoredMessage) {
	if len(view) == 0 {
		return nil, nil
	}
	start := compactionTailStart(view)
	prefix = cloneStoredMessages(view[:start])
	tail = cloneStoredMessages(view[start:])
	return prefix, tail
}

func compactionTailStart(view []agent.StoredMessage) int {
	if len(view) == 0 {
		return 0
	}

	lastAssistant := lastIndexByRole(view, "assistant", len(view)-1)
	lastUser := lastIndexByRole(view, "user", len(view)-1)

	if lastAssistant == -1 {
		if lastUser == -1 {
			return len(view)
		}
		return lastUser
	}

	if lastUser > lastAssistant {
		return lastUser
	}

	triggerUser := lastIndexByRole(view, "user", lastAssistant-1)
	if triggerUser != -1 {
		return triggerUser
	}
	return lastAssistant
}

func lastIndexByRole(messages []agent.StoredMessage, role string, end int) int {
	if len(messages) == 0 || end < 0 {
		return -1
	}
	if end >= len(messages) {
		end = len(messages) - 1
	}
	for i := end; i >= 0; i-- {
		if messages[i].Message.Role == role {
			return i
		}
	}
	return -1
}

func cloneStoredMessages(in []agent.StoredMessage) []agent.StoredMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]agent.StoredMessage, len(in))
	copy(out, in)
	return out
}

func storedMessagesToLLM(messages []agent.StoredMessage) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.Message)
	}
	return out
}
