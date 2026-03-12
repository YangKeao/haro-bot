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
	middleware := s.deps.middleware
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
		PromptMode:      PromptModeInterrupt,
		PromptFormat:    s.deps.promptFormat,
		ShouldIngest:    false,
		AvailableSkills: s.deps.skills.List(),
	}
	if !storeInSession {
		run.PendingInput = input
	}
	content, err = executeRunMiddleware(ctx, middleware.RunMiddleware, run, func(ctx context.Context, run *RunState) (string, error) {
		snapshot, err := loadContextSnapshot(ctx, s.deps.store, s.id, run.Prompt, run.PendingInput)
		if err != nil {
			return "", err
		}
		snapshot.apply(run)

		turn := newTurnState(run, 1, model, s.estimatorForModel(model), nil)
		resp, err := s.callLLM(ctx, log, turn, middleware, nil)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("empty llm response")
		}
		run.Output = resp.Choices[0].Message.Content
		return run.Output, nil
	})
	if err != nil {
		log.Error("interrupt llm error", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}
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
	log.Info("interrupt completed", zap.Int64("session_id", s.id), zap.Bool("stored", storeInSession))
	return content, nil
}
