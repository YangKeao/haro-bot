package llm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
)

var ErrContextWindowExceeded = errors.New("context window exceeded")

func IsContextWindowExceeded(err error) bool {
	return errors.Is(err, ErrContextWindowExceeded)
}

func normalizeChatCompletionError(err error, resp *openai.ChatCompletion) error {
	if err != nil {
		if isContextWindowError(err) {
			return fmt.Errorf("%w: %v", ErrContextWindowExceeded, err)
		}
		return err
	}
	if finishReason := firstFinishReason(resp); isContextWindowFinishReason(finishReason) {
		return fmt.Errorf("%w: finish_reason=%s", ErrContextWindowExceeded, finishReason)
	}
	return nil
}

func firstFinishReason(resp *openai.ChatCompletion) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].FinishReason)
}

func isContextWindowFinishReason(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	if strings.Contains(reason, "context_window") {
		return true
	}
	if strings.Contains(reason, "context") && strings.Contains(reason, "exceed") {
		return true
	}
	if strings.Contains(reason, "model_context") {
		return true
	}
	return false
}

func isContextWindowError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "context_length_exceeded"):
		return true
	case strings.Contains(msg, "context window"):
		return true
	case strings.Contains(msg, "context_window"):
		return true
	case strings.Contains(msg, "model_context_window_exceeded"):
		return true
	// Covers: "Requested token count exceeds the model's maximum context length"
	case strings.Contains(msg, "exceeds") && strings.Contains(msg, "context"):
		return true
	case strings.Contains(msg, "maximum context length"):
		return true
	default:
		return false
	}
}
