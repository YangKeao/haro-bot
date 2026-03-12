package agent

import (
	"fmt"
	"strings"

	"github.com/YangKeao/haro-bot/internal/llm"
)

const maxToolOutputTokens = 2048

func truncateToolOutputForModel(estimator *llm.TokenEstimator, output string) string {
	if estimator == nil || strings.TrimSpace(output) == "" {
		return output
	}
	totalTokens := estimator.CountTokens(output)
	if totalTokens <= maxToolOutputTokens {
		return output
	}

	runes := []rune(output)
	best := toolOutputMarker(totalTokens, strings.Contains(output, "\n"))
	low, high := 0, len(runes)
	for low <= high {
		kept := (low + high) / 2
		leftCount := kept / 2
		rightCount := kept - leftCount
		if leftCount+rightCount > len(runes) {
			rightCount = len(runes) - leftCount
		}

		prefix := string(runes[:leftCount])
		suffix := string(runes[len(runes)-rightCount:])
		removedTokens := totalTokens - estimator.CountTokens(prefix) - estimator.CountTokens(suffix)
		if removedTokens < 0 {
			removedTokens = 0
		}
		candidate := prefix + toolOutputMarker(removedTokens, strings.Contains(output, "\n")) + suffix
		if estimator.CountTokens(candidate) <= maxToolOutputTokens {
			best = candidate
			low = kept + 1
			continue
		}
		high = kept - 1
	}
	return best
}

func toolOutputMarker(removedTokens int, multiline bool) string {
	if removedTokens < 0 {
		removedTokens = 0
	}
	if multiline {
		return fmt.Sprintf("\n…%d tokens truncated…\n", removedTokens)
	}
	return fmt.Sprintf("…%d tokens truncated…", removedTokens)
}
