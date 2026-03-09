package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/openai/openai-go"
)

// streamResult holds the result of streaming completion
type streamResult struct {
	completion       *openai.ChatCompletion
	reasoningContent string // Accumulated reasoning content (for GLM/DeepSeek)
}

func streamChatCompletion(ctx context.Context, client *openai.Client, params openai.ChatCompletionNewParams, handler StreamHandler) (*streamResult, error) {
	if client == nil {
		return nil, errors.New("llm client not configured")
	}
	stream := client.Chat.Completions.NewStreaming(ctx, params)
	if stream == nil {
		return nil, errors.New("llm stream not initialized")
	}
	defer stream.Close()

	var acc openai.ChatCompletionAccumulator
	var streamErr error
	var mu sync.Mutex
	
	// Accumulate reasoning content separately since openai-go's Accumulator doesn't handle ExtraFields
	var reasoningBuilder strings.Builder

	// Read stream in a goroutine to allow context cancellation
	streamChan := make(chan openai.ChatCompletionChunk, 100)
	go func() {
		defer close(streamChan)
		for stream.Next() {
			chunk := stream.Current()
			select {
			case streamChan <- chunk:
			case <-ctx.Done():
				return
			}
		}
		mu.Lock()
		streamErr = stream.Err()
		mu.Unlock()
	}()

	// Process chunks with context cancellation support
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-streamChan:
			if !ok {
				// Stream finished
				mu.Lock()
				err := streamErr
				mu.Unlock()
				if err != nil {
					return nil, err
				}
				
				return &streamResult{
					completion:       &acc.ChatCompletion,
					reasoningContent: reasoningBuilder.String(),
				}, nil
			}

			if handler != nil && len(chunk.Choices) > 0 {
				for _, choice := range chunk.Choices {
					// Handle reasoning content from ExtraFields (for models like GLM, DeepSeek)
					// Note: field.Valid() may return false for unknown fields, but Raw() still has the value
					if field, ok := choice.Delta.JSON.ExtraFields["reasoning_content"]; ok {
						raw := field.Raw()
						if raw != "" {
							var reasoningContent string
							// Raw returns the JSON-encoded value, need to unmarshal it
							if err := json.Unmarshal([]byte(raw), &reasoningContent); err == nil {
								// Accumulate reasoning content
								reasoningBuilder.WriteString(reasoningContent)
								// Stream to handler
								safeCallStreamHandler(handler, StreamEvent{ReasoningDelta: reasoningContent})
							}
						}
					}
					// Handle regular content
					if choice.Delta.Content != "" {
						safeCallStreamHandler(handler, StreamEvent{Delta: choice.Delta.Content})
					}
				}
			}
			if ok := acc.AddChunk(chunk); !ok {
				return nil, errors.New("failed to accumulate stream chunk")
			}
		}
	}
}

func safeCallStreamHandler(handler StreamHandler, event StreamEvent) {
	defer func() {
		_ = recover()
	}()
	handler(event)
}
