package agent

import (
	"context"
	"encoding/json"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/tools"
	"go.uber.org/zap"
)

type DefaultToolRunner struct {
	registry  *tools.Registry
	store     memory.StoreAPI
	estimator *llm.TokenEstimator
}

func NewToolRunner(registry *tools.Registry, store memory.StoreAPI, estimator *llm.TokenEstimator) *DefaultToolRunner {
	return &DefaultToolRunner{
		registry:  registry,
		store:     store,
		estimator: estimator,
	}
}

func (r *DefaultToolRunner) Run(ctx context.Context, sessionID int64, baseDir string, calls []llm.ToolCall) ([]StoredMessage, error) {
	log := logging.L().Named("tool_runner")
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
				return nil, err
			}
			ctxMsg, err := newStoredMessage(entryID, toolMsg)
			if err != nil {
				return nil, err
			}
			out = append(out, ctxMsg)
			continue
		}
		log.Debug("tool start", zap.String("tool", call.Function.Name), zap.Int64("session_id", sessionID))
		tc := tools.ToolContext{
			SessionID: sessionID,
			BaseDir:   baseDir,
		}
		var result tools.ToolResult
		var output string
		var err error
		if rich, ok := tool.(tools.RichTool); ok {
			result, err = rich.ExecuteRich(ctx, tc, json.RawMessage(call.Function.Arguments))
			output = result.ModelText
		} else {
			output, err = tool.Execute(ctx, tc, json.RawMessage(call.Function.Arguments))
		}
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
		metadata := &memory.MessageMetadata{
			ToolCallID:        call.ID,
			Status:            status,
			ToolName:          result.ToolName,
			MCPServer:         result.MCPServer,
			DisplayContent:    result.DisplayText,
			StructuredContent: result.StructuredContent,
			ObservationKey:    result.ObservationKey,
			ArtifactIDs:       result.ArtifactIDs,
		}
		entryID, err := r.store.AddMessageAndGetID(ctx, sessionID, "tool", output, metadata)
		if err != nil {
			return nil, err
		}
		ctxMsg, err := newStoredMessage(entryID, toolMsg, metadata)
		if err != nil {
			return nil, err
		}
		out = append(out, ctxMsg)
	}
	return out, nil
}
