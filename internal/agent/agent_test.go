package agent

import (
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/memory"
)

func TestFormatSummaryMessageHidesAutoCompactPhase(t *testing.T) {
	summary := &memory.Summary{
		Phase:   "auto-compact",
		Summary: "Keep cooking advice brief and continue from the beef stew discussion.",
	}

	got := formatSummaryMessage(summary)

	if !strings.HasPrefix(got, internalCheckpointPrefix) {
		t.Fatalf("expected internal checkpoint block, got %q", got)
	}
	if strings.Contains(got, "auto-compact") {
		t.Fatalf("auto-compact phase should not be exposed in summary message: %q", got)
	}
	if !strings.Contains(got, "policy: do_not_mention_checkpoint_or_compaction_unless_user_asks") {
		t.Fatalf("expected non-disclosure policy, got %q", got)
	}
	if !strings.Contains(got, summary.Summary) {
		t.Fatalf("expected summary body to be preserved, got %q", got)
	}
	if !strings.Contains(got, "</internal_checkpoint>") {
		t.Fatalf("expected closing checkpoint tag, got %q", got)
	}
}

func TestFormatSummaryMessageKeepsNonCompactPhase(t *testing.T) {
	summary := &memory.Summary{
		Phase:   "handoff",
		Summary: "User wants a follow-up on deployment steps.",
	}

	got := formatSummaryMessage(summary)

	if !strings.Contains(got, "phase: handoff") {
		t.Fatalf("expected non-compact phase to remain visible, got %q", got)
	}
}

func TestIsSessionSummarySystemMessageUsesCheckpointMarker(t *testing.T) {
	if !isSessionSummarySystemMessage("<internal_checkpoint>\nsummary:\nkeep going\n</internal_checkpoint>") {
		t.Fatal("expected checkpoint marker to be recognized")
	}
	if isSessionSummarySystemMessage("Session summary:\nold format") {
		t.Fatal("old freeform prefix should no longer be treated as the active checkpoint format")
	}
}
