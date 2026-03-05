package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	llm            *llm.Client
	model          string
	promptFormat   string
	maxContext     int
}

func New(store *memory.Store, skills *skills.Manager, toolRegistry *tools.Registry, defaultBaseDir string, llmClient *llm.Client, model string, promptFormat string) *Agent {
	return &Agent{
		store:          store,
		skills:         skills,
		toolRegistry:   toolRegistry,
		defaultBaseDir: defaultBaseDir,
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
	return a.runGlobalLoop(ctx, sessionID, userID, messages, model)
}

type activateSkillArgs struct {
	Name string `json:"name"`
	Goal string `json:"goal"`
}

func (a *Agent) runSkillLoop(ctx context.Context, sessionID int64, userID int64, skill skills.Skill, messages []llm.Message, model string) (string, error) {
	maxTurns := 3
	tools := a.skillTools(skill)
	for i := 0; i < maxTurns; i++ {
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

		toolMsgs, err := a.handleSkillTools(ctx, sessionID, userID, skill, msg.ToolCalls)
		if err != nil {
			return "", err
		}
		messages = append(messages, msg)
		messages = append(messages, toolMsgs...)
	}
	return "", errors.New("tool loop exceeded")
}

func (a *Agent) runGlobalLoop(ctx context.Context, sessionID int64, userID int64, messages []llm.Message, model string) (string, error) {
	maxTurns := 3
	tools := a.globalTools()
	for i := 0; i < maxTurns; i++ {
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

		activation := findToolCall(msg.ToolCalls, toolActivateSkill)
		if activation != nil {
			var args activateSkillArgs
			if err := json.Unmarshal([]byte(activation.Function.Arguments), &args); err != nil {
				return "", err
			}
			if args.Name == "" {
				return "", errors.New("activate_skill missing name")
			}
			skill, err := a.skills.Load(args.Name)
			if err != nil {
				return "", err
			}
			if err := a.store.RecordSkillCall(ctx, sessionID, skill.Metadata.Name, args, map[string]any{"status": "activated"}, "activated"); err != nil {
				return "", err
			}
			toolOutput := fmt.Sprintf("activated skill: %s", skill.Metadata.Name)
			toolMsg := llm.Message{Role: "tool", ToolCallID: activation.ID, Content: toolOutput}
			if err := a.store.AddMessage(ctx, sessionID, "tool", toolOutput, map[string]any{"tool_call_id": activation.ID}); err != nil {
				return "", err
			}
			messages = append(messages, msg, toolMsg)
			messages = append(messages, llm.Message{Role: "user", Content: buildSkillPrompt(skill)})
			return a.runSkillLoop(ctx, sessionID, userID, skill, messages, model)
		}

		toolMsgs, err := a.handleGlobalTools(ctx, sessionID, userID, msg.ToolCalls)
		if err != nil {
			return "", err
		}
		messages = append(messages, msg)
		messages = append(messages, toolMsgs...)
	}
	return "", errors.New("tool loop exceeded")
}

func (a *Agent) handleSkillTools(ctx context.Context, sessionID int64, userID int64, skill skills.Skill, calls []llm.ToolCall) ([]llm.Message, error) {
	var out []llm.Message
	for _, call := range calls {
		if a.toolRegistry == nil {
			return nil, errors.New("tool registry not configured")
		}
		tool, ok := a.toolRegistry.Get(call.Function.Name)
		if !ok {
			toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: "unsupported tool"}
			out = append(out, toolMsg)
			continue
		}
		output, err := tool.Execute(ctx, tools.ToolContext{
			SessionID: sessionID,
			UserID:    userID,
			BaseDir:   skill.Metadata.Dir,
			SkillName: skill.Metadata.Name,
		}, json.RawMessage(call.Function.Arguments))
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
		_ = a.store.RecordSkillCall(ctx, sessionID, skill.Metadata.Name, map[string]any{"arguments": call.Function.Arguments}, map[string]any{"output": output}, status)
		_ = a.store.AddMessage(ctx, sessionID, "tool", output, map[string]any{"tool_call_id": call.ID, "status": status})
	}
	return out, nil
}

func (a *Agent) handleGlobalTools(ctx context.Context, sessionID int64, userID int64, calls []llm.ToolCall) ([]llm.Message, error) {
	var out []llm.Message
	for _, call := range calls {
		if a.toolRegistry == nil {
			return nil, errors.New("tool registry not configured")
		}
		tool, ok := a.toolRegistry.Get(call.Function.Name)
		if !ok {
			toolMsg := llm.Message{Role: "tool", ToolCallID: call.ID, Content: "unsupported tool"}
			out = append(out, toolMsg)
			continue
		}
		output, err := tool.Execute(ctx, tools.ToolContext{
			SessionID: sessionID,
			UserID:    userID,
			BaseDir:   a.defaultBaseDir,
		}, json.RawMessage(call.Function.Arguments))
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
		_ = a.store.AddMessage(ctx, sessionID, "tool", output, map[string]any{"tool_call_id": call.ID, "status": status})
	}
	return out, nil
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

func activateSkillTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.FunctionSpec{
			Name:        toolActivateSkill,
			Description: "Activate a skill from the available skills list.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Skill name",
					},
					"goal": map[string]any{
						"type":        "string",
						"description": "User goal for the skill",
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

func (a *Agent) globalTools() []llm.Tool {
	tools := []llm.Tool{activateSkillTool()}
	if a.toolRegistry == nil {
		return tools
	}
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

func (a *Agent) skillTools(skill skills.Skill) []llm.Tool {
	var tools []llm.Tool
	if a.toolRegistry == nil {
		return tools
	}
	for _, t := range a.toolRegistry.List() {
		if !toolAllowed(skill, t.Name()) {
			continue
		}
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

func toolAllowed(skill skills.Skill, tool string) bool {
	return len(skill.AllowedTools) == 0 || contains(skill.AllowedTools, tool)
}

func contains(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}
