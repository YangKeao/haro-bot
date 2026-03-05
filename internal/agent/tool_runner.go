package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
)

type DefaultToolRunner struct {
	registry *tools.Registry
	store    ConversationStore
	skills   *skills.Manager
	prompts  PromptBuilder
}

func NewToolRunner(registry *tools.Registry, store ConversationStore, skillsMgr *skills.Manager, prompts PromptBuilder) *DefaultToolRunner {
	if prompts == nil {
		prompts = DefaultPromptBuilder{}
	}
	return &DefaultToolRunner{
		registry: registry,
		store:    store,
		skills:   skillsMgr,
		prompts:  prompts,
	}
}

func (r *DefaultToolRunner) Run(ctx context.Context, sessionID, userID int64, baseDir string, activeSkill *skills.Skill, calls []llm.ToolCall) ([]llm.Message, *skills.Skill, error) {
	if r == nil || r.registry == nil {
		return nil, activeSkill, errors.New("tool registry not configured")
	}
	currentSkill := activeSkill
	out := make([]llm.Message, 0, len(calls))
	for _, call := range calls {
		tool, ok := r.registry.Get(call.Function.Name)
		if !ok {
			toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: "unsupported tool"}
			out = append(out, toolMsg)
			continue
		}
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
		}
		toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: output}
		out = append(out, toolMsg)
		_ = r.store.AddMessage(ctx, sessionID, "tool", output, &memory.MessageMetadata{ToolCallID: call.ID, Status: status})
	}
	return out, currentSkill, nil
}
