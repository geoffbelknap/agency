# Agency Relay — Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the relay client, token model, gateway endpoints, CLI commands, and operator network that let an operator tunnel their local Agency through Cloudflare — with the relay running as a Docker container alongside the other infra services.

**Architecture:** New `internal/relay/` package with config, HMAC-signed tokens, tunnel client (multiplexed WSS), and local proxy. Gateway gains `/api/v1/relay/*` endpoints. CLI gains `agency relay` command group. Relay runs as a Docker container on a new `agency-operator` network (shared with agency-web, isolated from mediation). The Cloudflare Worker/DO side is a separate plan.

**Tech Stack:** Go, gorilla/websocket (already in go.mod), crypto/hmac, chi router, cobra CLI — no new dependencies.

**Spec:** `docs/specs/agency-relay.md`

---

## Network Architecture

```
agency-mediation (not Internal)              agency-operator (not Internal)
├── gateway-proxy (restricted socket API)    ├── agency-web     (host-gateway → full API)
├── comms                                    ├── agency-relay   (host-gateway → full API
├── knowledge                                │                   + outbound to Cloudflare)
├── intake                                   └── (isolated from mediation containers)
├── web-fetch
├── egress
└── enforcer(s)
```

Operator containers use `ExtraHosts: ["gateway:host-gateway"]` to reach the full gateway API on `localhost:8200` — the same pattern agency-web already uses. The `agency-operator` network is non-Internal (allows outbound for relay → Cloudflare WSS). Mediation and operator containers cannot see each other.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/relay/config.go` | Create | `RelayConfig` struct, load/save `~/.agency/relay.yaml` |
| `internal/relay/config_test.go` | Create | Round-trip, defaults, file-not-found, permissions |
| `internal/relay/token.go` | Create | Access token gen (HMAC-SHA256), refresh secret gen, validation |
| `internal/relay/token_test.go` | Create | Sign/verify, expiry, tamper detection, refresh secret format |
| `internal/relay/protocol.go` | Create | Frame types (handshake, request, response, ws_open, ws_frame, ws_close, token_refresh) |
| `internal/relay/protocol_test.go` | Create | Marshal/unmarshal round-trips |
| `internal/relay/client.go` | Create | Tunnel client: WSS dial, handshake, frame mux, reconnection, token refresh loop |
| `internal/relay/client_test.go` | Create | Handshake, reconnect backoff, token refresh scheduling |
| `internal/relay/proxy.go` | Create | Receives request frames, calls gateway, returns response frames; WS bridge |
| `internal/relay/proxy_test.go` | Create | Request/response proxying, timeout, headers |
| `internal/api/handlers_relay.go` | Create | HTTP handlers for `/api/v1/relay/*` + `buildAgencyConfig` helper |
| `internal/api/handlers_relay_test.go` | Create | Handler tests |
| `internal/api/middleware_auth.go` | Modify | Add refresh-secret scoped auth (like egress token pattern) |
| `internal/api/middleware_auth_test.go` | Modify | Add refresh-secret auth tests |
| `internal/api/routes.go` | Modify | Register relay routes, update `/__agency/config` |
| `internal/apiclient/client.go` | Modify | Add `RelayConnect/Disconnect/Status/Revoke/Reissue/Destroy` methods |
| `internal/cli/commands.go` | Modify | Add `relayCmd()` group with subcommands |
| `internal/orchestrate/containers/networks.go` | Modify | Add `CreateOperatorNetwork()` factory |
| `internal/orchestrate/containers/networks_test.go` | Modify | Test operator network creation |
| `internal/orchestrate/infra.go` | Modify | Add `agency-operator` to `ensureNetworks()`, add `ensureRelay()`, migrate agency-web, add to startup |
| `images/relay/Dockerfile` | Create | Minimal relay container image |
| `cmd/gateway/main.go` | Modify | Pass relay secret to BearerAuth |

---

### Task 1: Relay Config

**Files:**
- Create: `internal/relay/config.go`
- Create: `internal/relay/config_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/relay/config_test.go
package relay

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRelayConfig_FileNotFound(t *testing.T) {
	cfg, err := LoadRelayConfig("/nonexistent")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.Enabled {
		t.Error("expected Enabled=false for missing config")
	}
	if cfg.RelayToken != "" {
		t.Error("expected empty relay token")
	}
}

func TestRelayConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &RelayConfig{
		RelayToken:          "rt_abc123",
		RefreshSecret:       "rs_secret456",
		AccountID:           "acc_789",
		RelayURL:            "wss://relay.tinyfleck.io/tunnel",
		Enabled:             true,
		TrustAcknowledged:   true,
		TrustAcknowledgedAt: "2026-04-04T12:00:00Z",
	}

	if err := original.Save(dir); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadRelayConfig(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.RelayToken != original.RelayToken {
		t.Errorf("relay_token: got %q, want %q", loaded.RelayToken, original.RelayToken)
	}
	if loaded.RefreshSecret != original.RefreshSecret {
		t.Errorf("refresh_secret: got %q, want %q", loaded.RefreshSecret, original.RefreshSecret)
	}
	if loaded.AccountID != original.AccountID {
		t.Errorf("account_id: got %q, want %q", loaded.AccountID, original.AccountID)
	}
	if loaded.RelayURL != original.RelayURL {
		t.Errorf("relay_url: got %q, want %q", loaded.RelayURL, original.RelayURL)
	}
	if !loaded.Enabled {
		t.Error("expected Enabled=true")
	}
	if !loaded.TrustAcknowledged {
		t.Error("expected TrustAcknowledged=true")
	}
}

func TestRelayConfig_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	cfg := &RelayConfig{RelayToken: "rt_test"}
	if err := cfg.Save(dir); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "relay.yaml"))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions: got %o, want 0600", perm)
	}
}

func TestRelayConfig_Clear(t *testing.T) {
	dir := t.TempDir()
	cfg := &RelayConfig{
		RelayToken:    "rt_test",
		RefreshSecret: "rs_test",
		AccountID:     "acc_test",
		Enabled:       true,
	}
	cfg.Save(dir)
	cfg.Clear()

	if cfg.RelayToken != "" || cfg.RefreshSecret != "" || cfg.AccountID != "" || cfg.Enabled {
		t.Error("Clear did not reset all fields")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd agency && go test ./internal/relay/ -v -run TestLoad`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write implementation**

```go
// internal/relay/config.go
package relay

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RelayConfig holds the operator's relay connection state.
// Stored at ~/.agency/relay.yaml. File permissions: 0600.
type RelayConfig struct {
	RelayToken          string `yaml:"relay_token,omitempty"`
	RefreshSecret       string `yaml:"refresh_secret,omitempty"`
	AccountID           string `yaml:"account_id,omitempty"`
	RelayURL            string `yaml:"relay_url,omitempty"`
	Enabled             bool   `yaml:"enabled"`
	TrustAcknowledged   bool   `yaml:"trust_acknowledged,omitempty"`
	TrustAcknowledgedAt string `yaml:"trust_acknowledged_at,omitempty"`
}

const relayConfigFile = "relay.yaml"

// LoadRelayConfig reads ~/.agency/relay.yaml. Returns zero-value config if
// the file does not exist (first run).
func LoadRelayConfig(home string) (*RelayConfig, error) {
	path := filepath.Join(home, relayConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RelayConfig{}, nil
		}
		return nil, err
	}

	var cfg RelayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save writes the config to ~/.agency/relay.yaml with 0600 permissions.
func (c *RelayConfig) Save(home string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, relayConfigFile), data, 0600)
}

// Clear resets all credential and state fields. Call before Save to wipe.
func (c *RelayConfig) Clear() {
	c.RelayToken = ""
	c.RefreshSecret = ""
	c.AccountID = ""
	c.Enabled = false
	c.TrustAcknowledged = false
	c.TrustAcknowledgedAt = ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd agency && go test ./internal/relay/ -v`
Expected: PASS (all 4 tests)

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/relay/config.go internal/relay/config_test.go
git commit -m "feat(relay): add relay config load/save with tests"
```

---

### Task 2: Access Token Generation and Validation

**Files:**
- Create: `internal/relay/token.go`
- Create: `internal/relay/token_test.go`

The spec defines HMAC-SHA256 signed tokens: `base64(payload) + "." + base64(hmac)`. Gateway validates statelessly.

- [ ] **Step 1: Write failing tests**

```go
// internal/relay/token_test.go
package relay

import (
	"crypto/rand"
	"testing"
	"time"
)

func testHMACKey() []byte {
	key := make([]byte, 32)
	rand.Read(key)
	return key
}

func TestGenerateAccessToken(t *testing.T) {
	key := testHMACKey()
	tok, err := GenerateAccessToken(key, 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestValidateAccessToken_Valid(t *testing.T) {
	key := testHMACKey()
	tok, _ := GenerateAccessToken(key, 15*time.Minute)

	claims, err := ValidateAccessToken(tok, key)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.Subject != "relay" {
		t.Errorf("subject: got %q, want %q", claims.Subject, "relay")
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	key := testHMACKey()
	tok, _ := GenerateAccessToken(key, -1*time.Minute)

	_, err := ValidateAccessToken(tok, key)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateAccessToken_Tampered(t *testing.T) {
	key := testHMACKey()
	tok, _ := GenerateAccessToken(key, 15*time.Minute)

	tampered := []byte(tok)
	if tampered[0] == 'a' {
		tampered[0] = 'b'
	} else {
		tampered[0] = 'a'
	}

	_, err := ValidateAccessToken(string(tampered), key)
	if err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestValidateAccessToken_WrongKey(t *testing.T) {
	key1 := testHMACKey()
	key2 := testHMACKey()
	tok, _ := GenerateAccessToken(key1, 15*time.Minute)

	_, err := ValidateAccessToken(tok, key2)
	if err == nil {
		t.Fatal("expected error for wrong key")
	}
}

func TestGenerateRefreshSecret(t *testing.T) {
	secret := GenerateRefreshSecret()
	if secret == "" {
		t.Fatal("expected non-empty secret")
	}
	if len(secret) < 40 {
		t.Errorf("secret too short: %d chars", len(secret))
	}
	if secret[:3] != "rs_" {
		t.Errorf("expected rs_ prefix, got %q", secret[:3])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd agency && go test ./internal/relay/ -v -run TestGenerate`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write implementation**

```go
// internal/relay/token.go
package relay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AccessTokenClaims is the payload of a relay access token.
type AccessTokenClaims struct {
	Subject  string `json:"sub"`
	IssuedAt int64  `json:"iat"`
	Expiry   int64  `json:"exp"`
	JTI      string `json:"jti"`
}

// GenerateAccessToken creates an HMAC-SHA256 signed access token.
// Format: base64url(json_payload).base64url(hmac_sha256(payload, key))
func GenerateAccessToken(hmacKey []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := AccessTokenClaims{
		Subject:  "relay",
		IssuedAt: now.Unix(),
		Expiry:   now.Add(ttl).Unix(),
		JTI:      uuid.New().String(),
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte(encodedPayload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encodedPayload + "." + sig, nil
}

// ValidateAccessToken verifies the HMAC signature and checks expiry.
func ValidateAccessToken(token string, hmacKey []byte) (*AccessTokenClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed token")
	}
	encodedPayload, encodedSig := parts[0], parts[1]

	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte(encodedPayload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(encodedSig), []byte(expectedSig)) {
		return nil, errors.New("invalid signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return nil, errors.New("malformed payload")
	}

	var claims AccessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.New("malformed claims")
	}

	if claims.Subject != "relay" {
		return nil, errors.New("invalid subject")
	}

	if time.Now().Unix() > claims.Expiry {
		return nil, errors.New("token expired")
	}

	return &claims, nil
}

// GenerateRefreshSecret creates a long-lived secret for local token refresh.
// Prefixed with "rs_" for identification. Never leaves the operator's machine.
func GenerateRefreshSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "rs_" + hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd agency && go test ./internal/relay/ -v -run "TestGenerate|TestValidate"`
Expected: PASS (all 5 tests)

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/relay/token.go internal/relay/token_test.go
git commit -m "feat(relay): HMAC-signed access tokens and refresh secrets"
```

---

### Task 3: Tunnel Protocol Frame Types

**Files:**
- Create: `internal/relay/protocol.go`
- Create: `internal/relay/protocol_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/relay/protocol_test.go
package relay

import (
	"encoding/json"
	"testing"
)

func TestFrame_MarshalHandshake(t *testing.T) {
	f := Frame{
		Type:          FrameHandshake,
		Version:       1,
		RelayToken:    "rt_abc",
		AccessToken:   "at_xyz",
		AgencyVersion: "0.8.2",
		AgencyBuild:   "a1b2c3d",
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Frame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != FrameHandshake {
		t.Errorf("type: got %q, want %q", decoded.Type, FrameHandshake)
	}
	if decoded.Version != 1 {
		t.Errorf("version: got %d, want 1", decoded.Version)
	}
}

func TestFrame_MarshalRequest(t *testing.T) {
	f := Frame{
		Type:    FrameRequest,
		ID:      "req_001",
		Method:  "GET",
		Path:    "/api/v1/agents",
		Headers: map[string]string{"Accept": "application/json"},
	}
	data, _ := json.Marshal(f)
	var decoded Frame
	json.Unmarshal(data, &decoded)
	if decoded.ID != "req_001" || decoded.Method != "GET" || decoded.Path != "/api/v1/agents" {
		t.Errorf("request frame round-trip failed: %+v", decoded)
	}
}

func TestFrame_MarshalResponse(t *testing.T) {
	f := Frame{
		Type:    FrameResponse,
		ID:      "req_001",
		Status:  200,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    json.RawMessage(`[{"name":"analyst"}]`),
	}
	data, _ := json.Marshal(f)
	var decoded Frame
	json.Unmarshal(data, &decoded)
	if decoded.Status != 200 {
		t.Errorf("status: got %d, want 200", decoded.Status)
	}
	if string(decoded.Body) != `[{"name":"analyst"}]` {
		t.Errorf("body: got %s", decoded.Body)
	}
}

func TestFrame_MarshalWSOpen(t *testing.T) {
	f := Frame{Type: FrameWSOpen, ID: "ws_001", Path: "/ws"}
	data, _ := json.Marshal(f)
	var decoded Frame
	json.Unmarshal(data, &decoded)
	if decoded.Type != FrameWSOpen || decoded.ID != "ws_001" {
		t.Errorf("ws_open round-trip failed: %+v", decoded)
	}
}

func TestFrame_MarshalWSFrame(t *testing.T) {
	payload := json.RawMessage(`{"v":1,"type":"agent_status"}`)
	f := Frame{Type: FrameWSFrame, ID: "ws_001", Data: payload}
	data, _ := json.Marshal(f)
	var decoded Frame
	json.Unmarshal(data, &decoded)
	if string(decoded.Data) != string(payload) {
		t.Errorf("ws_frame data mismatch")
	}
}

func TestFrame_MarshalTokenRefresh(t *testing.T) {
	f := Frame{Type: FrameTokenRefresh, AccessToken: "at_new"}
	data, _ := json.Marshal(f)
	var decoded Frame
	json.Unmarshal(data, &decoded)
	if decoded.AccessToken != "at_new" {
		t.Errorf("access_token: got %q, want %q", decoded.AccessToken, "at_new")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd agency && go test ./internal/relay/ -v -run TestFrame`
Expected: FAIL — types not defined

- [ ] **Step 3: Write implementation**

```go
// internal/relay/protocol.go
package relay

import "encoding/json"

// Frame type constants for the tunnel protocol.
const (
	FrameHandshake    = "handshake"
	FrameTokenRefresh = "token_refresh"
	FrameRequest      = "request"
	FrameResponse     = "response"
	FrameWSOpen       = "ws_open"
	FrameWSFrame      = "ws_frame"
	FrameWSClose      = "ws_close"
)

// Frame is the envelope for all tunnel protocol messages.
// Fields are populated based on the frame type.
type Frame struct {
	Type string `json:"type"`

	// Handshake fields
	Version       int    `json:"version,omitempty"`
	RelayToken    string `json:"relay_token,omitempty"`
	AccessToken   string `json:"access_token,omitempty"`
	AgencyVersion string `json:"agency_version,omitempty"`
	AgencyBuild   string `json:"agency_build,omitempty"`

	// Request/response fields
	ID      string            `json:"id,omitempty"`
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Status  int               `json:"status,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`

	// WebSocket fields
	Data json.RawMessage `json:"data,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd agency && go test ./internal/relay/ -v -run TestFrame`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/relay/protocol.go internal/relay/protocol_test.go
git commit -m "feat(relay): tunnel protocol frame types"
```

---

### Task 4: Token Refresh Endpoint + Scoped Middleware

**Files:**
- Create: `internal/api/handlers_relay.go`
- Create: `internal/api/handlers_relay_test.go`
- Modify: `internal/api/middleware_auth.go`
- Modify: `internal/api/middleware_auth_test.go`
- Modify: `internal/api/routes.go`

The refresh secret authenticates only to `POST /api/v1/relay/token` — same scoped-token pattern as the egress token.

- [ ] **Step 1: Write failing middleware tests**

Add to the existing `tests` slice in `internal/api/middleware_auth_test.go`. Add a `relaySecret` field to the test struct and pass it as the third arg to `BearerAuth`:

```go
// Add field to test struct:
relaySecret string

// Add test cases:
{
	name:           "relay refresh secret on token endpoint",
	configToken:    testToken,
	egressToken:    testEgressToken,
	relaySecret:    "rs_test_secret",
	path:           "/api/v1/relay/token",
	method:         http.MethodPost,
	authHeader:     "Bearer rs_test_secret",
	wantStatusCode: http.StatusOK,
},
{
	name:           "relay refresh secret on wrong endpoint",
	configToken:    testToken,
	egressToken:    testEgressToken,
	relaySecret:    "rs_test_secret",
	path:           "/api/v1/agents",
	method:         http.MethodGet,
	authHeader:     "Bearer rs_test_secret",
	wantStatusCode: http.StatusForbidden,
},

// Update middleware creation in test loop:
middleware := BearerAuth(tc.configToken, tc.egressToken, tc.relaySecret)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd agency && go test ./internal/api/ -v -run TestBearerAuth`
Expected: FAIL — `BearerAuth` doesn't accept 3 args

- [ ] **Step 3: Update BearerAuth middleware**

In `internal/api/middleware_auth.go`, add `relaySecret` parameter:

```go
func BearerAuth(token, egressToken, relaySecret string) func(http.Handler) http.Handler {
```

Add after the egress token block, before the final 401:

```go
			// Scoped relay refresh secret: only token refresh endpoint.
			if relaySecret != "" && constantTimeEqual(relaySecret, incoming) {
				if r.URL.Path == "/api/v1/relay/token" && r.Method == http.MethodPost {
					next.ServeHTTP(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "token scope insufficient"})
				return
			}
```

- [ ] **Step 4: Update all BearerAuth call sites**

In `cmd/gateway/main.go`, where `api.BearerAuth(cfg.Token, cfg.EgressToken)` is called, change to:

```go
relayCfg, _ := relay.LoadRelayConfig(cfg.Home)
relaySecret := ""
if relayCfg != nil {
	relaySecret = relayCfg.RefreshSecret
}
r.Use(api.BearerAuth(cfg.Token, cfg.EgressToken, relaySecret))
```

Add import: `"github.com/geoffbelknap/agency/internal/relay"`

- [ ] **Step 5: Run middleware tests**

Run: `cd agency && go test ./internal/api/ -v -run TestBearerAuth`
Expected: PASS

- [ ] **Step 6: Write token refresh handler + test**

```go
// internal/api/handlers_relay_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/relay"
)

func newTestRelayHandler(t *testing.T) (*handler, string) {
	t.Helper()
	dir := t.TempDir()
	hmacKey := make([]byte, 32)
	for i := range hmacKey {
		hmacKey[i] = byte(i)
	}
	cfg := &config.Config{
		Home:    dir,
		HMACKey: hmacKey,
		Version: "0.8.2",
		BuildID: "abc1234",
	}
	h := &handler{cfg: cfg}
	return h, dir
}

func TestRelayTokenHandler(t *testing.T) {
	h, dir := newTestRelayHandler(t)

	relayCfg := &relay.RelayConfig{RefreshSecret: "rs_test", Enabled: true}
	relayCfg.Save(dir)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/relay/token", nil)
	w := httptest.NewRecorder()
	h.relayToken(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200. Body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expires_in: got %d, want 900", resp.ExpiresIn)
	}

	claims, err := relay.ValidateAccessToken(resp.AccessToken, h.cfg.HMACKey)
	if err != nil {
		t.Fatalf("validate returned token: %v", err)
	}
	if claims.Subject != "relay" {
		t.Errorf("subject: got %q, want relay", claims.Subject)
	}
}
```

```go
// internal/api/handlers_relay.go
package api

import (
	"net/http"
	"time"

	"github.com/geoffbelknap/agency/internal/relay"
)

const accessTokenTTL = 15 * time.Minute

// relayToken generates a short-lived access token for the tunnel.
// Authenticated by the refresh secret (scoped in middleware).
func (h *handler) relayToken(w http.ResponseWriter, r *http.Request) {
	if len(h.cfg.HMACKey) == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "HMAC key not configured"})
		return
	}

	token, err := relay.GenerateAccessToken(h.cfg.HMACKey, accessTokenTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token": token,
		"expires_in":   int(accessTokenTTL.Seconds()),
	})
}
```

- [ ] **Step 7: Register the route**

In `internal/api/routes.go`, add inside the `r.Route("/api/v1", ...)` block:

```go
// Relay
r.Route("/relay", func(r chi.Router) {
	r.Post("/token", h.relayToken)
})
```

- [ ] **Step 8: Run all tests**

Run: `cd agency && go test ./internal/api/ -v -run TestRelay`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
cd agency && git add internal/api/handlers_relay.go internal/api/handlers_relay_test.go internal/api/middleware_auth.go internal/api/middleware_auth_test.go internal/api/routes.go cmd/gateway/main.go
git commit -m "feat(relay): token refresh endpoint with scoped auth"
```

---

### Task 5: Gateway Relay Endpoints

**Files:**
- Modify: `internal/api/handlers_relay.go`
- Modify: `internal/api/handlers_relay_test.go`
- Modify: `internal/api/routes.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/api/handlers_relay_test.go`:

```go
func TestRelayStatus_NotConfigured(t *testing.T) {
	h, _ := newTestRelayHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/status", nil)
	w := httptest.NewRecorder()
	h.relayStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["enabled"] != false {
		t.Error("expected enabled=false when not configured")
	}
}

func TestRelayConnect_GeneratesCredentials(t *testing.T) {
	h, dir := newTestRelayHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/relay/connect", nil)
	w := httptest.NewRecorder()
	h.relayConnect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200. Body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["refresh_secret"] == nil || resp["refresh_secret"] == "" {
		t.Error("expected refresh_secret in response")
	}
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Error("expected access_token in response")
	}

	cfg, _ := relay.LoadRelayConfig(dir)
	if cfg.RefreshSecret == "" {
		t.Error("expected refresh secret saved to relay.yaml")
	}
	if !cfg.Enabled {
		t.Error("expected enabled=true after connect")
	}
}

func TestRelayRevoke_ClearsSecret(t *testing.T) {
	h, dir := newTestRelayHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/relay/connect", nil)
	w := httptest.NewRecorder()
	h.relayConnect(w, req)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/relay/revoke", nil)
	w = httptest.NewRecorder()
	h.relayRevoke(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	cfg, _ := relay.LoadRelayConfig(dir)
	if cfg.RefreshSecret != "" {
		t.Error("expected refresh secret cleared after revoke")
	}
}

func TestRelayDestroy_ClearsAll(t *testing.T) {
	h, dir := newTestRelayHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/relay/connect", nil)
	w := httptest.NewRecorder()
	h.relayConnect(w, req)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/relay/destroy", nil)
	w = httptest.NewRecorder()
	h.relayDestroy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	cfg, _ := relay.LoadRelayConfig(dir)
	if cfg.RelayToken != "" || cfg.RefreshSecret != "" || cfg.Enabled {
		t.Error("expected all fields cleared after destroy")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd agency && go test ./internal/api/ -v -run "TestRelay"`
Expected: FAIL — methods not defined

- [ ] **Step 3: Write handlers**

Add to `internal/api/handlers_relay.go`:

```go
func (h *handler) relayConnect(w http.ResponseWriter, r *http.Request) {
	if len(h.cfg.HMACKey) == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "HMAC key not configured"})
		return
	}

	relayCfg, _ := relay.LoadRelayConfig(h.cfg.Home)
	relayCfg.RefreshSecret = relay.GenerateRefreshSecret()
	relayCfg.Enabled = true

	accessToken, err := relay.GenerateAccessToken(h.cfg.HMACKey, accessTokenTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
		return
	}

	if err := relayCfg.Save(h.cfg.Home); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save relay config"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"refresh_secret": relayCfg.RefreshSecret,
		"access_token":   accessToken,
		"expires_in":     int(accessTokenTTL.Seconds()),
	})
}

func (h *handler) relayDisconnect(w http.ResponseWriter, r *http.Request) {
	relayCfg, _ := relay.LoadRelayConfig(h.cfg.Home)
	relayCfg.Enabled = false
	relayCfg.Save(h.cfg.Home)
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (h *handler) relayStatus(w http.ResponseWriter, r *http.Request) {
	relayCfg, _ := relay.LoadRelayConfig(h.cfg.Home)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":            relayCfg.Enabled,
		"relay_url":          relayCfg.RelayURL,
		"account_id":         relayCfg.AccountID,
		"trust_acknowledged": relayCfg.TrustAcknowledged,
		"has_refresh_secret": relayCfg.RefreshSecret != "",
		"has_relay_token":    relayCfg.RelayToken != "",
	})
}

func (h *handler) relayRevoke(w http.ResponseWriter, r *http.Request) {
	relayCfg, _ := relay.LoadRelayConfig(h.cfg.Home)
	relayCfg.RefreshSecret = ""
	relayCfg.Enabled = false
	relayCfg.Save(h.cfg.Home)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (h *handler) relayReissue(w http.ResponseWriter, r *http.Request) {
	if len(h.cfg.HMACKey) == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "HMAC key not configured"})
		return
	}

	relayCfg, _ := relay.LoadRelayConfig(h.cfg.Home)
	relayCfg.RefreshSecret = relay.GenerateRefreshSecret()
	relayCfg.Enabled = true

	accessToken, err := relay.GenerateAccessToken(h.cfg.HMACKey, accessTokenTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
		return
	}
	relayCfg.Save(h.cfg.Home)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"refresh_secret": relayCfg.RefreshSecret,
		"access_token":   accessToken,
		"expires_in":     int(accessTokenTTL.Seconds()),
	})
}

func (h *handler) relayDestroy(w http.ResponseWriter, r *http.Request) {
	relayCfg, _ := relay.LoadRelayConfig(h.cfg.Home)
	relayCfg.Clear()
	relayCfg.Save(h.cfg.Home)
	writeJSON(w, http.StatusOK, map[string]string{"status": "destroyed"})
}

// buildAgencyConfig constructs the /__agency/config response.
// Adds relay fields when relay is enabled.
func buildAgencyConfig(cfg *config.Config) map[string]interface{} {
	resp := map[string]interface{}{
		"token":   cfg.Token,
		"gateway": "",
	}

	relayCfg, _ := relay.LoadRelayConfig(cfg.Home)
	if relayCfg != nil && relayCfg.Enabled {
		resp["via"] = "relay"
		if relayCfg.RelayURL != "" {
			resp["relay_url"] = relayCfg.RelayURL
		}
	}

	return resp
}
```

Add `config` import to `handlers_relay.go`:

```go
"github.com/geoffbelknap/agency/internal/config"
```

- [ ] **Step 4: Register routes and update `/__agency/config`**

In `internal/api/routes.go`, expand the relay route group:

```go
// Relay
r.Route("/relay", func(r chi.Router) {
	r.Post("/connect", h.relayConnect)
	r.Post("/disconnect", h.relayDisconnect)
	r.Get("/status", h.relayStatus)
	r.Post("/token", h.relayToken)
	r.Post("/revoke", h.relayRevoke)
	r.Post("/reissue", h.relayReissue)
	r.Post("/destroy", h.relayDestroy)
})
```

Replace the inline `/__agency/config` handler:

```go
r.Get("/__agency/config", func(w http.ResponseWriter, r *http.Request) {
	resp := buildAgencyConfig(cfg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
})
```

- [ ] **Step 5: Run all relay tests**

Run: `cd agency && go test ./internal/api/ -v -run "TestRelay"`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/handlers_relay.go internal/api/handlers_relay_test.go internal/api/routes.go
git commit -m "feat(relay): gateway endpoints and /__agency/config relay fields"
```

---

### Task 6: Relay Proxy (Request/Response Forwarding)

**Files:**
- Create: `internal/relay/proxy.go`
- Create: `internal/relay/proxy_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/relay/proxy_test.go
package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxy_ForwardsGETRequest(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}
		if r.Header.Get("X-Agency-Via") != "relay" {
			t.Error("expected X-Agency-Via: relay header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode([]map[string]string{{"name": "analyst"}})
	}))
	defer gateway.Close()

	p := NewProxy(gateway.URL, "access_tok_123")
	reqFrame := Frame{
		Type:    FrameRequest,
		ID:      "req_001",
		Method:  "GET",
		Path:    "/api/v1/agents",
		Headers: map[string]string{"Accept": "application/json"},
	}

	respFrame, err := p.HandleRequest(reqFrame)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if respFrame.Type != FrameResponse {
		t.Errorf("type: got %q, want %q", respFrame.Type, FrameResponse)
	}
	if respFrame.ID != "req_001" {
		t.Errorf("id: got %q, want %q", respFrame.ID, "req_001")
	}
	if respFrame.Status != 200 {
		t.Errorf("status: got %d, want 200", respFrame.Status)
	}
}

func TestProxy_ForwardsPOSTWithBody(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "scout" {
			t.Errorf("body name: got %q, want scout", body["name"])
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})
	}))
	defer gateway.Close()

	p := NewProxy(gateway.URL, "tok")
	reqFrame := Frame{
		Type:   FrameRequest,
		ID:     "req_002",
		Method: "POST",
		Path:   "/api/v1/agents",
		Body:   json.RawMessage(`{"name":"scout"}`),
	}

	respFrame, err := p.HandleRequest(reqFrame)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if respFrame.Status != 201 {
		t.Errorf("status: got %d, want 201", respFrame.Status)
	}
}

func TestProxy_UpdatesAccessToken(t *testing.T) {
	p := NewProxy("http://localhost:8200", "old_token")
	p.UpdateAccessToken("new_token")
	if p.accessToken != "new_token" {
		t.Error("access token not updated")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd agency && go test ./internal/relay/ -v -run TestProxy`
Expected: FAIL — Proxy type not defined

- [ ] **Step 3: Write implementation**

```go
// internal/relay/proxy.go
package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Proxy forwards request frames to the local gateway and returns response frames.
type Proxy struct {
	gatewayURL  string
	accessToken string
	mu          sync.RWMutex
	client      *http.Client
}

// NewProxy creates a proxy targeting the given gateway URL.
func NewProxy(gatewayURL, accessToken string) *Proxy {
	return &Proxy{
		gatewayURL:  gatewayURL,
		accessToken: accessToken,
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

// UpdateAccessToken swaps the current access token (called on token_refresh).
func (p *Proxy) UpdateAccessToken(token string) {
	p.mu.Lock()
	p.accessToken = token
	p.mu.Unlock()
}

// HandleRequest proxies a request frame to the local gateway and returns the response frame.
func (p *Proxy) HandleRequest(req Frame) (Frame, error) {
	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequest(req.Method, p.gatewayURL+req.Path, bodyReader)
	if err != nil {
		return Frame{Type: FrameResponse, ID: req.ID, Status: 502}, fmt.Errorf("build request: %w", err)
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	p.mu.RLock()
	httpReq.Header.Set("Authorization", "Bearer "+p.accessToken)
	p.mu.RUnlock()
	httpReq.Header.Set("X-Agency-Via", "relay")

	if len(req.Body) > 0 && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Frame{Type: FrameResponse, ID: req.ID, Status: 502}, fmt.Errorf("gateway request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Frame{Type: FrameResponse, ID: req.ID, Status: 502}, fmt.Errorf("read response: %w", err)
	}

	respHeaders := make(map[string]string)
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	return Frame{
		Type:    FrameResponse,
		ID:      req.ID,
		Status:  resp.StatusCode,
		Headers: respHeaders,
		Body:    json.RawMessage(respBody),
	}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd agency && go test ./internal/relay/ -v -run TestProxy`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/relay/proxy.go internal/relay/proxy_test.go
git commit -m "feat(relay): request/response proxy for tunnel frames"
```

---

### Task 7: Tunnel Client

**Files:**
- Create: `internal/relay/client.go`
- Create: `internal/relay/client_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/relay/client_test.go
package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var testUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func TestClient_Handshake(t *testing.T) {
	var handshakeReceived Frame
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		mu.Lock()
		json.Unmarshal(msg, &handshakeReceived)
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := ClientConfig{
		RelayURL:      wsURL,
		RelayToken:    "rt_test",
		AccessToken:   "at_test",
		AgencyVersion: "0.8.2",
		AgencyBuild:   "abc1234",
		GatewayURL:    "http://localhost:8200",
	}

	client := NewClient(cfg, nil)
	go client.Run()
	time.Sleep(200 * time.Millisecond)
	client.Stop()

	mu.Lock()
	defer mu.Unlock()
	if handshakeReceived.Type != FrameHandshake {
		t.Errorf("type: got %q, want %q", handshakeReceived.Type, FrameHandshake)
	}
	if handshakeReceived.RelayToken != "rt_test" {
		t.Errorf("relay_token: got %q, want rt_test", handshakeReceived.RelayToken)
	}
	if handshakeReceived.Version != 1 {
		t.Errorf("version: got %d, want 1", handshakeReceived.Version)
	}
}

func TestClient_ProxiesRequest(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer gateway.Close()

	var responseFrame Frame
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		conn.ReadMessage() // handshake

		reqFrame := Frame{Type: FrameRequest, ID: "req_test", Method: "GET", Path: "/api/v1/health"}
		data, _ := json.Marshal(reqFrame)
		conn.WriteMessage(websocket.TextMessage, data)

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		mu.Lock()
		json.Unmarshal(msg, &responseFrame)
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := ClientConfig{
		RelayURL:      wsURL,
		RelayToken:    "rt_test",
		AccessToken:   "at_test",
		GatewayURL:    gateway.URL,
	}

	client := NewClient(cfg, nil)
	go client.Run()
	time.Sleep(500 * time.Millisecond)
	client.Stop()

	mu.Lock()
	defer mu.Unlock()
	if responseFrame.Type != FrameResponse {
		t.Errorf("type: got %q, want response", responseFrame.Type)
	}
	if responseFrame.ID != "req_test" {
		t.Errorf("id: got %q, want req_test", responseFrame.ID)
	}
	if responseFrame.Status != 200 {
		t.Errorf("status: got %d, want 200", responseFrame.Status)
	}
}

func TestClient_ReconnectsOnDisconnect(t *testing.T) {
	connectCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		connectCount++
		mu.Unlock()
		conn.ReadMessage()
		conn.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := ClientConfig{
		RelayURL:   wsURL,
		RelayToken: "rt_test",
		AccessToken: "at_test",
		GatewayURL: "http://localhost:8200",
	}

	client := NewClient(cfg, nil)
	client.reconnectMin = 50 * time.Millisecond
	client.reconnectMax = 200 * time.Millisecond
	go client.Run()
	time.Sleep(600 * time.Millisecond)
	client.Stop()

	mu.Lock()
	defer mu.Unlock()
	if connectCount < 2 {
		t.Errorf("expected at least 2 connect attempts, got %d", connectCount)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd agency && go test ./internal/relay/ -v -run TestClient -timeout 10s`
Expected: FAIL — Client type not defined

- [ ] **Step 3: Write implementation**

```go
// internal/relay/client.go
package relay

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
)

// ClientConfig holds tunnel connection parameters.
type ClientConfig struct {
	RelayURL      string
	RelayToken    string
	AccessToken   string
	AgencyVersion string
	AgencyBuild   string
	GatewayURL    string
}

// Client manages the tunnel WebSocket to the relay service.
type Client struct {
	cfg          ClientConfig
	proxy        *Proxy
	log          *log.Logger
	stop         chan struct{}
	done         chan struct{}
	reconnectMin time.Duration
	reconnectMax time.Duration
	mu           sync.Mutex
}

// NewClient creates a tunnel client.
func NewClient(cfg ClientConfig, logger *log.Logger) *Client {
	return &Client{
		cfg:          cfg,
		proxy:        NewProxy(cfg.GatewayURL, cfg.AccessToken),
		log:          logger,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
		reconnectMin: 1 * time.Second,
		reconnectMax: 30 * time.Second,
	}
}

// Run connects to the relay and processes frames. Reconnects on disconnect.
// Blocks until Stop is called.
func (c *Client) Run() {
	defer close(c.done)
	backoff := c.reconnectMin

	for {
		err := c.connectOnce()

		select {
		case <-c.stop:
			return
		default:
		}

		if err != nil && c.log != nil {
			c.log.Warn("relay tunnel disconnected", "err", err, "reconnect_in", backoff)
		}

		select {
		case <-c.stop:
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > c.reconnectMax {
			backoff = c.reconnectMax
		}
	}
}

// Stop gracefully shuts down the tunnel client.
func (c *Client) Stop() {
	close(c.stop)
	<-c.done
}

func (c *Client) connectOnce() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.cfg.RelayURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send handshake
	handshake := Frame{
		Type:          FrameHandshake,
		Version:       1,
		RelayToken:    c.cfg.RelayToken,
		AccessToken:   c.cfg.AccessToken,
		AgencyVersion: c.cfg.AgencyVersion,
		AgencyBuild:   c.cfg.AgencyBuild,
	}
	data, _ := json.Marshal(handshake)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return err
	}

	for {
		select {
		case <-c.stop:
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var frame Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			continue
		}

		if frame.Type == FrameRequest {
			go c.handleRequest(conn, frame)
		}
	}
}

func (c *Client) handleRequest(conn *websocket.Conn, req Frame) {
	resp, err := c.proxy.HandleRequest(req)
	if err != nil {
		resp = Frame{
			Type:   FrameResponse,
			ID:     req.ID,
			Status: 502,
			Body:   json.RawMessage(`{"error":"gateway unreachable"}`),
		}
	}

	data, _ := json.Marshal(resp)
	c.mu.Lock()
	conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()
}

// RefreshAccessToken updates the proxy's token and sends token_refresh to DO.
func (c *Client) RefreshAccessToken(conn *websocket.Conn, newToken string) {
	c.proxy.UpdateAccessToken(newToken)
	c.cfg.AccessToken = newToken

	frame := Frame{Type: FrameTokenRefresh, AccessToken: newToken}
	data, _ := json.Marshal(frame)
	c.mu.Lock()
	conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()
}
```

- [ ] **Step 4: Run tests**

Run: `cd agency && go test ./internal/relay/ -v -run TestClient -timeout 10s`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/relay/client.go internal/relay/client_test.go
git commit -m "feat(relay): tunnel client with handshake, proxying, reconnection"
```

---

### Task 8: Operator Network + Relay Container

**Files:**
- Modify: `internal/orchestrate/containers/networks.go`
- Modify: `internal/orchestrate/containers/networks_test.go`
- Modify: `internal/orchestrate/infra.go`
- Create: `images/relay/Dockerfile`

This task creates the `agency-operator` network, adds the relay container, and migrates agency-web to the operator network.

- [ ] **Step 1: Add operator network factory + test**

Add to `internal/orchestrate/containers/networks.go`:

```go
// CreateOperatorNetwork creates a non-internal bridge network for operator-facing
// tools (agency-web, relay). Allows outbound access (relay needs to reach Cloudflare).
// Isolated from the mediation network — operator tools only need the gateway.
func CreateOperatorNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: false,
		Labels:   mergeLabels(labels),
	})
	return err
}
```

Add test to `internal/orchestrate/containers/networks_test.go`:

```go
func TestCreateOperatorNetwork_NotInternal(t *testing.T) {
	mock := &mockNetworkAPI{}
	err := CreateOperatorNetwork(context.Background(), mock, "agency-operator", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastOpts.Internal {
		t.Error("operator network should NOT be internal (relay needs outbound)")
	}
}

func TestCreateOperatorNetwork_MergesAgencyManagedLabel(t *testing.T) {
	mock := &mockNetworkAPI{}
	CreateOperatorNetwork(context.Background(), mock, "agency-operator", map[string]string{"custom": "label"})
	if mock.lastOpts.Labels["agency.managed"] != "true" {
		t.Error("expected agency.managed label")
	}
	if mock.lastOpts.Labels["custom"] != "label" {
		t.Error("expected custom label preserved")
	}
}
```

- [ ] **Step 2: Run network tests**

Run: `cd agency && go test ./internal/orchestrate/containers/ -v -run TestCreateOperator`
Expected: PASS

- [ ] **Step 3: Add operator network constant and creation to infra.go**

In `internal/orchestrate/infra.go`, add constant at line ~35:

```go
operatorNet = "agency-operator"
```

In `ensureNetworks()`, add to the `nets` slice:

```go
{operatorNet, false},  // agency-operator (operator tools, outbound allowed)
```

Use the operator network factory:

```go
for _, n := range nets {
	_, inspectErr := inf.cli.NetworkInspect(ctx, n.name, network.InspectOptions{})
	if inspectErr != nil {
		var err error
		switch {
		case n.internal:
			err = containers.CreateInternalNetwork(ctx, inf.cli, n.name, nil)
		case n.name == operatorNet:
			err = containers.CreateOperatorNetwork(ctx, inf.cli, n.name, nil)
		default:
			err = containers.CreateEgressNetwork(ctx, inf.cli, n.name, nil)
		}
		if err != nil {
			return fmt.Errorf("create network %s: %w", n.name, err)
		}
		inf.log.Debug("created network", "name", n.name, "internal", n.internal)
	}
}
```

- [ ] **Step 4: Migrate agency-web to operator network**

In `ensureWeb()` (~line 889), change:

```go
// Before:
hc.NetworkMode = "bridge"
hc.ExtraHosts = []string{"gateway:host-gateway"}

// After:
hc.NetworkMode = container.NetworkMode(operatorNet)
hc.ExtraHosts = []string{"gateway:host-gateway"}
```

The `ExtraHosts` stays — agency-web still needs `gateway:host-gateway` to reach the full gateway API. The change is moving from the default Docker bridge to the dedicated operator network.

- [ ] **Step 5: Add relay container image**

Create `images/relay/Dockerfile`:

```dockerfile
FROM golang:1.24-alpine AS builder
ARG BUILD_ID=dev
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${BUILD_ID}" -o /relay ./cmd/gateway/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /relay /usr/local/bin/agency
ENTRYPOINT ["agency", "relay", "connect", "--foreground"]
```

Add image to `defaultImages` map in `infra.go`:

```go
"relay": "agency-relay:latest",
```

Add health check:

```go
"relay": {
	Test:        []string{"CMD-SHELL", "test -f /tmp/relay-healthy"},
	Interval:    10 * time.Second,
	Timeout:     3 * time.Second,
	StartPeriod: 5 * time.Second,
	Retries:     3,
},
```

- [ ] **Step 6: Add ensureRelay() function**

Add to `infra.go`:

```go
func (inf *Infra) ensureRelay(ctx context.Context) error {
	// Only start if relay is configured and enabled
	relayCfg, err := relay.LoadRelayConfig(inf.home())
	if err != nil || !relayCfg.Enabled || relayCfg.RelayToken == "" {
		return nil // relay not configured, skip silently
	}

	if err := images.Resolve(ctx, inf.cli, "relay", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		inf.log.Warn("relay image not available, skipping", "err", err)
		return nil // non-fatal
	}

	name := containerName("relay")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
		return nil
	}

	containers.StopAndRemove(ctx, inf.cli, name, inf.log)

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.NetworkMode = container.NetworkMode(operatorNet)
	hc.ExtraHosts = []string{"gateway:host-gateway"}
	hc.ReadonlyRootfs = true
	hc.Binds = []string{
		filepath.Join(inf.home(), "relay.yaml") + ":/etc/agency/relay.yaml:ro",
	}
	hc.Resources.Memory = 32 * 1024 * 1024  // 32MB
	hc.Resources.NanoCPUs = 250_000_000      // 0.25 CPU
	pidsLimit := int64(64)
	hc.Resources.PidsLimit = &pidsLimit

	_, err = containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:    defaultImages["relay"],
			Hostname: "relay",
			Env: []string{
				"AGENCY_RELAY_CONFIG=/etc/agency/relay.yaml",
				"AGENCY_GATEWAY_URL=http://gateway:8200",
			},
			Labels: map[string]string{
				"agency.managed":       "true",
				"agency.role":          "infra",
				"agency.component":     "relay",
				"agency.build.id":      images.ImageBuildLabel(ctx, inf.cli, defaultImages["relay"]),
				"agency.build.gateway": inf.BuildID,
			},
			Healthcheck: defaultHealthChecks["relay"],
		},
		hc,
		nil,
	)
	return err
}
```

- [ ] **Step 7: Add relay to infra startup sequence**

In `EnsureRunningWithProgress()`, add relay to the components list:

```go
{"relay", "Starting relay tunnel", inf.ensureRelay},
```

Add after `{"web", ...}` — relay starts after web since both are on the operator network.

- [ ] **Step 8: Build to verify compilation**

Run: `cd agency && go build ./cmd/gateway/`
Expected: Compiles without errors

- [ ] **Step 9: Run network tests**

Run: `cd agency && go test ./internal/orchestrate/containers/ -v`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
cd agency && git add internal/orchestrate/containers/networks.go internal/orchestrate/containers/networks_test.go internal/orchestrate/infra.go images/relay/Dockerfile
git commit -m "feat(relay): operator network, relay container, migrate agency-web"
```

---

### Task 9: API Client Methods

**Files:**
- Modify: `internal/apiclient/client.go`

- [ ] **Step 1: Add methods**

Add to `internal/apiclient/client.go`:

```go
// ── Relay ──────────────────────────────────────────────────────────────────

func (c *Client) RelayConnect() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/relay/connect", nil, &result)
	return result, err
}

func (c *Client) RelayDisconnect() error {
	_, err := c.Post("/api/v1/relay/disconnect", nil)
	return err
}

func (c *Client) RelayStatus() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/relay/status", &result)
	return result, err
}

func (c *Client) RelayRevoke() error {
	_, err := c.Post("/api/v1/relay/revoke", nil)
	return err
}

func (c *Client) RelayReissue() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/relay/reissue", nil, &result)
	return result, err
}

func (c *Client) RelayDestroy() error {
	_, err := c.Post("/api/v1/relay/destroy", nil)
	return err
}
```

- [ ] **Step 2: Build to verify compilation**

Run: `cd agency && go build ./cmd/gateway/`
Expected: Compiles

- [ ] **Step 3: Commit**

```bash
cd agency && git add internal/apiclient/client.go
git commit -m "feat(relay): API client methods"
```

---

### Task 10: CLI Commands

**Files:**
- Modify: `internal/cli/commands.go`

- [ ] **Step 1: Add relayCmd() and subcommands**

```go
func relayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relay",
		Short: "Manage relay tunnel to tinyfleck.io",
	}
	cmd.AddCommand(
		relayConnectCmd(),
		relayDisconnectCmd(),
		relayStatusCmd(),
		relayRevokeCmd(),
		relayReissueCmd(),
		relayDestroyCmd(),
	)
	return cmd
}

func relayConnectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "connect",
		Short: "Start relay tunnel (device auth on first run)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			_, err = c.RelayConnect()
			if err != nil {
				return err
			}
			fmt.Printf("%s Relay credentials generated\n", green.Render("✓"))
			fmt.Printf("  Refresh secret stored in ~/.agency/relay.yaml\n")
			fmt.Printf("  Run %s to bring up the relay container\n", bold.Render("agency infra up"))
			return nil
		},
	}
}

func relayDisconnectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect",
		Short: "Stop relay tunnel (keep credentials)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.RelayDisconnect(); err != nil {
				return err
			}
			fmt.Printf("%s Relay disconnected\n", green.Render("✓"))
			return nil
		},
	}
}

func relayStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show relay connection state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			status, err := c.RelayStatus()
			if err != nil {
				return err
			}
			enabled, _ := status["enabled"].(bool)
			if enabled {
				fmt.Printf("%s Relay enabled\n", green.Render("●"))
			} else {
				fmt.Printf("%s Relay disabled\n", dim.Render("○"))
			}
			if url, ok := status["relay_url"].(string); ok && url != "" {
				fmt.Printf("  URL: %s\n", url)
			}
			if status["has_refresh_secret"] == true {
				fmt.Printf("  Credentials: %s\n", green.Render("configured"))
			} else {
				fmt.Printf("  Credentials: %s\n", dim.Render("not configured"))
			}
			return nil
		},
	}
}

func relayRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "Delete refresh secret (access expires in ~15 min)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.RelayRevoke(); err != nil {
				return err
			}
			fmt.Printf("%s Refresh secret revoked. Access token expires within 15 minutes.\n", green.Render("✓"))
			return nil
		},
	}
}

func relayReissueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reissue",
		Short: "Generate new refresh secret and access token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.RelayReissue(); err != nil {
				return err
			}
			fmt.Printf("%s New credentials issued\n", green.Render("✓"))
			return nil
		},
	}
}

func relayDestroyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Revoke credentials and remove all relay config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.RelayDestroy(); err != nil {
				return err
			}
			fmt.Printf("%s Relay config destroyed\n", green.Render("✓"))
			return nil
		},
	}
}
```

- [ ] **Step 2: Register in RegisterCommands**

In the `// ── Grouped subcommands` section, add `relayCmd()`:

```go
for _, cmd := range []*cobra.Command{
	channelCmd(), infraCmd(), hubCmd(), teamCmd(), capCmd(),
	intakeCmd(), knowledgeCmd(), policyCmd(), adminCmd(),
	contextCmd(), missionCmd(), eventCmd(), webhookCmd(), meeseeksCmd(), notificationsCmd(), auditCmd(),
	credentialCmd(),
	relayCmd(),
} {
```

- [ ] **Step 3: Build and verify**

Run: `cd agency && go build ./cmd/gateway/`
Expected: Compiles

- [ ] **Step 4: Commit**

```bash
cd agency && git add internal/cli/commands.go
git commit -m "feat(relay): CLI commands — connect, disconnect, status, revoke, reissue, destroy"
```

---

### Task 11: Full Integration Build + Test

**Files:** None new — verification only.

- [ ] **Step 1: Run full relay test suite**

Run: `cd agency && go test ./internal/relay/ -v -timeout 30s`
Expected: All relay package tests pass

- [ ] **Step 2: Run API handler tests**

Run: `cd agency && go test ./internal/api/ -v -run "TestRelay|TestBearerAuth" -timeout 30s`
Expected: All relay handler and middleware tests pass

- [ ] **Step 3: Run network tests**

Run: `cd agency && go test ./internal/orchestrate/containers/ -v -timeout 30s`
Expected: All network tests pass (including new operator network tests)

- [ ] **Step 4: Build full binary**

Run: `cd agency && go build ./cmd/gateway/`
Expected: Clean compilation

- [ ] **Step 5: Run full test suite (check for regressions)**

Run: `cd agency && go test ./... -timeout 120s 2>&1 | tail -40`
Expected: No new failures

- [ ] **Step 6: Commit (if any fixes were needed)**

```bash
cd agency && git add -A && git commit -m "fix(relay): integration fixes from full test suite"
```

---

## Post-Implementation Notes

**What this plan builds:** The complete gateway-side relay: config, HMAC tokens, tunnel protocol, proxy, tunnel client with reconnection, gateway API endpoints, CLI commands, operator Docker network, relay container, and agency-web network migration.

**What needs the Cloudflare plan (separate):** Worker routing, Durable Object, D1 schema, R2 deployment, OAuth flows, device auth, waitlist/admin UI. The Cloudflare Worker connects to the tunnel client built here.

**Operator network benefits:**
- agency-web and relay isolated from mediation containers (comms, knowledge, enforcer)
- Relay gets outbound access for Cloudflare WSS without touching the egress proxy
- Docker restart policy (`unless-stopped`) handles relay auto-restart — no launchd/systemd needed
- `agency infra up` / `agency infra down` manages relay lifecycle like every other service
