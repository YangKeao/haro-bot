package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
)

func (s *Session) Handle(ctx context.Context, userID int64, channel string, input string, modelOverride string, extraHooks MiddlewareSet) (output string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log := logging.L().Named("agent")
	model := s.deps.model
	if modelOverride != "" {
		model = modelOverride
	}

	ctx, cancel := context.WithCancel(ctx)
	s.setCancelFunc(cancel)
	defer func() {
		s.clearCancelFunc()
		cancel()
	}()

	log.Info("handle start", zap.Int64("session_id", s.id), zap.Int64("user_id", userID), zap.String("channel", channel))
	if _, err := s.deps.store.AddMessageAndGetID(ctx, s.id, "user", input, nil); err != nil {
		log.Error("add user message failed", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}

	middleware := mergeMiddlewareSets(s.deps.middleware, extraHooks)
	run := &RunState{
		SessionID:       s.id,
		UserID:          userID,
		Channel:         channel,
		Model:           model,
		Input:           input,
		PromptMode:      PromptModeHandle,
		PromptFormat:    s.deps.promptFormat,
		ShouldIngest:    true,
		AvailableSkills: s.deps.skills.List(),
	}
	output, err = executeRunMiddleware(ctx, middleware.RunMiddleware, run, func(ctx context.Context, run *RunState) (string, error) {
		snapshot, err := loadContextSnapshot(ctx, s.deps.store, s.id, run.Prompt, run.PendingInput)
		if err != nil {
			return "", err
		}
		snapshot.apply(run)
		output, err := s.runLoop(ctx, run, middleware, nil)
		if err != nil {
			return "", err
		}
		run.Output = output
		return output, nil
	})
	if err != nil {
		log.Error("handle failed", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}
	log.Info("handle completed", zap.Int64("session_id", s.id))
	return output, nil
}
