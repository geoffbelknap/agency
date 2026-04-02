package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestProxyHandler(t *testing.T, dg *DomainGate, svc *ServiceRegistry) (*ProxyHandler, *httptest.Server) {
	t.Helper()

	// Create a fake upstream that echoes back
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Echo", "true")
		w.Header().Set("Set-Cookie", "bad=cookie")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "method=%s host=%s path=%s", r.Method, r.Host, r.URL.Path)
	}))

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	t.Cleanup(func() { audit.Close() })

	if dg == nil {
		dg = NewDomainGate()
	}

	ph := &ProxyHandler{
		egressProxy: upstream.URL,
		domainGate:  dg,
		services:    svc,
		audit:       audit,
		agentName:   "test-agent",
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	return ph, upstream
}

func TestProxyForwardedThroughEgress(t *testing.T) {
	// Create a backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "backend-response")
	}))
	defer backend.Close()

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	dg := NewDomainGate()

	ph := &ProxyHandler{
		egressProxy: backend.URL, // Use backend directly as "egress"
		domainGate:  dg,
		audit:       audit,
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Host = "example.com"
	rr := httptest.NewRecorder()
	ph.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestProxyDomainBlocked(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte("mode: allowlist\ndomains:\n  - safe.com\n"), 0644)

	dg := NewDomainGate()
	dg.LoadFromFile(f)

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	ph := &ProxyHandler{
		egressProxy: "http://localhost:1",
		domainGate:  dg,
		audit:       audit,
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	req := httptest.NewRequest("GET", "http://evil.com/hack", nil)
	req.Host = "evil.com"
	rr := httptest.NewRecorder()
	ph.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "domain blocked") {
		t.Errorf("expected domain blocked message, got: %s", body)
	}
}

func TestProxyServicePassesThrough(t *testing.T) {
	// Backend verifies X-Agency-Service is passed through (not consumed)
	// and no credential is injected by the enforcer (WI-3: egress handles credentials)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc := r.Header.Get("X-Agency-Service")
		auth := r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "service=%s auth=%s", svc, auth)
	}))
	defer backend.Close()

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	dg := NewDomainGate()

	svc := NewServiceRegistry()
	svc.Register("github", &ServiceCredential{
		Header: "Authorization",
		Value:  "enforcer-scope-only",
	})

	ph := &ProxyHandler{
		egressProxy: backend.URL,
		domainGate:  dg,
		services:    svc,
		audit:       audit,
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	req := httptest.NewRequest("GET", "http://api.github.com/repos", nil)
	req.Host = "api.github.com"
	req.Header.Set("X-Agency-Service", "github")
	rr := httptest.NewRecorder()
	ph.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	// X-Agency-Service should pass through to egress
	if !strings.Contains(body, "service=github") {
		t.Errorf("expected X-Agency-Service=github passed through, got: %s", body)
	}
}

func TestProxyDangerousHeadersStripped(t *testing.T) {
	// Backend that sends dangerous headers
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "session=abc")
		w.Header().Set("X-Safe-Header", "keep-this")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	dg := NewDomainGate()

	ph := &ProxyHandler{
		egressProxy: backend.URL,
		domainGate:  dg,
		audit:       audit,
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Host = "example.com"
	rr := httptest.NewRecorder()
	ph.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("Set-Cookie") != "" {
		t.Error("Set-Cookie header should be stripped")
	}
	if rr.Header().Get("X-Safe-Header") != "keep-this" {
		t.Error("safe headers should be preserved")
	}
}

func TestProxyConnectBlockedDomain(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(f, []byte("mode: allowlist\ndomains:\n  - safe.com\n"), 0644)

	dg := NewDomainGate()
	dg.LoadFromFile(f)

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	ph := &ProxyHandler{
		egressProxy: "http://localhost:1",
		domainGate:  dg,
		audit:       audit,
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	req := httptest.NewRequest("CONNECT", "evil.com:443", nil)
	req.Host = "evil.com:443"
	rr := httptest.NewRecorder()
	ph.HandleConnect(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestProxyUpstreamError(t *testing.T) {
	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	dg := NewDomainGate()

	// Point to a non-existent proxy to trigger upstream error
	ph := &ProxyHandler{
		egressProxy: "http://127.0.0.1:1",
		domainGate:  dg,
		audit:       audit,
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Host = "example.com"
	rr := httptest.NewRecorder()
	ph.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
}

func TestProxyPostBody(t *testing.T) {
	// Backend that echoes body
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer backend.Close()

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	dg := NewDomainGate()

	ph := &ProxyHandler{
		egressProxy: backend.URL,
		domainGate:  dg,
		audit:       audit,
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	body := strings.NewReader(`{"test":"data"}`)
	req := httptest.NewRequest("POST", "http://example.com/api", body)
	req.Host = "example.com"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ph.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestProxyInjectsAgentHeader(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent := r.Header.Get("X-Agency-Agent")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, agent)
	}))
	defer backend.Close()

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	dg := NewDomainGate()

	ph := &ProxyHandler{
		egressProxy: backend.URL,
		domainGate:  dg,
		audit:       audit,
		agentName:   "my-agent",
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Host = "example.com"
	rr := httptest.NewRecorder()
	ph.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "my-agent" {
		t.Errorf("expected X-Agency-Agent=my-agent, got %q", rr.Body.String())
	}
}

func TestProxyNoAgentHeaderWhenEmpty(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent := r.Header.Get("X-Agency-Agent")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, agent)
	}))
	defer backend.Close()

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	dg := NewDomainGate()

	ph := &ProxyHandler{
		egressProxy: backend.URL,
		domainGate:  dg,
		audit:       audit,
		transport:   http.DefaultTransport.(*http.Transport).Clone(),
	}

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Host = "example.com"
	rr := httptest.NewRecorder()
	ph.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "" {
		t.Errorf("expected no X-Agency-Agent header, got %q", rr.Body.String())
	}
}
