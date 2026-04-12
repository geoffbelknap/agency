package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func testMediationProxy(t *testing.T, serviceURLs map[string]string) *MediationProxy {
	t.Helper()
	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	t.Cleanup(func() { audit.Close() })
	return NewMediationProxy(serviceURLs, nil, audit)
}

func TestMediationProxyRoutesToComms(t *testing.T) {
	comms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"path":"` + r.URL.Path + `","method":"` + r.Method + `"}`))
	}))
	defer comms.Close()

	mp := testMediationProxy(t, map[string]string{
		"comms": comms.URL,
	})

	body := strings.NewReader(`{"author":"agent","content":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/mediation/comms/channels/general/messages", body)
	w := httptest.NewRecorder()
	mp.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	respBody, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(respBody), `/channels/general/messages`) {
		t.Fatalf("expected path forwarded, got %s", string(respBody))
	}
}

func TestMediationProxyRoutesToKnowledge(t *testing.T) {
	knowledge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"service":"knowledge","path":"` + r.URL.Path + `"}`))
	}))
	defer knowledge.Close()

	mp := testMediationProxy(t, map[string]string{
		"knowledge": knowledge.URL,
	})

	req := httptest.NewRequest(http.MethodGet, "/mediation/knowledge/query?q=test", nil)
	w := httptest.NewRecorder()
	mp.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMediationProxyRejectsUnknownService(t *testing.T) {
	mp := testMediationProxy(t, map[string]string{})

	req := httptest.NewRequest(http.MethodGet, "/mediation/evil/data", nil)
	w := httptest.NewRecorder()
	mp.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestMediationProxyWebSocketFullUpgrade(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("ws upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		_, msg, _ := conn.ReadMessage()
		conn.WriteMessage(websocket.TextMessage, msg)
	}))
	defer wsServer.Close()

	mp := testMediationProxy(t, map[string]string{
		"comms": wsServer.URL,
	})
	proxyServer := httptest.NewServer(mp)
	defer proxyServer.Close()

	wsURL := strings.Replace(proxyServer.URL, "http://", "ws://", 1) + "/mediation/comms/ws?agent=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial through proxy failed: %v", err)
	}
	defer conn.Close()

	conn.WriteMessage(websocket.TextMessage, []byte("ping"))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ws read failed: %v", err)
	}
	if string(msg) != "ping" {
		t.Fatalf("expected echo 'ping', got %q", string(msg))
	}
}

func TestMediationProxyRuntimeInjectsGatewayHeaders(t *testing.T) {
	runtimeTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer gateway-token" {
			t.Fatalf("Authorization = %q, want Bearer gateway-token", got)
		}
		if got := r.Header.Get("X-Agency-Agent"); got != "coordinator" {
			t.Fatalf("X-Agency-Agent = %q, want coordinator", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer runtimeTarget.Close()

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	t.Cleanup(func() { audit.Close() })
	mp := NewMediationProxy(map[string]string{
		"runtime": runtimeTarget.URL,
	}, map[string]map[string]string{
		"runtime": {
			"Authorization":  "Bearer gateway-token",
			"X-Agency-Agent": "coordinator",
		},
	}, audit)

	req := httptest.NewRequest(http.MethodPost, "/mediation/runtime/instances/inst/runtime/nodes/drive/actions/add_viewer", strings.NewReader(`{"email":"person@example.com"}`))
	w := httptest.NewRecorder()
	mp.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
