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
