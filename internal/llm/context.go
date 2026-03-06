package llm

type ContextConfig struct {
	WindowTokens                  int
	AutoCompactTokenLimit         int
	EffectiveContextWindowPercent int
}

const defaultEffectiveContextWindowPercent = 95

func (c ContextConfig) EffectivePercent() int {
	if c.EffectiveContextWindowPercent <= 0 {
		return defaultEffectiveContextWindowPercent
	}
	if c.EffectiveContextWindowPercent > 100 {
		return 100
	}
	return c.EffectiveContextWindowPercent
}

func (c ContextConfig) EffectiveWindowTokens() int {
	if c.WindowTokens <= 0 {
		return 0
	}
	return (c.WindowTokens * c.EffectivePercent()) / 100
}

// AutoCompactLimit returns the compaction threshold, clamped to 90% of the context window
// when the context window is known (Codex-style behavior).
func (c ContextConfig) AutoCompactLimit() int {
	if c.WindowTokens <= 0 {
		if c.AutoCompactTokenLimit > 0 {
			return c.AutoCompactTokenLimit
		}
		return 0
	}
	limit := (c.WindowTokens * 9) / 10
	if c.AutoCompactTokenLimit > 0 && c.AutoCompactTokenLimit < limit {
		limit = c.AutoCompactTokenLimit
	}
	return limit
}
