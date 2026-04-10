package main

import "testing"

func TestNormalizeQuickstartProvider(t *testing.T) {
	tests := map[string]string{
		"":              "",
		"anthropic":     "anthropic",
		"OpenAI":        "openai",
		"google":        "gemini",
		"gemini":        "gemini",
		"Google-Gemini": "gemini",
		"  GOOGLE  ":    "gemini",
	}

	for input, want := range tests {
		if got := normalizeProvider(input); got != want {
			t.Fatalf("normalizeProvider(%q) = %q, want %q", input, got, want)
		}
	}
}
