package agent

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestTokenBudget(t *testing.T) {
	tests := []struct {
		name              string
		cfg               llm.ContextConfig
		wantEffective     int
		wantAutoCompact   int
		wantInputBudget   int
		wantSummaryBudget int
	}{
		{
			name:              "zero config uses defaults",
			cfg:               llm.ContextConfig{},
			wantEffective:     0,
			wantAutoCompact:   0,
			wantInputBudget:   0,
			wantSummaryBudget: 0,
		},
		{
			name:              "with effective window only",
			cfg:               llm.ContextConfig{WindowTokens: 100000, EffectiveContextWindowPercent: 80},
			wantEffective:     80000,
			wantAutoCompact:   90000, // 90% of WindowTokens
			wantInputBudget:   80000,
			wantSummaryBudget: 90000, // uses autoCompact since it's set
		},
		{
			name: "with auto compact limit",
			cfg: llm.ContextConfig{
				WindowTokens:                  100000,
				EffectiveContextWindowPercent: 80,
				AutoCompactTokenLimit:         50000,
			},
			wantEffective:     80000,
			wantAutoCompact:   50000, // min(90000, 50000)
			wantInputBudget:   80000,
			wantSummaryBudget: 50000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := computeTokenBudget(tt.cfg)
			if budget.Effective != tt.wantEffective {
				t.Errorf("Effective = %d, want %d", budget.Effective, tt.wantEffective)
			}
			if budget.AutoCompact != tt.wantAutoCompact {
				t.Errorf("AutoCompact = %d, want %d", budget.AutoCompact, tt.wantAutoCompact)
			}
			if budget.InputBudget != tt.wantInputBudget {
				t.Errorf("InputBudget = %d, want %d", budget.InputBudget, tt.wantInputBudget)
			}
			if budget.SummaryBudget != tt.wantSummaryBudget {
				t.Errorf("SummaryBudget = %d, want %d", budget.SummaryBudget, tt.wantSummaryBudget)
			}
		})
	}
}
