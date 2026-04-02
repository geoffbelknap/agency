package main

import "testing"

func TestBlocklist_ExactMatch(t *testing.T) {
	bl := NewBlocklist()
	bl.AddDeny("evil.com")
	if !bl.IsBlocked("evil.com") {
		t.Error("expected evil.com to be blocked")
	}
	if bl.IsBlocked("good.com") {
		t.Error("expected good.com to not be blocked")
	}
}

func TestBlocklist_WildcardMatch(t *testing.T) {
	bl := NewBlocklist()
	bl.AddDeny("*.onion")
	if !bl.IsBlocked("hidden.onion") {
		t.Error("expected hidden.onion to be blocked")
	}
	if !bl.IsBlocked("deep.hidden.onion") {
		t.Error("expected deep.hidden.onion to be blocked")
	}
	if bl.IsBlocked("onion.com") {
		t.Error("expected onion.com to not be blocked")
	}
}

func TestBlocklist_IPPattern(t *testing.T) {
	bl := NewBlocklist()
	bl.AddDeny("169.254.*")
	if !bl.IsBlocked("169.254.169.254") {
		t.Error("expected metadata IP to be blocked")
	}
	if bl.IsBlocked("10.0.0.1") {
		t.Error("expected 10.0.0.1 to not be blocked")
	}
}

func TestBlocklist_Merge(t *testing.T) {
	platform := NewBlocklist()
	platform.AddDeny("malware.com")
	operator := NewBlocklist()
	operator.AddDeny("pastebin.com")
	merged := MergeBlocklists(platform, operator)
	if !merged.IsBlocked("malware.com") {
		t.Error("expected malware.com blocked after merge")
	}
	if !merged.IsBlocked("pastebin.com") {
		t.Error("expected pastebin.com blocked after merge")
	}
}

func TestBlocklist_LoadYAML(t *testing.T) {
	yaml := []byte("deny:\n  - evil.com\n  - \"*.onion\"\n")
	bl, err := LoadBlocklistYAML(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bl.IsBlocked("evil.com") {
		t.Error("expected evil.com to be blocked")
	}
	if !bl.IsBlocked("test.onion") {
		t.Error("expected test.onion to be blocked")
	}
}
