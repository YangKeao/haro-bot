package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	"go.uber.org/zap"
)

type DefaultToolRunner struct {
	registry  *tools.Registry
	store     ConversationStore
	skills    *skills.Manager
	prompts   PromptBuilder
	estimator *llm.TokenEstimator
}

func NewToolRunner(registry *tools.Registry, store ConversationStore, skillsMgr *skills.Manager, prompts PromptBuilder, estimator *llm.TokenEstimator) *DefaultToolRunner {
	if prompts == nil {
		prompts = NewDefaultPromptBuilder(nil)
	}
	return &DefaultToolRunner{
		registry:  registry,
		store:     store,
		skills:    skillsMgr,
		prompts:   prompts,
		estimator: estimator,
	}
}

func (r *DefaultToolRunner) Run(ctx context.Context, sessionID, userID int64, baseDir string, activeSkill *skills.Skill, calls []llm.ToolCall) ([]ContextMessage, *skills.Skill, error) {
	log := logging.L().Named("tool_runner")
	if r == nil || r.registry == nil {
		return nil, activeSkill, errors.New("tool registry not configured")
	}
	currentSkill := activeSkill
	out := make([]ContextMessage, 0, len(calls))
	for _, call := range calls {
		tool, ok := r.registry.Get(call.Function.Name)
		if !ok {
			log.Warn("tool not found", zap.String("tool", call.Function.Name), zap.Int64("session_id", sessionID))
			toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: "unsupported tool"}
			entryID, err := r.store.AddMessageAndGetID(ctx, sessionID, "tool", toolMsg.Content, &memory.MessageMetadata{
				ToolCallID: call.ID,
				Status:     "error",
			})
			if err != nil {
				return nil, currentSkill, err
			}
			ctxMsg, err := newPersistedContextMessage(entryID, toolMsg)
			if err != nil {
				return nil, currentSkill, err
			}
			out = append(out, ctxMsg)
			continue
		}
		log.Debug("tool start", zap.String("tool", call.Function.Name), zap.Int64("session_id", sessionID))
		tc := tools.ToolContext{
			SessionID: sessionID,
			UserID:    userID,
			BaseDir:   baseDir,
		}
		if currentSkill != nil {
			tc.BaseDir = currentSkill.Metadata.Dir
			tc.SkillName = currentSkill.Metadata.Name
		}
		output, err := tool.Execute(ctx, tc, json.RawMessage(call.Function.Arguments))
		status := "ok"
		if err != nil {
			if errors.Is(err, tools.ErrApprovalStopped) {
				if output == "" {
					output = "operation stopped by user"
				}
				if _, storeErr := r.store.AddMessageAndGetID(ctx, sessionID, "tool", output, &memory.MessageMetadata{
					ToolCallID: call.ID,
					Status:     "error",
				}); storeErr != nil {
					return nil, currentSkill, storeErr
				}
				log.Warn("tool stopped", zap.String("tool", call.Function.Name), zap.Int64("session_id", sessionID), zap.Error(err))
				return nil, currentSkill, err
			}
			status = "error"
			if output == "" {
				output = "error: " + err.Error()
			} else {
				output = "error: " + err.Error() + "\n" + output
			}
			log.Warn("tool error", zap.String("tool", call.Function.Name), zap.Int64("session_id", sessionID), zap.Error(err))
		} else {
			log.Debug("tool ok", zap.String("tool", call.Function.Name), zap.Int64("session_id", sessionID))
		}
		truncated := truncateToolOutputForModel(r.estimator, output)
		if truncated != output {
			log.Debug("tool output truncated",
				zap.String("tool", call.Function.Name),
				zap.Int64("session_id", sessionID),
				zap.Int("original_tokens", r.estimator.CountTokens(output)),
				zap.Int("truncated_tokens", r.estimator.CountTokens(truncated)),
			)
			output = truncated
		}
		toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: output}
		entryID, err := r.store.AddMessageAndGetID(ctx, sessionID, "tool", output, &memory.MessageMetadata{
			ToolCallID: call.ID,
			Status:     status,
		})
		if err != nil {
			return nil, currentSkill, err
		}
		ctxMsg, err := newPersistedContextMessage(entryID, toolMsg)
		if err != nil {
			return nil, currentSkill, err
		}
		out = append(out, ctxMsg)
	}
	return out, currentSkill, nil
}
