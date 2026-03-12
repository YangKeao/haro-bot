package llm

import "testing"

func TestTokenEstimator(t *testing.T) {
	t.Run("creates estimator for known model", func(t *testing.T) {
		est, err := NewTokenEstimator("gpt-4o")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if est == nil {
			t.Fatal("expected non-nil estimator")
		}
	})

	t.Run("counts message tokens", func(t *testing.T) {
		est, _ := NewTokenEstimator("gpt-4o")
		msg := Message{Role: "user", Content: "Hello, world!"}
		count := est.CountMessage(msg)
		if count <= 0 {
			t.Errorf("expected positive token count, got %d", count)
		}
	})

	t.Run("counts messages tokens", func(t *testing.T) {
		est, _ := NewTokenEstimator("gpt-4o")
		msgs := []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello!"},
		}
		count := est.CountMessages(msgs)
		if count <= 0 {
			t.Errorf("expected positive token count, got %d", count)
		}
	})
}

func TestContextConfig(t *testing.T) {
	t.Run("zero config returns zero effective", func(t *testing.T) {
		cfg := ContextConfig{}
		if cfg.EffectiveWindowTokens() != 0 {
			t.Errorf("expected 0, got %d", cfg.EffectiveWindowTokens())
		}
	})

	t.Run("calculates effective window", func(t *testing.T) {
		cfg := ContextConfig{
			WindowTokens:                  100000,
			EffectiveContextWindowPercent: 80,
		}
		effective := cfg.EffectiveWindowTokens()
		if effective != 80000 {
			t.Errorf("expected 80000, got %d", effective)
		}
	})

	t.Run("defaults to 95 percent", func(t *testing.T) {
		cfg := ContextConfig{
			WindowTokens: 100000,
		}
		effective := cfg.EffectiveWindowTokens()
		if effective != 95000 {
			t.Errorf("expected 95000, got %d", effective)
		}
	})

	t.Run("auto compact limit", func(t *testing.T) {
		cfg := ContextConfig{
			AutoCompactTokenLimit: 50000,
		}
		limit := cfg.AutoCompactLimit()
		if limit != 50000 {
			t.Errorf("expected 50000, got %d", limit)
		}
	})
}

func TestReasoningConfig(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		cfg := ReasoningConfig{}
		if cfg.Enabled {
			t.Error("expected reasoning to be disabled by default")
		}
	})
}

func TestIsContextWindowExceeded(t *testing.T) {
	t.Run("returns false for nil error", func(t *testing.T) {
		if IsContextWindowExceeded(nil) {
			t.Error("expected false for nil error")
		}
	})
}
