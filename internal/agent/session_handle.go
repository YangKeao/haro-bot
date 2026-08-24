package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"go.uber.org/zap"
)

func (s *Session) Handle(ctx context.Context, userID int64, channel string, input string, modelOverride string, metadata *memory.MessageMetadata, extraHooks MiddlewareSet) (output string, err error) {
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
	userEntryID, err := s.deps.store.AddMessageAndGetID(ctx, s.id, "user", input, metadata)
	if err != nil {
		log.Error("add user message failed", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}
	s.deps.toolRegistry.ResetSession(s.id)

	middleware := mergeMiddlewareSets(s.deps.middleware, extraHooks)
	availableSkills := s.deps.skills.List()
	if s.deps.allowedSkills != nil {
		filtered := availableSkills[:0]
		for _, skill := range availableSkills {
			if _, ok := s.deps.allowedSkills[skill.Name]; ok {
				filtered = append(filtered, skill)
			}
		}
		availableSkills = filtered
	}
	run := &RunState{
		SessionID:         s.id,
		TurnStartEntryID:  userEntryID,
		Model:             model,
		PromptFormat:      s.deps.promptFormat,
		AvailableSkills:   availableSkills,
		AgentInstructions: s.deps.instructions,
	}
	output, err = executeRunMiddleware(ctx, middleware.RunMiddleware, run, func(ctx context.Context, run *RunState) (string, error) {
		snapshot, err := loadContextSnapshot(ctx, s.deps.store, s.id, run.Prompt)
		if err != nil {
			return "", err
		}
		snapshot.apply(run)
		output, err := s.runLoop(ctx, run, middleware)
		if err != nil {
			return "", err
		}
		return output, nil
	})
	if err != nil {
		log.Error("handle failed", zap.Int64("session_id", s.id), zap.Error(err))
		return "", err
	}
	log.Info("handle completed", zap.Int64("session_id", s.id))
	return output, nil
}
