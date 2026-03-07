package server

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitTelegramMessageUnderLimit(t *testing.T) {
	text := strings.Repeat("a", telegramSafeMessageRunes-1)
	parts := splitTelegramMessage(text)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0] != text {
		t.Fatalf("expected text to be unchanged")
	}
}

func TestSplitTelegramMessageSplitsAndPreserves(t *testing.T) {
	text := strings.Repeat("b", telegramSafeMessageRunes+50)
	parts := splitTelegramMessage(text)
	if len(parts) < 2 {
		t.Fatalf("expected multiple parts, got %d", len(parts))
	}
	joined := strings.Join(parts, "")
	if joined != text {
		t.Fatalf("expected joined text to match original")
	}
	for i, part := range parts {
		if utf8.RuneCountInString(part) > telegramSafeMessageRunes {
			t.Fatalf("part %d exceeds safe limit", i)
		}
	}
}

func TestSplitTelegramMessagePrefersNewline(t *testing.T) {
	prefix := strings.Repeat("x", telegramSafeMessageRunes-10) + "\n"
	suffix := strings.Repeat("y", 20)
	text := prefix + suffix
	parts := splitTelegramMessage(text)
	if len(parts) < 2 {
		t.Fatalf("expected split, got %d", len(parts))
	}
	if !strings.HasSuffix(parts[0], "\n") {
		t.Fatalf("expected first part to end with newline")
	}
	joined := strings.Join(parts, "")
	if joined != text {
		t.Fatalf("expected joined text to match original")
	}
}
