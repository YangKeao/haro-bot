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
	registry *tools.Registry
	store    ConversationStore
	skills   *skills.Manager
	prompts  PromptBuilder
}

func NewToolRunner(registry *tools.Registry, store ConversationStore, skillsMgr *skills.Manager, prompts PromptBuilder) *DefaultToolRunner {
	if prompts == nil {
		prompts = NewDefaultPromptBuilder(nil)
	}
	return &DefaultToolRunner{
		registry: registry,
		store:    store,
		skills:   skillsMgr,
		prompts:  prompts,
	}
}

func (r *DefaultToolRunner) Run(ctx context.Context, sessionID, userID int64, baseDir string, activeSkill *skills.Skill, calls []llm.ToolCall) ([]llm.Message, *skills.Skill, error) {
	log := logging.L().Named("tool_runner")
	if r == nil || r.registry == nil {
		return nil, activeSkill, errors.New("tool registry not configured")
	}
	currentSkill := activeSkill
	out := make([]llm.Message, 0, len(calls))
	for _, call := range calls {
		tool, ok := r.registry.Get(call.Function.Name)
		if !ok {
			log.Warn("tool not found", zap.String("tool", call.Function.Name), zap.Int64("session_id", sessionID))
			toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: "unsupported tool"}
			out = append(out, toolMsg)
			continue
		}
		log.Debug("tool start", zap.String("tool", call.Function.Name), zap.Int64("session_id", sessionID))
		toolCtx := tools.ToolContext{
			SessionID: sessionID,
			UserID:    userID,
			BaseDir:   baseDir,
		}
		if currentSkill != nil {
			toolCtx.BaseDir = currentSkill.Metadata.Dir
			toolCtx.SkillName = currentSkill.Metadata.Name
		}
		output, err := tool.Execute(ctx, toolCtx, json.RawMessage(call.Function.Arguments))
		status := "ok"
		if err != nil {
			if errors.Is(err, tools.ErrApprovalStopped) {
				if output == "" {
					output = "operation stopped by user"
				}
				_ = r.store.AddMessage(ctx, sessionID, "tool", output, &memory.MessageMetadata{ToolCallID: call.ID, Status: "error"})
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
		toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: output}
		out = append(out, toolMsg)
		_ = r.store.AddMessage(ctx, sessionID, "tool", output, &memory.MessageMetadata{ToolCallID: call.ID, Status: status})
	}
	return out, currentSkill, nil
}
