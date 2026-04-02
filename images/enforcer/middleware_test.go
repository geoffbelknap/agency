package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthHealthBypasses(t *testing.T) {
	am := NewAuthMiddleware(nil)
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthValidScopedToken(t *testing.T) {
	am := NewAuthMiddleware(nil)
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer agency-scoped-abc123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthValidAPIKey(t *testing.T) {
	am := NewAuthMiddleware([]APIKey{{Key: "test-key-123", Name: "workspace"}})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthValidAPIKeyHeader(t *testing.T) {
	am := NewAuthMiddleware([]APIKey{{Key: "test-key-456", Name: "workspace"}})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("X-API-Key", "test-key-456")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthMissingToken(t *testing.T) {
	am := NewAuthMiddleware(nil)
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthInvalidToken(t *testing.T) {
	am := NewAuthMiddleware([]APIKey{{Key: "valid-key", Name: "workspace"}})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthProxyAuthorizationBasicScopedKey(t *testing.T) {
	am := NewAuthMiddleware(nil)
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Simulate what urllib/requests sends when proxy URL is http://agency-scoped-xxx:@enforcer:18080
	creds := base64.StdEncoding.EncodeToString([]byte("agency-scoped-abc123:"))
	req := httptest.NewRequest("GET", "http://juice-shop:3000/api/Challenges", nil)
	req.Header.Set("Proxy-Authorization", "Basic "+creds)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthProxyAuthorizationBearerScopedKey(t *testing.T) {
	am := NewAuthMiddleware(nil)
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://juice-shop:3000/api/Challenges", nil)
	req.Header.Set("Proxy-Authorization", "Bearer agency-scoped-abc123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthProxyAuthorizationAPIKey(t *testing.T) {
	am := NewAuthMiddleware([]APIKey{{Key: "test-key-789", Name: "workspace"}})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	creds := base64.StdEncoding.EncodeToString([]byte("test-key-789:"))
	req := httptest.NewRequest("GET", "http://juice-shop:3000/", nil)
	req.Header.Set("Proxy-Authorization", "Basic "+creds)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthProxyAuthorizationInvalidKey(t *testing.T) {
	am := NewAuthMiddleware([]APIKey{{Key: "valid-key", Name: "workspace"}})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	creds := base64.StdEncoding.EncodeToString([]byte("wrong-key:"))
	req := httptest.NewRequest("GET", "http://juice-shop:3000/", nil)
	req.Header.Set("Proxy-Authorization", "Basic "+creds)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthProxyAuthorizationEmptyUsername(t *testing.T) {
	am := NewAuthMiddleware(nil)
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Empty username — should be rejected
	creds := base64.StdEncoding.EncodeToString([]byte(":"))
	req := httptest.NewRequest("GET", "http://juice-shop:3000/", nil)
	req.Header.Set("Proxy-Authorization", "Basic "+creds)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty username, got %d", rr.Code)
	}
}

func TestExtractProxyTokenBearer(t *testing.T) {
	token, ok := extractProxyToken("Bearer agency-scoped-xyz")
	if !ok || token != "agency-scoped-xyz" {
		t.Errorf("expected agency-scoped-xyz/true, got %q/%v", token, ok)
	}
}

func TestExtractProxyTokenBasic(t *testing.T) {
	creds := base64.StdEncoding.EncodeToString([]byte("agency-scoped-abc:"))
	token, ok := extractProxyToken("Basic " + creds)
	if !ok || token != "agency-scoped-abc" {
		t.Errorf("expected agency-scoped-abc/true, got %q/%v", token, ok)
	}
}

func TestExtractProxyTokenBasicWithPassword(t *testing.T) {
	// Username is extracted regardless of password content
	creds := base64.StdEncoding.EncodeToString([]byte("mykey:somepassword"))
	token, ok := extractProxyToken("Basic " + creds)
	if !ok || token != "mykey" {
		t.Errorf("expected mykey/true, got %q/%v", token, ok)
	}
}

func TestExtractProxyTokenBasicEmptyUsername(t *testing.T) {
	creds := base64.StdEncoding.EncodeToString([]byte(":"))
	token, ok := extractProxyToken("Basic " + creds)
	if ok {
		t.Errorf("expected false for empty username, got token=%q ok=%v", token, ok)
	}
}

func TestExtractProxyTokenInvalidBase64(t *testing.T) {
	_, ok := extractProxyToken("Basic not-valid-base64!!!")
	if ok {
		t.Error("expected false for invalid base64")
	}
}

func TestExtractProxyTokenUnknownScheme(t *testing.T) {
	_, ok := extractProxyToken("Digest something")
	if ok {
		t.Error("expected false for unknown scheme")
	}
}

func TestAuthSetKeysReload(t *testing.T) {
	am := NewAuthMiddleware([]APIKey{{Key: "old-key", Name: "workspace"}})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Old key should work
	req := httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer old-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with old key, got %d", rr.Code)
	}

	// Reload with new keys
	am.SetKeys([]APIKey{{Key: "new-key", Name: "workspace"}})

	// Old key should fail
	req = httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer old-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with old key after reload, got %d", rr.Code)
	}

	// New key should work
	req = httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer new-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with new key, got %d", rr.Code)
	}
}
