# Input Validation & SSRF Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix path traversal across ~25 API handlers and 3 SSRF vulnerabilities, then configure CodeQL to recognize our sanitizers.

**Architecture:** Single `requireName` validation function (whitelist regex) applied at every handler boundary. Shared `validateOutboundURL` with TOCTOU-safe custom dialer for SSRF. CodeQL model pack to register both as sanitizers.

**Tech Stack:** Go 1.26, chi/v5, net/http/httptest, CodeQL (GitHub Actions)

**Spec:** `docs/specs/input-validation-and-ssrf-hardening.md`

---

### Task 1: `requireName` Validation Helper

**Files:**
- Create: `internal/api/validation.go`
- Create: `internal/api/validation_test.go`

- [ ] **Step 1: Write the test file**

```go
// internal/api/validation_test.go
package api

import (
	"net/http/httptest"
	"testing"
)

func TestRequireName(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		// Valid names
		{"a", true},
		{"ab", true},
		{"my-agent", true},
		{"agent-01", true},
		{"a1b2c3", true},
		{"x", true},

		// Path traversal attacks
		{"..", false},
		{"../etc/passwd", false},
		{"..%2Fevil", false},
		{"../../credentials/store.enc", false},

		// Invalid characters
		{"", false},
		{".", false},
		{"My-Agent", false},
		{"agent_01", false},
		{"agent 01", false},
		{"agent/evil", false},
		{"agent\\evil", false},
		{"-leading-hyphen", false},
		{"trailing-hyphen-", false},

		// Length limit
		{"a234567890123456789012345678901234567890123456789012345678901234", true},  // 64 chars
		{"a2345678901234567890123456789012345678901234567890123456789012345", false}, // 65 chars
	}

	for _, tt := range tests {
		w := httptest.NewRecorder()
		name, ok := requireName(w, tt.input)
		if ok != tt.valid {
			t.Errorf("requireName(%q) = %v, want %v", tt.input, ok, tt.valid)
		}
		if ok && name != tt.input {
			t.Errorf("requireName(%q) returned %q, want same", tt.input, name)
		}
		if !ok && w.Code != 400 {
			t.Errorf("requireName(%q) status = %d, want 400", tt.input, w.Code)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/api/ -run TestRequireName -v`
Expected: FAIL — `requireName` undefined

- [ ] **Step 3: Write the implementation**

```go
// internal/api/validation.go
package api

import (
	"net/http"
	"regexp"
)

// validResourceName matches lowercase alphanumeric names with hyphens, 1-64 chars.
// This is a whitelist — path traversal characters (.., /, \) categorically cannot match.
// Aligns with existing creation-time validators: validateAgentName, reMissionName, validPresetName.
var validResourceName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// requireName validates a user-supplied resource name from a URL param, query param, or JSON body.
// Returns the name and true if valid, or writes a 400 response and returns ("", false).
func requireName(w http.ResponseWriter, raw string) (string, bool) {
	if !validResourceName.MatchString(raw) {
		writeJSON(w, 400, map[string]string{"error": "invalid name"})
		return "", false
	}
	return raw, true
}

// requireNameStr validates a resource name without writing an HTTP response.
// For use in MCP tool handlers and internal functions that format their own errors.
func requireNameStr(name string) bool {
	return validResourceName.MatchString(name)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/api/ -run TestRequireName -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/validation.go internal/api/validation_test.go
git commit -m "feat: add requireName validation helper for path traversal prevention"
```

---

### Task 2: Apply `requireName` to Agent & Team Handlers

**Files:**
- Modify: `internal/api/handlers_agent.go` (lines 150–237: createTeam, showTeam, teamActivity)

- [ ] **Step 1: Add `requireName` to `showTeam`**

In `internal/api/handlers_agent.go`, replace line 185:

```go
// Before:
name := chi.URLParam(r, "name")

// After:
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

- [ ] **Step 2: Add `requireName` to `teamActivity`**

Same file, replace line 201:

```go
// Before:
name := chi.URLParam(r, "name")

// After:
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

- [ ] **Step 3: Add `requireName` to `createTeam`**

