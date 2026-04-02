package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerAuth(t *testing.T) {
	const testToken = "test-secret-token"

	// Simple next handler that always returns 200.
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name           string
		configToken    string
		path           string
		authHeader     string
		xAgencyToken   string
		wantStatusCode int
	}{
		{
			name:           "valid Bearer token",
			configToken:    testToken,
			path:           "/api/v1/agents",
			authHeader:     "Bearer " + testToken,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "valid X-Agency-Token",
			configToken:    testToken,
			path:           "/api/v1/agents",
			xAgencyToken:   testToken,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "missing token",
			configToken:    testToken,
			path:           "/api/v1/agents",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "wrong token",
			configToken:    testToken,
			path:           "/api/v1/agents",
			authHeader:     "Bearer wrong-token",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "health endpoint without token",
			configToken:    testToken,
			path:           "/api/v1/health",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "empty config token allows all (dev mode)",
			configToken:    "",
			path:           "/api/v1/agents",
			wantStatusCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := BearerAuth(tc.configToken)(ok)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
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
