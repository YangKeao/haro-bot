package agent

import (
	"context"
	"errors"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"go.uber.org/zap"
)

func (s *Session) Interrupt(ctx context.Context, userID int64, input string, modelOverride string, storeInSession bool, metadata *memory.MessageMetadata, messenger SessionMessenger) (content string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log := logging.L().Named("agent_interrupt")
	model := s.deps.model
	if modelOverride != "" {
		model = modelOverride
	}
	hooks := s.deps.hooks
	if storeInSession {
		if _, err := s.deps.store.AddMessageAndGetID(ctx, s.id, "user", input, nil); err != nil {
			return "", err
		}
	}
	run := &RunState{
		SessionID:       s.id,
		UserID:          userID,
		Model:           model,
		Input:           input,
		ShouldIngest:    false,
		AvailableSkills: s.deps.skills.List(),
	}
	defer func() {
		if finalizeErr := executeRunFinalizeHooks(ctx, hooks.RunHooks, run, err); finalizeErr != nil && err == nil {
			err = finalizeErr
		}
	}()

	if err := executeRunBeforePromptHooks(ctx, hooks.RunHooks, run); err != nil {
		return "", err
	}
	systemPrompt := s.deps.promptBuilder.Interrupt(ctx, run.Memories, s.deps.promptFormat)
	pendingInput := ""
	if !storeInSession {
		pendingInput = input
	}
	snapshot, err := loadContextSnapshot(ctx, s.deps.store, s.id, systemPrompt, pendingInput)
	if err != nil {
		return "", err
	}
	snapshot.apply(run)

	turn := newTurnState(run, 1, model, s.estimatorForModel(model), nil)
	resp, err := s.callLLM(ctx, log, turn, hooks, nil)
	if err != nil {
		log.Error("interrupt llm error", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("empty llm response")
	}
	content = resp.Choices[0].Message.Content
	run.Output = content
	if storeInSession {
		if _, err := s.deps.store.AddMessageAndGetID(ctx, s.id, "assistant", content, metadata); err != nil {
			log.Error("interrupt store failed", zap.Int64("session_id", s.id), zap.Error(err))
			return "", err
		}
		if messenger != nil {
			if err := messenger.SendSessionMessage(ctx, s.id, content); err != nil {
				log.Warn("interrupt send failed", zap.Int64("session_id", s.id), zap.Error(err))
			}
		}
	}
	if err := executeRunAfterHooks(ctx, hooks.RunHooks, run); err != nil {
		return "", err
	}
	log.Info("interrupt completed", zap.Int64("session_id", s.id), zap.Bool("stored", storeInSession))
	return content, nil
}
