package webhost

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerServesHealthConfigAndSPAFallback(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("token: test-token\ngateway_addr: 0.0.0.0:8200\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>agency</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app.js"), []byte("console.log('agency')"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler, err := Handler(Options{DistDir: dist, AgencyHome: home})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	assertBody(t, server.URL+"/health", "ok")
	assertBody(t, server.URL+"/setup", "<html>agency</html>")
	assertBody(t, server.URL+"/assets/app.js", "console.log('agency')")

	resp, err := http.Get(server.URL + "/__agency/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var cfg map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["token"] != "test-token" || cfg["gateway"] != "" {
		t.Fatalf("unexpected config response: %#v", cfg)
	}
}

func TestHandlerProxiesGatewayRequests(t *testing.T) {
	var apiAuth string
	var wsAuth string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			apiAuth = r.Header.Get("Authorization")
			_, _ = io.WriteString(w, "api-ok")
		case "/ws":
			wsAuth = r.Header.Get("Authorization")
			_, _ = io.WriteString(w, "ws-ok")
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("token: test-token\ngateway_addr: "+gateway.URL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>agency</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler, err := Handler(Options{DistDir: dist, AgencyHome: home})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	assertBody(t, server.URL+"/api/v1/status", "api-ok")
	assertBody(t, server.URL+"/ws", "ws-ok")
	if apiAuth != "" {
		t.Fatalf("api proxy injected Authorization = %q, want empty", apiAuth)
	}
	if wsAuth != "Bearer test-token" {
		t.Fatalf("ws proxy Authorization = %q, want bearer token", wsAuth)
	}
}

func assertBody(t *testing.T, url, want string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Fatalf("GET %s body = %q, want %q", url, string(body), want)
	}
}
