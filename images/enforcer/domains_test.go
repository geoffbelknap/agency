package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDomainGateDenylistUnlisted(t *testing.T) {
	dg := NewDomainGate()
	if !dg.Allowed("api.example.com") {
		t.Error("unlisted domain should be allowed in denylist mode")
	}
}

func TestDomainGateDenylistBlocked(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte(`
mode: denylist
domains:
  - evil.com
  - malware.net
`), 0644)

	dg := NewDomainGate()
	if err := dg.LoadFromFile(f); err != nil {
		t.Fatalf("load error: %v", err)
	}

	if dg.Allowed("evil.com") {
		t.Error("denied domain should be blocked")
	}
	if dg.Allowed("malware.net") {
		t.Error("denied domain should be blocked")
	}
	if !dg.Allowed("safe.com") {
		t.Error("unlisted domain should be allowed")
	}
}

func TestDomainGateAllowlistListed(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte(`
mode: allowlist
domains:
  - api.anthropic.com
  - github.com
`), 0644)

	dg := NewDomainGate()
	if err := dg.LoadFromFile(f); err != nil {
		t.Fatalf("load error: %v", err)
	}

	if !dg.Allowed("api.anthropic.com") {
		t.Error("listed domain should be allowed")
	}
	if !dg.Allowed("github.com") {
		t.Error("listed domain should be allowed")
	}
}

func TestDomainGateAllowlistUnlisted(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte(`
mode: allowlist
domains:
  - api.anthropic.com
`), 0644)

	dg := NewDomainGate()
	if err := dg.LoadFromFile(f); err != nil {
		t.Fatalf("load error: %v", err)
	}

	if dg.Allowed("evil.com") {
		t.Error("unlisted domain should be blocked in allowlist mode")
	}
}

func TestDomainGateWildcard(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte(`
mode: allowlist
domains:
  - "*.github.com"
  - api.anthropic.com
`), 0644)

	dg := NewDomainGate()
	if err := dg.LoadFromFile(f); err != nil {
		t.Fatalf("load error: %v", err)
	}

	if !dg.Allowed("api.github.com") {
		t.Error("subdomain should match wildcard")
	}
	if !dg.Allowed("raw.github.com") {
		t.Error("subdomain should match wildcard")
	}
	if dg.Allowed("github.io") {
		t.Error("non-matching domain should be blocked")
	}
}

func TestDomainGateInfraBypass(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte(`
mode: allowlist
domains:
  - api.example.com
`), 0644)

	dg := NewDomainGate()
	if err := dg.LoadFromFile(f); err != nil {
		t.Fatalf("load error: %v", err)
	}

	// Infrastructure domains always pass even in allowlist mode
	if !dg.Allowed("egress") {
		t.Error("infrastructure domain 'egress' should bypass")
	}
	if !dg.Allowed("enforcer") {
		t.Error("infrastructure domain 'enforcer' should bypass")
	}
	if !dg.Allowed("localhost") {
		t.Error("infrastructure domain 'localhost' should bypass")
	}
	if !dg.Allowed("127.0.0.1") {
		t.Error("infrastructure domain '127.0.0.1' should bypass")
	}
}

func TestDomainGateObjectFormat(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte(`
agent: test-agent
mode: allowlist
domains:
  - domain: httpbin.org
    approved_at: '2026-03-10T04:18:48Z'
    approved_by: operator
    reason: E2E test
  - domain: api.github.com
    approved_at: '2026-03-10T04:19:00Z'
    approved_by: operator
    reason: needs GitHub access
`), 0644)

	dg := NewDomainGate()
	if err := dg.LoadFromFile(f); err != nil {
		t.Fatalf("load error: %v", err)
	}

	if !dg.Allowed("httpbin.org") {
		t.Error("domain from object format should be allowed")
	}
	if !dg.Allowed("api.github.com") {
		t.Error("domain from object format should be allowed")
	}
	if dg.Allowed("evil.com") {
		t.Error("unlisted domain should be blocked in allowlist mode")
	}
}

func TestDomainGateStripPort(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte(`
mode: allowlist
domains:
  - api.example.com
`), 0644)

	dg := NewDomainGate()
	if err := dg.LoadFromFile(f); err != nil {
		t.Fatalf("load error: %v", err)
	}

	if !dg.Allowed("api.example.com:443") {
		t.Error("domain with port should match")
	}
}

func TestDomainGateCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte(`
mode: allowlist
domains:
  - Api.Example.COM
`), 0644)

	dg := NewDomainGate()
	if err := dg.LoadFromFile(f); err != nil {
		t.Fatalf("load error: %v", err)
	}

	if !dg.Allowed("api.example.com") {
		t.Error("domain lookup should be case insensitive")
	}
}
