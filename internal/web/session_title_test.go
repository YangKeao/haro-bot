package web

import (
	"strings"
	"testing"
)

func TestAutoSessionTitleStoresPlainText(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "underscores serialized by Markdown composer",
			content: `请你调查 tidb\_enable\_check\_constraint 的行为`,
			want:    "请你调查 tidb_enable_check_constraint 的行为",
		},
		{
			name:    "all CommonMark punctuation escapes",
			content: `Explain \*literal\* \[label\] and escaped slash \\`,
			want:    `Explain *literal* [label] and escaped slash \`,
		},
		{
			name:    "non punctuation backslash is preserved",
			content: `Open C:\Users\name`,
			want:    `Open C:\Users\name`,
		},
		{
			name:    "blank message fallback",
			content: " \n\t ",
			want:    "Image conversation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoSessionTitle(tt.content); got != tt.want {
				t.Fatalf("autoSessionTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAutoSessionTitleTruncatesAfterUnescaping(t *testing.T) {
	content := strings.Repeat(`a\_`, 30)
	want := strings.Repeat("a_", 28) + "…"
	if got := autoSessionTitle(content); got != want {
		t.Fatalf("autoSessionTitle() = %q, want %q", got, want)
	}
}
