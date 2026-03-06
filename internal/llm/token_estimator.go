package llm

import (
	"strings"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

type TokenEstimator struct {
	enc *tiktoken.Tiktoken
}

func NewTokenEstimator(model string) (*TokenEstimator, error) {
	model = strings.TrimSpace(model)
	enc, err := tiktoken.EncodingForModel(model)
	if err != nil {
		enc, err = tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
		if err != nil {
			return nil, err
		}
	}
	return &TokenEstimator{enc: enc}, nil
}

func (e *TokenEstimator) CountTokens(text string) int {
	if e == nil || e.enc == nil || text == "" {
		return 0
	}
	return len(e.enc.EncodeOrdinary(text))
}

func (e *TokenEstimator) CountMessage(msg Message) int {
	if e == nil || e.enc == nil {
		return 0
	}
	total := tokensPerMessageOverhead
	if msg.Content != "" {
		total += e.CountTokens(msg.Content)
	}
	if msg.ToolCallID != "" {
		total += tokensPerToolCallOverhead
		total += e.CountTokens(msg.ToolCallID)
	}
	if len(msg.ToolCalls) > 0 {
		for _, call := range msg.ToolCalls {
			total += tokensPerToolCallOverhead
			if call.ID != "" {
				total += e.CountTokens(call.ID)
			}
			if call.Function.Name != "" {
				total += e.CountTokens(call.Function.Name)
			}
			if call.Function.Arguments != "" {
				total += e.CountTokens(call.Function.Arguments)
			}
		}
	}
	return total
}

func (e *TokenEstimator) CountMessages(msgs []Message) int {
	if e == nil || e.enc == nil || len(msgs) == 0 {
		return 0
	}
	total := 0
	for _, msg := range msgs {
		total += e.CountMessage(msg)
	}
	total += tokensReplyOverhead
	if total == 0 {
		return 0
	}
	return int(float64(total) * tokenEstimateMargin)
}

const (
	// Chat Completions overhead estimates (conservative).
	tokensPerMessageOverhead  = 4
	tokensPerToolCallOverhead = 6
	tokensReplyOverhead       = 3
	tokenEstimateMargin       = 1.05
)
