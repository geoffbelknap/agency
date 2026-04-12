package packages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/go-chi/chi/v5"
)

func TestPackagesList_ReturnsInstalledPackages(t *testing.T) {
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Registry: testPackageRegistry(t)})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/packages?kind=connector", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "slack-interactivity") {
		t.Fatalf("missing package in response: %s", rec.Body.String())
	}
}

func TestPackagesShow_ReturnsInstalledPackage(t *testing.T) {
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Registry: testPackageRegistry(t)})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/packages/connector/slack-interactivity", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"connector\"") {
		t.Fatalf("missing package kind in response: %s", rec.Body.String())
	}
}

func testPackageRegistry(t *testing.T) *hub.Registry {
	t.Helper()

	reg := hub.NewRegistry(t.TempDir())
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind:      "connector",
		Name:      "slack-interactivity",
		Version:   "1.0.0",
		Trust:     "verified",
		Installed: time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		Path:      "/tmp/slack-interactivity",
	}); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}
	return reg
}
