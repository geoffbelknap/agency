package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCallerMiddleware_AllowedCaller(t *testing.T) {
	allowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal": {"enforcer", "comms"},
	}
	mw := CallerValidation(allowlist)
	handler := mw(okHandler())

	req := httptest.NewRequest("POST", "/api/v1/agents/myagent/signal", nil)
	req.Header.Set("X-Agency-Caller", "enforcer")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCallerMiddleware_BlockedCaller(t *testing.T) {
	allowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal": {"enforcer", "comms"},
	}
	mw := CallerValidation(allowlist)
	handler := mw(okHandler())

	req := httptest.NewRequest("POST", "/api/v1/agents/myagent/signal", nil)
	req.Header.Set("X-Agency-Caller", "intake")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestCallerMiddleware_NoAllowlist(t *testing.T) {
	allowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal": {"enforcer"},
	}
	mw := CallerValidation(allowlist)
	handler := mw(okHandler())

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for unprotected route, got %d", rr.Code)
	}
}

func TestCallerMiddleware_MissingHeader(t *testing.T) {
	allowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal": {"enforcer"},
	}
	mw := CallerValidation(allowlist)
	handler := mw(okHandler())

	req := httptest.NewRequest("POST", "/api/v1/agents/myagent/signal", nil)
	// No X-Agency-Caller header set
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for missing header, got %d", rr.Code)
	}
}

func TestCallerMiddleware_WildcardPath(t *testing.T) {
	allowlist := map[string][]string{
		"GET /api/v1/graph/*": {"body", "enforcer"},
	}
	mw := CallerValidation(allowlist)
	handler := mw(okHandler())

	req := httptest.NewRequest("GET", "/api/v1/graph/communities/123", nil)
	req.Header.Set("X-Agency-Caller", "body")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for wildcard match, got %d", rr.Code)
	}
}

func TestCallerMiddleware_ParamPath(t *testing.T) {
	allowlist := map[string][]string{
		"GET /api/v1/agents/{name}/economics": {"gateway"},
	}
	mw := CallerValidation(allowlist)
	handler := mw(okHandler())

	req := httptest.NewRequest("GET", "/api/v1/agents/secops-alpha/economics", nil)
	req.Header.Set("X-Agency-Caller", "gateway")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for param path match, got %d", rr.Code)
	}
}
