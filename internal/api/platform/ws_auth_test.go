package platform_test

// Integration test: /ws goes through BearerAuth → platform handler → hub upgrade.
// Verifies the end-to-end accept/reject behavior that
// TASK-ios-ws-auth-001 introduces.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/geoffbelknap/agency/internal/api"
	"github.com/geoffbelknap/agency/internal/api/platform"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/ws"
)

// setupWSTestServer mounts the platform routes behind BearerAuth and returns
// a live server plus the ws:// URL for /ws.
func setupWSTestServer(t *testing.T, token string) (*httptest.Server, string) {
	t.Helper()

	// NewHub starts the run loop internally — no explicit Run() call needed.
	hub := ws.NewHub(slog.Default())

	r := chi.NewRouter()
	r.Use(api.BearerAuth(token, "", nil))
	platform.RegisterRoutes(r, platform.Deps{
		WSHub:  hub,
		Config: &config.Config{Token: token},
		Logger: slog.Default(),
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	return srv, wsURL
}

func TestWSUpgrade_RejectsMissingToken(t *testing.T) {
	_, wsURL := setupWSTestServer(t, "test-token")

	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	_, resp, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without auth")
	}
	if resp == nil {
		t.Fatalf("expected HTTP response on failed handshake, got err=%v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWSUpgrade_AcceptsBearerHeader(t *testing.T) {
	const token = "test-token"
	_, wsURL := setupWSTestServer(t, token)

	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, headers)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial failed: err=%v status=%d", err, status)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", resp.StatusCode)
	}
}

func TestWSUpgrade_AcceptsBearerSubprotocol(t *testing.T) {
	const token = "test-token"
	_, wsURL := setupWSTestServer(t, token)

	dialer := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
		// Offer app protocol + bearer entry; server echoes only the app protocol.
		Subprotocols: []string{ws.AppSubprotocol, "bearer." + token},
	}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial failed: err=%v status=%d", err, status)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", resp.StatusCode)
	}
	// Server must echo ONLY the app protocol. The bearer entry must not
	// appear in the response; if it did, the token would be reflected.
	selected := resp.Header.Get("Sec-WebSocket-Protocol")
	if selected != ws.AppSubprotocol {
		t.Errorf("selected subprotocol = %q, want %q", selected, ws.AppSubprotocol)
	}
	if strings.Contains(selected, "bearer.") {
		t.Errorf("server reflected bearer entry in response: %q", selected)
	}
}

func TestWSUpgrade_RejectsWrongSubprotocolToken(t *testing.T) {
	_, wsURL := setupWSTestServer(t, "correct-token")

	dialer := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
		Subprotocols:     []string{ws.AppSubprotocol, "bearer.wrong-token"},
	}
	_, resp, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail with wrong token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Errorf("status = %d, want 401 (err=%v)", status, err)
	}
}
