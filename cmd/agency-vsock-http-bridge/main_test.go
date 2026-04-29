//go:build linux

package main

import "testing"

func TestParseBridgeSpecs(t *testing.T) {
	specs, err := parseBridgeSpecs("127.0.0.1:3128=2:3128, 127.0.0.1:8081=2:8081")
	if err != nil {
		t.Fatalf("parseBridgeSpecs returned error: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("len = %d, want 2", len(specs))
	}
	if specs[0] != (bridgeSpec{Listen: "127.0.0.1:3128", CID: 2, Port: 3128}) {
		t.Fatalf("spec[0] = %#v", specs[0])
	}
	if specs[1] != (bridgeSpec{Listen: "127.0.0.1:8081", CID: 2, Port: 8081}) {
		t.Fatalf("spec[1] = %#v", specs[1])
	}
}

func TestParseBridgeSpecsRejectsInvalidShape(t *testing.T) {
	if _, err := parseBridgeSpecs("127.0.0.1:3128"); err == nil {
		t.Fatal("expected invalid bridge error")
	}
	if _, err := parseBridgeSpecs("127.0.0.1:3128=cid:3128"); err == nil {
		t.Fatal("expected invalid CID error")
	}
	if _, err := parseBridgeSpecs(""); err == nil {
		t.Fatal("expected empty bridge error")
	}
}

func TestParseGuestListenerSpecs(t *testing.T) {
	specs, err := parseGuestListenerSpecs("3128=127.0.0.1:3128, 8081=127.0.0.1:8081")
	if err != nil {
		t.Fatalf("parseGuestListenerSpecs returned error: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("len = %d, want 2", len(specs))
	}
	if specs[0] != (guestListenerSpec{Port: 3128, Target: "127.0.0.1:3128"}) {
		t.Fatalf("spec[0] = %#v", specs[0])
	}
	if specs[1] != (guestListenerSpec{Port: 8081, Target: "127.0.0.1:8081"}) {
		t.Fatalf("spec[1] = %#v", specs[1])
	}
}

func TestParseGuestListenerSpecsRejectsInvalidShape(t *testing.T) {
	if _, err := parseGuestListenerSpecs("3128"); err == nil {
		t.Fatal("expected invalid guest listener error")
	}
	if _, err := parseGuestListenerSpecs("not-a-port=127.0.0.1:3128"); err == nil {
		t.Fatal("expected invalid guest listener port error")
	}
	if _, err := parseGuestListenerSpecs("3128="); err == nil {
		t.Fatal("expected empty target error")
	}
}
