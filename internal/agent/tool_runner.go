package agent

import (
	"context"
	"encoding/json"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	"go.uber.org/zap"
)

type DefaultToolRunner struct {
	registry *tools.Registry
	store    ConversationStore
	skills   *skills.Manager
}

func NewToolRunner(registry *tools.Registry, store ConversationStore, skillsMgr *skills.Manager) *DefaultToolRunner {
	return &DefaultToolRunner{
		registry: registry,
		store:    store,
		skills:   skillsMgr,
	}
}

func (r *DefaultToolRunner) Run(ctx context.Context, sessionID, userID int64, baseDir string, activeSkill *skills.Skill, calls []llm.ToolCall) ([]StoredMessage, *skills.Skill, error) {
	log := logging.L().Named("tool_runner")
	currentSkill := activeSkill
	out := make([]StoredMessage, 0, len(calls))
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
			ctxMsg, err := newStoredMessage(entryID, toolMsg)
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
		toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: output}
		entryID, err := r.store.AddMessageAndGetID(ctx, sessionID, "tool", output, &memory.MessageMetadata{
			ToolCallID: call.ID,
			Status:     status,
		})
		if err != nil {
			return nil, currentSkill, err
		}
		ctxMsg, err := newStoredMessage(entryID, toolMsg)
		if err != nil {
			return nil, currentSkill, err
		}
		out = append(out, ctxMsg)
	}
	return out, currentSkill, nil
}
