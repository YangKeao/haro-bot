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

const (
	toolActivateSkill = "activate_skill"
)

type Agent struct {
	store          *memory.Store
	skills         *skills.Manager
	toolRegistry   *tools.Registry
	defaultBaseDir string
	maxToolTurns   int
	llm            *llm.Client
	model          string
	promptFormat   string
	maxContext     int
}

func New(store *memory.Store, skills *skills.Manager, toolRegistry *tools.Registry, defaultBaseDir string, maxToolTurns int, llmClient *llm.Client, model string, promptFormat string) *Agent {
	if maxToolTurns <= 0 {
		maxToolTurns = 1024
	}
	return &Agent{
		store:          store,
		skills:         skills,
		toolRegistry:   toolRegistry,
		defaultBaseDir: defaultBaseDir,
		maxToolTurns:   maxToolTurns,
		llm:            llmClient,
		model:          model,
		promptFormat:   promptFormat,
		maxContext:     20,
	}
}

func (a *Agent) Handle(ctx context.Context, userID int64, channel string, input string) (string, error) {
	return a.HandleWithModel(ctx, userID, channel, input, "")
}

func (a *Agent) HandleWithModel(ctx context.Context, userID int64, channel string, input string, modelOverride string) (string, error) {
	model := a.model
	if modelOverride != "" {
		model = modelOverride
	}
	sessionID, err := a.store.GetOrCreateSession(ctx, userID, channel)
	if err != nil {
		return "", err
	}
	if err := a.store.AddMessage(ctx, sessionID, "user", input, nil); err != nil {
		return "", err
	}

	recent, err := a.store.LoadRecentMessages(ctx, sessionID, a.maxContext)
	if err != nil {
		return "", err
	}
	memories, err := a.store.LoadLongMemories(ctx, userID, 8)
	if err != nil {
		return "", err
	}
	availableSkills := a.skills.List()

	systemPrompt := buildSystemPrompt(memories, availableSkills, a.promptFormat)
	messages := []llm.Message{{Role: "system", Content: systemPrompt}}
	messages = append(messages, toLLMMessages(recent)...) // includes user input
	return a.runLoop(ctx, sessionID, userID, messages, model, nil)
}

type activateSkillArgs struct {
	Name string `json:"name"`
	Goal string `json:"goal"`
}

func (a *Agent) runLoop(ctx context.Context, sessionID int64, userID int64, messages []llm.Message, model string, activeSkill *skills.Skill) (string, error) {
	maxTurns := a.maxToolTurns
	for i := 0; i < maxTurns; i++ {
		tools := a.toolsFor()
		resp, err := a.llm.Chat(ctx, llm.ChatRequest{
			Model:    model,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("empty llm response")
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			if err := a.store.AddMessage(ctx, sessionID, "assistant", msg.Content, nil); err != nil {
				return "", err
			}
			return msg.Content, nil
		}

		if err := a.store.AddMessage(ctx, sessionID, "assistant", msg.Content, map[string]any{"tool_calls": msg.ToolCalls}); err != nil {
			return "", err
		}

		toolMsgs, updatedSkill, err := a.handleTools(ctx, sessionID, userID, activeSkill, msg.ToolCalls)
		if err != nil {
			return "", err
		}
		activeSkill = updatedSkill
		messages = append(messages, msg)
		messages = append(messages, toolMsgs...)
	}
	return "", errors.New("tool loop exceeded")
}

func (a *Agent) handleTools(ctx context.Context, sessionID int64, userID int64, activeSkill *skills.Skill, calls []llm.ToolCall) ([]llm.Message, *skills.Skill, error) {
	var out []llm.Message
	currentSkill := activeSkill
	for _, call := range calls {
		if a.toolRegistry == nil {
			return nil, currentSkill, errors.New("tool registry not configured")
		}
		tool, ok := a.toolRegistry.Get(call.Function.Name)
		if !ok {
			toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: "unsupported tool"}
			out = append(out, toolMsg)
			continue
		}
		tc := tools.ToolContext{
			SessionID: sessionID,
			UserID:    userID,
			BaseDir:   a.defaultBaseDir,
		}
		skillName := ""
		if currentSkill != nil {
			tc.BaseDir = currentSkill.Metadata.Dir
			skillName = currentSkill.Metadata.Name
			tc.SkillName = skillName
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
		if call.Function.Name == toolActivateSkill && err == nil {
			var args activateSkillArgs
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				return nil, currentSkill, err
			}
			if args.Name == "" {
				return nil, currentSkill, errors.New("activate_skill missing name")
			}
			skill, err := a.skills.Load(args.Name)
			if err != nil {
				return nil, currentSkill, err
			}
			if err := a.store.RecordSkillCall(ctx, sessionID, skill.Metadata.Name, args, map[string]any{"status": "activated"}, "activated"); err != nil {
				return nil, currentSkill, err
			}
			out = append(out, llm.Message{Role: "user", Content: buildSkillPrompt(skill)})
			currentSkill = &skill
		} else if currentSkill != nil {
			_ = a.store.RecordSkillCall(ctx, sessionID, skillName, map[string]any{"arguments": call.Function.Arguments}, map[string]any{"output": output}, status)
		}
		_ = a.store.AddMessage(ctx, sessionID, "tool", output, map[string]any{"tool_call_id": call.ID, "status": status})
	}
	return out, currentSkill, nil
}

func findToolCall(calls []llm.ToolCall, name string) *llm.ToolCall {
	for i := range calls {
		if calls[i].Function.Name == name {
			return &calls[i]
		}
	}
	return nil
}

func toLLMMessages(msgs []memory.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, llm.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

func (a *Agent) toolsFor() []llm.Tool {
	if a.toolRegistry == nil {
		return nil
	}
	var tools []llm.Tool
	for _, t := range a.toolRegistry.List() {
		tools = append(tools, llm.Tool{
			Type: "function",
			Function: llm.FunctionSpec{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return tools
}
