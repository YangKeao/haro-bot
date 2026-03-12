package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
)

func (s *Session) Handle(ctx context.Context, userID int64, channel string, input string, modelOverride string, extraHooks HookSet) (output string, err error) {
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

	hooks := mergeHookSets(s.deps.hooks, extraHooks)
	run := &RunState{
		SessionID:       s.id,
		UserID:          userID,
		Channel:         channel,
		Model:           model,
		Input:           input,
		ShouldIngest:    true,
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
	systemPrompt := s.deps.promptBuilder.System(ctx, run.Memories, run.AvailableSkills, s.deps.promptFormat)
	snapshot, err := loadContextSnapshot(ctx, s.deps.store, s.id, systemPrompt, "")
	if err != nil {
		return "", err
	}
	snapshot.apply(run)

	output, err = s.runLoop(ctx, run, hooks, nil)
	if err != nil {
		log.Error("handle failed", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}
	run.Output = output
	if err := executeRunAfterHooks(ctx, hooks.RunHooks, run); err != nil {
		return "", err
	}
	log.Info("handle completed", zap.Int64("session_id", s.id))
	return output, nil
}
