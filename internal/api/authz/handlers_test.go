package authz

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
	"github.com/go-chi/chi/v5"
)

func TestAuthzResolve_ReturnsDecision(t *testing.T) {
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Resolver: authzcore.Resolver{}})

	body := strings.NewReader(`{"subject":"agent:community-admin/coordinator","target":"node:community-admin/drive_admin","action":"add_viewer","instance":"community-admin","grants":[{"subject":"agent:community-admin/coordinator","target":"node:community-admin/drive_admin","actions":["add_viewer"]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/authz/resolve", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"allow":true`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}
