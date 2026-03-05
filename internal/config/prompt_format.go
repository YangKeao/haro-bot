package config

import "strings"

type PromptFormat string

const (
	PromptFormatOpenAI PromptFormat = "openai"
	PromptFormatClaude PromptFormat = "claude"
)

func NormalizePromptFormat(value string) PromptFormat {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "openai":
		return PromptFormatOpenAI
	case "claude", "anthropic", "xml":
		return PromptFormatClaude
	default:
		return PromptFormatOpenAI
	}
}
