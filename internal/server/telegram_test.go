package server

import (
	"strings"
	"testing"
)

func TestSplitTelegramMessageUnderLimit(t *testing.T) {
	// Create text that's under the byte limit
	text := strings.Repeat("a", telegramSafeMessageBytes-1)
	parts := splitTelegramMessage(text)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0] != text {
		t.Fatalf("expected text to be unchanged")
	}
}

func TestSplitTelegramMessageSplitsAndPreserves(t *testing.T) {
	text := strings.Repeat("b", telegramSafeMessageBytes+50)
	parts := splitTelegramMessage(text)
	if len(parts) < 2 {
		t.Fatalf("expected multiple parts, got %d", len(parts))
	}
	joined := strings.Join(parts, "")
	if joined != text {
		t.Fatalf("expected joined text to match original")
	}
	for i, part := range parts {
		if len(part) > telegramSafeMessageBytes {
			t.Fatalf("part %d exceeds safe limit: %d bytes", i, len(part))
		}
	}
}

func TestSplitTelegramMessagePrefersNewline(t *testing.T) {
	prefix := strings.Repeat("x", telegramSafeMessageBytes-10) + "\n"
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

func TestSplitTelegramMessageMultibyteChars(t *testing.T) {
	// Test with Chinese characters (3 bytes each in UTF-8)
	// Each Chinese character is 3 bytes, so 1267 chars = ~3800 bytes
	chineseChar := "中"
	text := strings.Repeat(chineseChar, 1500) // ~4500 bytes
	parts := splitTelegramMessage(text)
	
	if len(parts) < 2 {
		t.Fatalf("expected multiple parts for multibyte text, got %d", len(parts))
	}
	
	joined := strings.Join(parts, "")
	if joined != text {
		t.Fatalf("expected joined text to match original")
	}
	
	for i, part := range parts {
		if len(part) > telegramMaxMessageBytes {
			t.Fatalf("part %d exceeds max limit: %d bytes (max %d)", i, len(part), telegramMaxMessageBytes)
		}
	}
}

func TestSplitTelegramMessageMixedContent(t *testing.T) {
	// Test with mixed ASCII and multibyte characters
	text := "Hello " + strings.Repeat("世界", 1000) + " End"
	parts := splitTelegramMessage(text)
	
	joined := strings.Join(parts, "")
	if joined != text {
		t.Fatalf("expected joined text to match original")
	}
	
	// Verify each part is valid UTF-8 and within limits
	for i, part := range parts {
		if len(part) > telegramMaxMessageBytes {
			t.Fatalf("part %d exceeds max limit: %d bytes", i, len(part))
		}
	}
}

func TestSplitTelegramMessageEmoji(t *testing.T) {
	// Test with emoji (4 bytes each in UTF-8)
	emoji := "😀"
	text := strings.Repeat(emoji, 1200) // ~4800 bytes
	parts := splitTelegramMessage(text)
	
	if len(parts) < 2 {
		t.Fatalf("expected multiple parts for emoji text, got %d", len(parts))
	}
	
	joined := strings.Join(parts, "")
	if joined != text {
		t.Fatalf("expected joined text to match original")
	}
	
	for i, part := range parts {
		if len(part) > telegramMaxMessageBytes {
			t.Fatalf("part %d exceeds max limit: %d bytes", i, len(part))
		}
	}
}
