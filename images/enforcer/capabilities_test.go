package main

import "testing"

func TestDetectRequiredCaps_Tools(t *testing.T) {
	body := map[string]interface{}{
		"model": "claude-sonnet",
		"tools": []interface{}{map[string]interface{}{"type": "function"}},
	}
	caps := detectRequiredCaps(body)
	if !hasCap(caps, "tools") {
		t.Error("expected tools")
	}
}

func TestDetectRequiredCaps_Vision(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "x"}},
				},
			},
		},
	}
	caps := detectRequiredCaps(body)
	if !hasCap(caps, "vision") {
		t.Error("expected vision")
	}
}

func TestDetectRequiredCaps_Streaming(t *testing.T) {
	body := map[string]interface{}{"stream": true}
	caps := detectRequiredCaps(body)
	if !hasCap(caps, "streaming") {
		t.Error("expected streaming")
	}
}

func TestDetectRequiredCaps_PlainText(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hello"}},
	}
	caps := detectRequiredCaps(body)
	if len(caps) != 0 {
		t.Errorf("expected no caps, got %v", caps)
	}
}

func TestModelHasCapability(t *testing.T) {
	m := Model{Capabilities: []string{"tools", "vision", "streaming"}}
	if !m.HasCapability("tools") {
		t.Error("expected tools capability")
	}
	if !m.HasCapability("vision") {
		t.Error("expected vision capability")
	}
	if m.HasCapability("audio") {
		t.Error("unexpected audio capability")
	}
}

func TestModelHasCapability_Empty(t *testing.T) {
	m := Model{}
	if m.HasCapability("tools") {
		t.Error("empty model should not have any capability")
	}
}

func hasCap(caps []string, target string) bool {
	for _, c := range caps {
		if c == target {
			return true
		}
	}
	return false
}