Same file. Replace the existing name validation (lines 159–162):

```go
// Before:
if body.Name == "" {
    writeJSON(w, 400, map[string]string{"error": "name required"})
    return
}

// After:
if _, ok := requireName(w, body.Name); !ok {
    return
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/geoff/agency-workspace/agency && go build ./...`
Expected: Compiles cleanly

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers_agent.go
git commit -m "fix: add path traversal validation to team handlers"
```

---

### Task 3: Apply `requireName` to Agent Config, Canvas, Grants, Missions, Trajectory, Events, Capabilities

**Files:**
- Modify: `internal/api/handlers_agent_config.go` (lines 31, 66: agentConfig, updateAgentConfig)
- Modify: `internal/api/handlers_canvas.go` (lines 15, 35, 68: getCanvas, putCanvas, deleteCanvas)
- Modify: `internal/api/handlers_grants.go` (lines 16, 63: grantAgent, revokeAgent)
- Modify: `internal/api/handlers_missions.go` (lines 73, 94, 137, 170, 197, 301, 353, 397, 437, 450, 481, 508)
- Modify: `internal/api/handlers_trajectory.go` (line 19: getAgentTrajectory)
- Modify: `internal/api/handlers_events.go` (lines 99, 160, 176, 196, 258, 333, 415, 436: webhook CRUD + notification handlers)
- Modify: `internal/api/handlers_capabilities.go` (lines 32, 43, 83, 118: capability CRUD)

- [ ] **Step 1: `handlers_agent_config.go` — both handlers**

Replace `name := chi.URLParam(r, "name")` in both `agentConfig` (line 31) and `updateAgentConfig` (line 66):

```go
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

- [ ] **Step 2: `handlers_canvas.go` — all three handlers**

Replace `name := chi.URLParam(r, "name")` in `getCanvas` (line 15), `putCanvas` (line 35), `deleteCanvas` (line 68):

```go
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

- [ ] **Step 3: `handlers_grants.go` — both handlers**

Replace `name := chi.URLParam(r, "name")` in `grantAgent` (line 16) and `revokeAgent` (line 63):

```go
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

- [ ] **Step 4: `handlers_missions.go` — all handlers taking name from URL**

Replace `name := chi.URLParam(r, "name")` in every handler: `showMission` (line 73), `updateMission` (line 94), `missionHealth` (line 137), `deleteMission` (line 170), `assignMission` (line 197), `pauseMission` (line 301), `resumeMission` (line 353), `completeMission` (line 397), `missionHistory` (line 437), `missionKnowledge` (line 450), `claimMissionEvent` (line 481), `releaseMissionClaim` (line 508):

```go
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

- [ ] **Step 5: `handlers_trajectory.go`**

Replace `name := chi.URLParam(r, "name")` in `getAgentTrajectory` (line 19):

```go
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

Add `"path/filepath"` import is NOT needed — we're using the regex, not filepath functions.

- [ ] **Step 6: `handlers_events.go` — webhook and notification name handlers**

Replace `name := chi.URLParam(r, "name")` in `showWebhook` (line 160), `deleteWebhook` (line 176), `rotateWebhookSecret` (line 196), and any other handlers at lines 258, 333, 415, 436:

