package agent

import (
	"context"
	"errors"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
)

type Agent struct {
	store          ConversationStore
	skills         *skills.Manager
	toolRegistry   *tools.Registry
	promptBuilder  PromptBuilder
	toolRunner     ToolRunner
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
	promptBuilder := DefaultPromptBuilder{}
	toolRunner := NewToolRunner(toolRegistry, store, skills, promptBuilder)
	return &Agent{
		store:          store,
		skills:         skills,
		toolRegistry:   toolRegistry,
		promptBuilder:  promptBuilder,
		toolRunner:     toolRunner,
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
	systemPrompt := a.promptBuilder.System(memories, availableSkills, a.promptFormat)
	messages := []llm.Message{{Role: "system", Content: systemPrompt}}
	messages = append(messages, toLLMMessages(recent)...) // includes user input
	return a.runLoop(ctx, sessionID, userID, messages, model, nil)
}

// InterruptSession generates a response from an existing session context without using tools.
// If storeInSession is true, the interrupt message and response are persisted to the session.
func (a *Agent) InterruptSession(ctx context.Context, sessionID int64, userID int64, input string, modelOverride string, storeInSession bool) (string, error) {
	model := a.model
	if modelOverride != "" {
		model = modelOverride
	}
	if storeInSession {
		if err := a.store.AddMessage(ctx, sessionID, "user", input, nil); err != nil {
			return "", err
		}
	}
	recent, err := a.store.LoadRecentMessages(ctx, sessionID, a.maxContext)
	if err != nil {
		return "", err
	}
	memories, err := a.store.LoadLongMemories(ctx, userID, 8)
	if err != nil {
		return "", err
	}
	systemPrompt := a.promptBuilder.Interrupt(memories, a.promptFormat)
	messages := []llm.Message{{Role: "system", Content: systemPrompt}}
	messages = append(messages, toLLMMessages(recent)...)
	if !storeInSession {
		messages = append(messages, llm.Message{Role: "user", Content: input})
	}
	resp, err := a.llm.Chat(ctx, llm.ChatRequest{
		Model:    model,
		Messages: messages,
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("empty llm response")
	}
	content := resp.Choices[0].Message.Content
	if storeInSession {
		if err := a.store.AddMessage(ctx, sessionID, "assistant", content, nil); err != nil {
			return "", err
		}
	}
	return content, nil
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

		if err := a.store.AddMessage(ctx, sessionID, "assistant", msg.Content, &memory.MessageMetadata{ToolCalls: msg.ToolCalls}); err != nil {
			return "", err
		}

		toolMsgs, updatedSkill, err := a.toolRunner.Run(ctx, sessionID, userID, a.defaultBaseDir, activeSkill, msg.ToolCalls)
		if err != nil {
			return "", err
		}
		activeSkill = updatedSkill
		messages = append(messages, msg)
		messages = append(messages, toolMsgs...)
	}
	return "", errors.New("tool loop exceeded")
}

func toLLMMessages(msgs []memory.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		llmMsg := llm.Message{Role: m.Role, Content: m.Content}
		if m.Metadata != nil {
			if m.Metadata.ToolCallID != "" {
				llmMsg.ToolCallID = m.Metadata.ToolCallID
			}
			if len(m.Metadata.ToolCalls) > 0 {
				llmMsg.ToolCalls = m.Metadata.ToolCalls
			}
		}
		out = append(out, llmMsg)
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
