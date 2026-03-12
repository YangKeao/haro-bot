package agent

import (
	"context"
	"errors"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"go.uber.org/zap"
)

func (s *Session) runLoop(ctx context.Context, run *RunState, hooks MiddlewareSet, activeSkill *skills.Skill) (string, error) {
	log := logging.L().Named("agent_loop")
	maxTurns := s.deps.maxToolTurns
	for i := 0; i < maxTurns; i++ {
		turn := newTurnState(run, i+1, run.Model, s.estimatorForModel(run.Model), s.toolsFor())
		turn.ActiveSkill = activeSkill
		log.Debug("loop turn",
			zap.Int("turn", i+1),
			zap.Int("max_turns", maxTurns),
			zap.Int("message_count", len(composeLLMMessages(turn.Stored, turn.Transient))),
			zap.String("model", turn.Model),
		)
		log.Debug("tools prepared", zap.Int("count", len(turn.Tools)))

		err := executeTurnMiddleware(ctx, hooks.TurnMiddleware, turn, func(ctx context.Context, turn *TurnState) error {
			resp, err := s.callLLM(ctx, log, turn, hooks, turn.Tools)
			if err != nil {
				return err
			}
			run.Stored = turn.Stored
			run.Transient = turn.Transient
			if len(resp.Choices) == 0 {
				return errors.New("empty llm response")
			}
			msg := resp.Choices[0].Message
			log.Debug("llm response received",
				zap.Int("choices", len(resp.Choices)),
				zap.Int("tool_calls", len(msg.ToolCalls)),
			)
			if len(msg.ToolCalls) == 0 {
				if _, err := s.deps.store.AddMessageAndGetID(ctx, s.id, "assistant", msg.Content, nil); err != nil {
					log.Error("store assistant failed", zap.Int64("session_id", s.id), zap.Error(err))
					return err
				}
				if err := executeOutputListeners(ctx, hooks.OutputListeners, turn, msg.Content); err != nil {
					return err
				}
				turn.Output = msg.Content
				turn.Done = true
				log.Debug("assistant response stored", zap.Int64("session_id", s.id))
				return nil
			}

			log.Debug("tool calls received", zap.Int("count", len(msg.ToolCalls)), zap.Int64("session_id", s.id))
			if err := executeToolCallListeners(ctx, hooks.ToolCallListeners, turn, msg); err != nil {
				return err
			}
			assistantEntryID, err := s.deps.store.AddMessageAndGetID(ctx, s.id, "assistant", msg.Content, &memory.MessageMetadata{ToolCalls: msg.ToolCalls})
			if err != nil {
				return err
			}
			assistantMsg, err := newStoredMessage(assistantEntryID, msg)
			if err != nil {
				return err
			}
			log.Debug("assistant tool-call message stored", zap.Int64("session_id", s.id))

			toolMsgs, updatedSkill, err := s.deps.toolRunner.Run(ctx, s.id, run.UserID, s.deps.defaultBaseDir, turn.ActiveSkill, msg.ToolCalls)
			if err != nil {
				return err
			}
			log.Debug("tool run completed", zap.Int("tool_messages", len(toolMsgs)))
			turn.ActiveSkill = updatedSkill
			run.Stored = append(run.Stored, assistantMsg)
			run.Stored = append(run.Stored, toolMsgs...)
			turn.Stored = run.Stored
			return nil
		})
		if err != nil {
			log.Error("llm chat error", zap.Int64("session_id", s.id), zap.Error(err))
			return "", err
		}

		activeSkill = turn.ActiveSkill
		if turn.Done {
			return turn.Output, nil
		}
	}
	return "", errors.New("tool loop exceeded")
}
