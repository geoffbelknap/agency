package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerAuth(t *testing.T) {
	const testToken = "test-secret-token"
	const testEgressToken = "test-egress-token"

	// Simple next handler that always returns 200.
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name           string
		configToken    string
		egressToken    string
		path           string
		method         string
		authHeader     string
		xAgencyToken   string
		wantStatusCode int
	}{
		{
			name:           "valid Bearer token",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/agents",
			method:         http.MethodGet,
			authHeader:     "Bearer " + testToken,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "valid X-Agency-Token",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/agents",
			method:         http.MethodGet,
			xAgencyToken:   testToken,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "missing token",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/agents",
			method:         http.MethodGet,
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "wrong token",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/agents",
			method:         http.MethodGet,
			authHeader:     "Bearer wrong-token",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "health endpoint without token",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/health",
			method:         http.MethodGet,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "empty config token rejects requests (fail-closed)",
			configToken:    "",
			egressToken:    "",
			path:           "/api/v1/agents",
			method:         http.MethodGet,
			wantStatusCode: http.StatusUnauthorized,
		},
		// Scoped egress token tests
		{
			name:           "egress token on credential resolve endpoint",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/creds/internal/resolve",
			method:         http.MethodGet,
			authHeader:     "Bearer " + testEgressToken,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "egress token on wrong endpoint — forbidden",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/agents",
			method:         http.MethodGet,
			authHeader:     "Bearer " + testEgressToken,
			wantStatusCode: http.StatusForbidden,
		},
		{
			name:           "egress token on credential resolve with POST — forbidden",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/creds/internal/resolve",
			method:         http.MethodPost,
			authHeader:     "Bearer " + testEgressToken,
			wantStatusCode: http.StatusForbidden,
		},
		{
			name:           "full token still works on credential resolve",
			configToken:    testToken,
			egressToken:    testEgressToken,
			path:           "/api/v1/creds/internal/resolve",
			method:         http.MethodGet,
			authHeader:     "Bearer " + testToken,
			wantStatusCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := BearerAuth(tc.configToken, tc.egressToken, nil)(ok)

			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tc.path, nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			if tc.xAgencyToken != "" {
				req.Header.Set("X-Agency-Token", tc.xAgencyToken)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatusCode {
				t.Errorf("got status %d, want %d", rr.Code, tc.wantStatusCode)
			}
		})
	}
}

// TestBearerAuth_WebSocketSubprotocol verifies that a bearer token carried in
// the Sec-WebSocket-Protocol header as "bearer.<token>" is accepted on the
// /ws endpoint (the browser-friendly auth path) and rejected when absent
// or malformed.
func TestBearerAuth_WebSocketSubprotocol(t *testing.T) {
	const testToken = "test-ws-token"

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := BearerAuth(testToken, "", nil)(ok)

	tests := []struct {
		name       string
		subprotos  string
		wantStatus int
	}{
		{
			name:       "valid bearer in subprotocol is accepted",
			subprotos:  "agency.v1, bearer." + testToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "bearer first then app protocol is accepted",
			subprotos:  "bearer." + testToken + ", agency.v1",
			wantStatus: http.StatusOK,
		},
		{
			name:       "app protocol only is rejected",
			subprotos:  "agency.v1",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong bearer token is rejected",
			subprotos:  "agency.v1, bearer.wrong",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "no subprotocol header is rejected",
			subprotos:  "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed entry without bearer prefix is rejected",
			subprotos:  "agency.v1, " + testToken,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tc.subprotos != "" {
				req.Header.Set("Sec-WebSocket-Protocol", tc.subprotos)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Errorf("subprotos=%q: got status %d, want %d", tc.subprotos, rr.Code, tc.wantStatus)
			}
		})
	}
}

// TestExtractBearerSubprotocol covers the parser directly.
func TestExtractBearerSubprotocol(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"", ""},
		{"agency.v1", ""},
		{"agency.v1, bearer.abc123", "abc123"},
		{"bearer.abc123, agency.v1", "abc123"},
		{"  bearer.abc123  , agency.v1", "abc123"},
		{"bearer.", ""}, // empty token after prefix → returns ""
		{"beaRer.abc123", ""}, // case-sensitive per spec
	}
	for _, tc := range tests {
		t.Run(tc.header, func(t *testing.T) {
			got := extractBearerSubprotocol(tc.header)
			if got != tc.want {
				t.Errorf("extractBearerSubprotocol(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}
