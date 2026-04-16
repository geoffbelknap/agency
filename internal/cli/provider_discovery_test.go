package cli

import "testing"

func TestInferProviderToolCapabilitiesGemini(t *testing.T) {
	caps := inferProviderToolCapabilities("https://generativelanguage.googleapis.com/v1beta", "gemini-2.5-flash")
	want := map[string]bool{
		"provider-web-search":     true,
		"provider-url-context":    true,
		"provider-code-execution": true,
		"provider-file-search":    true,
		"provider-google-maps":    true,
		"provider-computer-use":   true,
	}
	if len(caps) != len(want) {
		t.Fatalf("caps = %#v", caps)
	}
	for _, cap := range caps {
		if !want[cap] {
			t.Fatalf("unexpected cap %q in %#v", cap, caps)
		}
	}
}

func TestInferProviderToolCapabilitiesUnknown(t *testing.T) {
	if caps := inferProviderToolCapabilities("https://llm.example.com/v1", "custom-model"); len(caps) != 0 {
		t.Fatalf("expected no provider tool caps for unknown provider, got %#v", caps)
	}
}