```go
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

For `createWebhook` (line 99), replace the empty check on `body.Name`:

```go
// Before:
if body.Name == "" || body.EventType == "" {

// After:
if _, ok := requireName(w, body.Name); !ok {
    return
}
if body.EventType == "" {
    writeJSON(w, 400, map[string]string{"error": "event_type required"})
    return
}
```

- [ ] **Step 7: `handlers_capabilities.go` — all handlers**

Replace `name := chi.URLParam(r, "name")` at lines 32, 43, 83, 118:

```go
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

- [ ] **Step 8: Build**

Run: `cd /home/geoff/agency-workspace/agency && go build ./...`
Expected: Compiles cleanly

- [ ] **Step 9: Commit**

```bash
git add internal/api/handlers_agent_config.go internal/api/handlers_canvas.go \
    internal/api/handlers_grants.go internal/api/handlers_missions.go \
    internal/api/handlers_trajectory.go internal/api/handlers_events.go \
    internal/api/handlers_capabilities.go
git commit -m "fix: add path traversal validation to config, canvas, grants, missions, trajectory, events, capabilities handlers"
```

---

### Task 4: Apply `requireName` to Admin, Policy, Routes, Connector, Hub, Manifest, Presets

**Files:**
- Modify: `internal/api/handlers_admin.go` (lines 435, 548, 626, 815, 835)
- Modify: `internal/api/routes.go` (lines 1176, 1183)
- Modify: `internal/api/handlers_connector_setup.go` (line 229 in `loadConnectorConfig`)
- Modify: `internal/api/handlers_hub.go` (`autoActivate`)
- Modify: `internal/api/manifest.go` (lines 20, 221)
- Modify: `internal/api/handlers_presets.go` (replace `validPresetName` with shared `requireName`)

- [ ] **Step 1: `handlers_admin.go` — adminTrust**

After the agent resolution block (line 438), add validation:

```go
// After: agent = body.Args["agent"]
// Add:
if agent != "" {
    if !requireNameStr(agent) {
        writeJSON(w, 400, map[string]string{"error": "invalid agent name"})
        return
    }
}
```

Note: the "list" action (line 441) does not use `agent` in a path — it iterates `os.ReadDir`. Only validate when agent is non-empty and will be used in a path.

- [ ] **Step 2: `handlers_admin.go` — adminAudit**

After line 551 (`agent := q.Get("agent")`), add:

```go
if agent != "" && !requireNameStr(agent) {
    writeJSON(w, 400, map[string]string{"error": "invalid agent name"})
    return
}
```

Note: `agent` can be empty (means "all agents" for stats/list). Only validate when non-empty.

- [ ] **Step 3: `handlers_admin.go` — adminEgress**

After line 626 (`agent := r.URL.Query().Get("agent")`), replace the empty check:

```go
// Before:
if agent == "" {
    writeJSON(w, 400, map[string]string{"error": "agent query parameter required"})
    return
}

// After:
if _, ok := requireName(w, agent); !ok {
    return
}
```

- [ ] **Step 4: `handlers_admin.go` — adminDepartment "show" case**

Replace line 815:

```go
// Before:
name := body.Args["name"]
if name == "" {
    writeJSON(w, 400, map[string]string{"error": "name required"})
    return
}

// After:
name := body.Args["name"]
if _, ok := requireName(w, name); !ok {
    return
}
```

- [ ] **Step 5: `handlers_admin.go` — rebuildAgent**

Replace line 835:

```go
// Before:
name := chi.URLParam(r, "name")

// After:
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

- [ ] **Step 6: `routes.go` — showPolicy and validatePolicy**

Replace `agent := chi.URLParam(r, "agent")` in `showPolicy` (line 1176) and `validatePolicy` (line 1183):

```go
agent, ok := requireName(w, chi.URLParam(r, "agent"))
if !ok {
    return
}
```

- [ ] **Step 7: `handlers_connector_setup.go` — loadConnectorConfig**

At the top of `loadConnectorConfig` (line 229), add:

```go
if !requireNameStr(name) {
    return nil, fmt.Errorf("invalid connector name")
}
```

- [ ] **Step 8: `handlers_hub.go` — autoActivate**

At the top of `autoActivate`, add validation of `inst.Name` and `inst.Kind`:

```go
if !requireNameStr(inst.Name) || !requireNameStr(inst.Kind) {
    h.log.Warn("invalid hub instance name or kind", "name", inst.Name, "kind", inst.Kind)
    return
}
```

- [ ] **Step 9: `manifest.go` — generateAgentManifest and loadPresetScopes**

These are called from handler code that already validates. Add a defensive check at each entry anyway (these are the two functions the PR was trying to protect):

In `generateAgentManifest` (line 20), at the top:
```go
if !requireNameStr(agentName) {
    return fmt.Errorf("invalid agent name")
}
```

In `loadPresetScopes` (line 221), at the top:
```go
if !requireNameStr(agentName) {
    return nil
}
```

- [ ] **Step 10: `handlers_presets.go` — replace `validPresetName`**

Replace all uses of `validPresetName.MatchString(name)` in `getPreset` (line 155), `createPreset` (line 191), `updatePreset` (line 225), `deletePreset` (line 267) with `requireName`. For `getPreset`, `updatePreset`, `deletePreset`:

```go
// Before:
name := chi.URLParam(r, "name")
if !validPresetName.MatchString(name) {
    writeJSON(w, 400, map[string]string{"error": "invalid preset name"})
    return
}

// After:
name, ok := requireName(w, chi.URLParam(r, "name"))
if !ok {
    return
}
```

For `createPreset` (body name):
```go
// Before:
if !validPresetName.MatchString(body.Name) {
    writeJSON(w, 400, map[string]string{"error": "invalid preset name"})
    return
}

// After:
if _, ok := requireName(w, body.Name); !ok {
    return
}
```

Remove the `validPresetName` regex declaration (line 52) after all uses are replaced.

- [ ] **Step 11: Build**

Run: `cd /home/geoff/agency-workspace/agency && go build ./...`
Expected: Compiles cleanly

- [ ] **Step 12: Commit**

```bash
git add internal/api/handlers_admin.go internal/api/routes.go \
    internal/api/handlers_connector_setup.go internal/api/handlers_hub.go \
    internal/api/manifest.go internal/api/handlers_presets.go
git commit -m "fix: add path traversal validation to admin, policy, connector, hub, manifest, preset handlers"
```

---

### Task 5: Apply `requireNameStr` to MCP Tool Handlers

**Files:**
- Modify: `internal/api/mcp_register.go`
- Modify: `internal/api/mcp_admin.go`

- [ ] **Step 1: Add validation helper for MCP handlers**

MCP tool handlers use `mapStr(args, "name")` / `mapStr(args, "agent")` and return `(string, bool)`. They don't have `http.ResponseWriter` for `requireName`. Use `requireNameStr` instead.

In `internal/api/mcp_register.go`, find each `name := mapStr(args, "agent")` or `name := mapStr(args, "name")` call that flows into filesystem operations. After each extraction, add:

```go
if !requireNameStr(name) {
    return `{"error":"invalid name"}`, false
}
```

Apply to lines: 263, 294, 374, 429, 465, 538, 565, 594, 649, 712, 863, 889, 922.

- [ ] **Step 2: Same for `mcp_admin.go`**

Apply the same pattern after `mapStr` calls at lines: 345, 572, 687, 728, 977, 1004, 1080, 1146, 1176, 1204.

For the `agent` extractions where empty is valid (like adminAudit stats), use:

```go
if agent != "" && !requireNameStr(agent) {
    return `{"error":"invalid agent name"}`, false
}
```

- [ ] **Step 3: Build**

Run: `cd /home/geoff/agency-workspace/agency && go build ./...`
Expected: Compiles cleanly

- [ ] **Step 4: Commit**

```bash
git add internal/api/mcp_register.go internal/api/mcp_admin.go
git commit -m "fix: add path traversal validation to MCP tool handlers"
```

---

### Task 6: `validateOutboundURL` with TOCTOU-Safe Dialer

**Files:**
- Create: `internal/api/urlvalidation.go`
- Create: `internal/api/urlvalidation_test.go`

- [ ] **Step 1: Write the test file**

```go
// internal/api/urlvalidation_test.go
package api

import (
	"testing"
)

func TestValidateOutboundURL(t *testing.T) {
	tests := []struct {
		url   string
		valid bool
	}{
		// Valid
		{"https://ntfy.sh/test", true},
		{"https://hooks.slack.com/services/T0/B0/abc", true},

		// Invalid scheme
		{"http://external.com/hook", false},
		{"ftp://evil.com", false},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},

		// http allowed for localhost (ntfy dev)
		{"http://localhost:8080/test", true},
		{"http://127.0.0.1:8080/test", true},

		// Private IPs blocked
		{"https://169.254.169.254/latest/meta-data/", false},
		{"https://10.0.0.1/internal", false},
		{"https://172.16.0.1/internal", false},
		{"https://192.168.1.1/internal", false},
		{"https://metadata.google.internal/computeMetadata/v1/", false},

		// Loopback blocked for https (no reason to https to yourself)
		{"https://localhost/hook", false},
		{"https://127.0.0.1/hook", false},

		// Empty / garbage
		{"", false},
		{"not-a-url", false},
	}

	for _, tt := range tests {
		err := validateOutboundURL(tt.url)
		if (err == nil) != tt.valid {
			t.Errorf("validateOutboundURL(%q) error=%v, wantValid=%v", tt.url, err, tt.valid)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		host    string
		private bool
	}{
		{"169.254.169.254", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},
	}

	for _, tt := range tests {
		result := isPrivateIP(tt.host)
		if result != tt.private {
			t.Errorf("isPrivateIP(%q) = %v, want %v", tt.host, result, tt.private)
		}
	}
}

func TestSafeDialer(t *testing.T) {
	// Verify the dialer is constructable and returns the right type
	d := newSafeDialer()
	if d == nil {
		t.Fatal("newSafeDialer returned nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/api/ -run "TestValidateOutboundURL|TestIsPrivateIP|TestSafeDialer" -v`
Expected: FAIL — functions undefined

- [ ] **Step 3: Write the implementation**

```go
// internal/api/urlvalidation.go
package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// validateOutboundURL checks that a URL is safe for the gateway to make outbound requests to.
// Rejects private IPs, non-https schemes (except http for localhost), and known metadata endpoints.
func validateOutboundURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("empty URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := parsed.Hostname()
	isLocalhost := host == "localhost" || host == "127.0.0.1" || host == "::1"

	// Scheme check: https required, http only for localhost
	switch parsed.Scheme {
	case "https":
		if isLocalhost {
			return fmt.Errorf("https to localhost is not allowed; use http")
		}
	case "http":
		if !isLocalhost {
			return fmt.Errorf("http scheme only allowed for localhost, got host %q", host)
		}
	default:
		return fmt.Errorf("scheme %q not allowed; use https", parsed.Scheme)
	}

	// Hostname blocklist (string-level, before DNS)
	if isBlockedHostname(host) {
		return fmt.Errorf("host %q is blocked", host)
	}

	// IP blocklist (catches raw IP addresses in the URL)
	if isPrivateIP(host) {
		return fmt.Errorf("private/reserved IP %q is blocked", host)
	}

	return nil
}

// isBlockedHostname checks for known dangerous hostnames.
func isBlockedHostname(host string) bool {
	lower := strings.ToLower(host)
	if strings.HasSuffix(lower, ".internal") || lower == "metadata.google.internal" {
		return true
	}
	return false
}

// isPrivateIP returns true if the string is a private, loopback, or link-local IP.
func isPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// newSafeDialer returns a net.Dialer wrapped with IP validation at connect time.
// This defends against DNS rebinding: even if a hostname resolves to a public IP at
// parse time, the dialer rejects connections to private IPs at the actual connect moment.
func newSafeDialer() *net.Dialer {
	return &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, c interface{}) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if isPrivateIP(host) {
				return fmt.Errorf("connection to private IP %s blocked", host)
			}
			return nil
		},
	}
}

// SafeHTTPClient returns an http.Client that rejects connections to private IPs.
// Use this for any gateway-originated outbound request to user-supplied URLs.
func SafeHTTPClient() *http.Client {
	dialer := newSafeDialer()
	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/api/ -run "TestValidateOutboundURL|TestIsPrivateIP|TestSafeDialer" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/urlvalidation.go internal/api/urlvalidation_test.go
git commit -m "feat: add validateOutboundURL with TOCTOU-safe dialer for SSRF prevention"
```

---

### Task 7: Apply SSRF Validation to Notification Webhooks

**Files:**
- Modify: `internal/api/handlers_events.go` (line 367 in `addNotification`)

- [ ] **Step 1: Add URL validation after the empty check**

In `addNotification`, after line 367 (`if body.Name == "" || body.URL == ""`), add:

```go
if err := validateOutboundURL(body.URL); err != nil {
    writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("invalid notification URL: %s", err)})
    return
}
```

Add `"fmt"` to the imports if not already present.

- [ ] **Step 2: Update the existing test**

In `internal/api/handlers_notifications_test.go`, add a test case after the existing `TestAddNotification`:

```go
func TestAddNotification_RejectsPrivateIP(t *testing.T) {
	h, _, _ := notifTestHandler(t)

	body := `{"name":"evil","type":"webhook","url":"https://169.254.169.254/latest/meta-data/","events":["operator_alert"]}`
	req := httptest.NewRequest("POST", "/api/v1/events/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for private IP, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddNotification_RejectsHTTP(t *testing.T) {
	h, _, _ := notifTestHandler(t)

	body := `{"name":"evil","type":"webhook","url":"http://external.com/hook","events":["operator_alert"]}`
	req := httptest.NewRequest("POST", "/api/v1/events/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for http to non-localhost, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 3: Run tests**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/api/ -run TestAddNotification -v`
Expected: PASS (both existing and new tests)

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_events.go internal/api/handlers_notifications_test.go
git commit -m "fix: validate notification webhook URLs against SSRF"
```

---

### Task 8: Apply SSRF Validation to Credential Test Endpoints

**Files:**
- Modify: `internal/credstore/store.go` (lines 352, 383)

- [ ] **Step 1: Add URL validation to `testJWTExchange`**

In `store.go`, at line 355 (after the tokenURL extraction), add:

```go
// Import "github.com/geoffbelknap/agency/internal/api" is not ideal (circular risk).
// Instead, inline the validation here. The credential store should not depend on the API package.
```

Since `credstore` cannot import `internal/api` (it would create a dependency from a lower-level package to a higher-level one), we need to either:
- Move `validateOutboundURL` and `isPrivateIP` to a shared package, or
- Duplicate the minimal validation inline.

Move the URL validation to a shared location. Create `internal/pkg/urlsafety/urlsafety.go`:

```go
// internal/pkg/urlsafety/urlsafety.go
package urlsafety

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Validate checks that a URL is safe for outbound requests.
// Rejects private IPs, non-https schemes (except http for localhost), and known metadata endpoints.
func Validate(raw string) error {
	if raw == "" {
		return fmt.Errorf("empty URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := parsed.Hostname()
	isLocalhost := host == "localhost" || host == "127.0.0.1" || host == "::1"

	switch parsed.Scheme {
	case "https":
		if isLocalhost {
			return fmt.Errorf("https to localhost is not allowed; use http")
		}
	case "http":
		if !isLocalhost {
			return fmt.Errorf("http scheme only allowed for localhost, got host %q", host)
		}
	default:
		return fmt.Errorf("scheme %q not allowed; use https", parsed.Scheme)
	}

	if isBlockedHostname(host) {
		return fmt.Errorf("host %q is blocked", host)
	}

	if IsPrivateIP(host) {
		return fmt.Errorf("private/reserved IP %q is blocked", host)
	}

	return nil
}

func isBlockedHostname(host string) bool {
	lower := strings.ToLower(host)
	return strings.HasSuffix(lower, ".internal") || lower == "metadata.google.internal"
}

// IsPrivateIP returns true if the string is a private, loopback, or link-local IP.
func IsPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// SafeClient returns an http.Client that rejects connections to private IPs at connect time.
func SafeClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, c interface{}) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if IsPrivateIP(host) {
				return fmt.Errorf("connection to private IP %s blocked", host)
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
		},
		Timeout: 10 * time.Second,
	}
}
```

- [ ] **Step 2: Update `internal/api/urlvalidation.go` to delegate to the shared package**

Replace `internal/api/urlvalidation.go` to be a thin wrapper:

```go
package api

import "github.com/geoffbelknap/agency/internal/pkg/urlsafety"

// validateOutboundURL checks that a URL is safe for gateway outbound requests.
func validateOutboundURL(raw string) error {
	return urlsafety.Validate(raw)
}
```

Update `internal/api/urlvalidation_test.go` to also test via the shared package (or just keep testing via the wrapper — the tests still pass since `validateOutboundURL` delegates).

- [ ] **Step 3: Apply to `testJWTExchange`**

In `internal/credstore/store.go`, add to `testJWTExchange` after the tokenURL check (line 355):

```go
if err := urlsafety.Validate(tokenURL); err != nil {
    return &TestResult{OK: false, Message: fmt.Sprintf("unsafe token URL: %s", err)}
}
```

Replace the `http.Client` (line 368):

```go
// Before:
client := &http.Client{Timeout: 10 * time.Second}

// After:
client := urlsafety.SafeClient()
```

Add import: `"github.com/geoffbelknap/agency/internal/pkg/urlsafety"`

- [ ] **Step 4: Apply to `testAPIKey`**

In `testAPIKey`, after `testURL` construction (line 395), add:

```go
if err := urlsafety.Validate(testURL); err != nil {
    return &TestResult{OK: false, Message: fmt.Sprintf("unsafe test URL: %s", err)}
}
```

Replace the `http.Client` (line 412):

```go
// Before:
client := &http.Client{Timeout: 10 * time.Second}

// After:
client := urlsafety.SafeClient()
```

- [ ] **Step 5: Run tests**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/credstore/ -v && go test ./internal/api/ -run "TestValidateOutboundURL|TestIsPrivateIP" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/urlsafety/urlsafety.go internal/api/urlvalidation.go \
    internal/api/urlvalidation_test.go internal/credstore/store.go
git commit -m "fix: add SSRF validation to credential test endpoints"
```

---

### Task 9: Fix Web-Fetch Redirect Blocklist Bypass

**Files:**
- Modify: `images/web-fetch/main.go` (lines 133–179: `buildHTTPClient`)

- [ ] **Step 1: Pass blocklist into `buildHTTPClient`**

Change the function signature:

```go
// Before:
func buildHTTPClient(cfg Config) *http.Client {

// After:
func buildHTTPClient(cfg Config, blocklist *Blocklist) *http.Client {
```

- [ ] **Step 2: Add blocklist check to CheckRedirect**

Replace the redirect-following block (lines 169–175):

```go
// Before:
} else {
    client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
        if len(via) >= maxRedirects {
            return http.ErrUseLastResponse
        }
        return nil
    }
}

// After:
} else {
    client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
        if len(via) >= maxRedirects {
            return http.ErrUseLastResponse
        }
        // Re-validate redirect target against blocklist (SSRF defense).
        if blocklist != nil && blocklist.IsBlocked(req.URL.Hostname()) {
            return http.ErrUseLastResponse
        }
        return nil
    }
}
```

- [ ] **Step 3: Update the caller**

Find where `buildHTTPClient` is called in `main.go` and pass the blocklist. In the `main()` function, the blocklist is built before the client. Update:

```go
// Before:
client := buildHTTPClient(cfg)

// After:
client := buildHTTPClient(cfg, blocklist)
```

- [ ] **Step 4: Add a test**

Add to `images/web-fetch/integration_test.go` (or create a new `redirect_test.go`):

```go
func TestRedirectToBlockedHost(t *testing.T) {
	bl := NewBlocklist()
	bl.Add("evil.internal")

	cfg := Config{Fetch: FetchConfig{
		FollowRedirects: true,
		MaxRedirects:    5,
		TimeoutSeconds:  5,
	}}
	client := buildHTTPClient(cfg, bl)

	// Create a test server that redirects to a "blocked" host
	// The redirect won't actually succeed (no DNS for evil.internal),
	// but we verify the CheckRedirect fires.
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.internal/payload", http.StatusFound)
	}))
	defer redirectServer.Close()

	resp, err := client.Get(redirectServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should get the redirect response (302), not follow it
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 (redirect stopped), got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd /home/geoff/agency-workspace/agency && go test ./images/web-fetch/ -run TestRedirectToBlockedHost -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add images/web-fetch/main.go images/web-fetch/redirect_test.go
git commit -m "fix: re-check blocklist on HTTP redirects in web-fetch (SSRF)"
```

---

### Task 10: CodeQL Configuration

**Files:**
- Create: `.github/workflows/codeql.yml`
- Create: `.github/codeql/codeql-config.yml`
- Create: `.github/codeql/extensions/agency-sanitizers.yml`
- Modify: `.github/workflows/ci.yml` (add permissions)

- [ ] **Step 1: Create CodeQL workflow**

```yaml
# .github/workflows/codeql.yml
name: "CodeQL"

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  schedule:
    - cron: "0 6 * * 1" # Weekly Monday 6am UTC

permissions:
  contents: read
  security-events: write

jobs:
  analyze:
    name: Analyze
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: github/codeql-action/init@v3
        with:
          languages: go
          config-file: .github/codeql/codeql-config.yml

      - uses: github/codeql-action/autobuild@v3

      - uses: github/codeql-action/analyze@v3
```

- [ ] **Step 2: Create CodeQL config**

```yaml
# .github/codeql/codeql-config.yml
name: agency-codeql-config

queries:
  - uses: security-and-quality

paths-ignore:
  - images/tests
  - vendor

model-packs:
  - .github/codeql/extensions
```

- [ ] **Step 3: Create sanitizer model pack**

```yaml
# .github/codeql/extensions/agency-sanitizers.yml
extensions:
  - addsTo:
      pack: codeql/go-all
      extensible: sinkModel
    data: []
  - addsTo:
      pack: codeql/go-all
      extensible: sourceModel
    data: []
  - addsTo:
      pack: codeql/go-all
      extensible: summaryModel
    data: []
  - addsTo:
      pack: codeql/go-all
      extensible: neutralModel
    data:
      # requireName is a sanitizer for path injection — it validates input against
      # a whitelist regex that categorically prevents path traversal characters.
      - ["github.com/geoffbelknap/agency/internal/api", "requireName", "PathInjection"]
      - ["github.com/geoffbelknap/agency/internal/api", "requireNameStr", "PathInjection"]
      # validateOutboundURL is a sanitizer for SSRF — it validates URL scheme and host.
      - ["github.com/geoffbelknap/agency/internal/api", "validateOutboundURL", "RequestForgery"]
      - ["github.com/geoffbelknap/agency/internal/pkg/urlsafety", "Validate", "RequestForgery"]
```

- [ ] **Step 4: Add permissions to CI workflow**

In `.github/workflows/ci.yml`, add after line 6 (`branches: [main]`):

```yaml
permissions:
  contents: read
```

- [ ] **Step 5: Build to verify no breakage**

Run: `cd /home/geoff/agency-workspace/agency && go build ./... && go test ./internal/api/ -v`
Expected: All pass (CodeQL config is YAML only, doesn't affect Go build)

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/codeql.yml .github/codeql/codeql-config.yml \
    .github/codeql/extensions/agency-sanitizers.yml .github/workflows/ci.yml
git commit -m "feat: add CodeQL scanning with custom sanitizer model pack"
```

---

### Task 11: Full Test Suite & Cleanup

**Files:**
- All modified files

- [ ] **Step 1: Run full Go test suite**

Run: `cd /home/geoff/agency-workspace/agency && go test ./...`
Expected: All tests pass

- [ ] **Step 2: Run Go build for all targets**

Run: `cd /home/geoff/agency-workspace/agency && go build ./cmd/gateway/ && go build ./images/enforcer/ && go build ./images/web-fetch/`
Expected: All compile cleanly

- [ ] **Step 3: Verify no unused imports**

Run: `cd /home/geoff/agency-workspace/agency && go vet ./...`
Expected: No errors

- [ ] **Step 4: Delete the spec plan doc (plan is complete, spec remains)**

The plan file stays until work is fully implemented. The spec at `docs/specs/input-validation-and-ssrf-hardening.md` is kept as architectural reference.

- [ ] **Step 5: Final commit if any cleanup was needed**

```bash
git add -A && git commit -m "chore: cleanup after input validation and SSRF hardening"
```
