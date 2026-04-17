package infra

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestInfraStatus_NonDockerBackendReturnsBackendAwareStatus(t *testing.T) {
	h := &handler{deps: Deps{
		Config: &config.Config{
			Home:    t.TempDir(),
			Version: "test",
			Hub: config.HubConfig{
				DeploymentBackend: "probe",
			},
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/status", nil)
	rec := httptest.NewRecorder()

	h.infraStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body["backend"]; got != "probe" {
		t.Fatalf("backend = %v, want probe", got)
	}
	if got := body["docker"]; got != "not_applicable" {
		t.Fatalf("docker = %v, want not_applicable", got)
	}
	if got := body["infra_control_available"]; got != false {
		t.Fatalf("infra_control_available = %v, want false", got)
	}
}

func TestInfraUp_NonDockerBackendReturnsBackendSpecificUnavailable(t *testing.T) {
	h := &handler{deps: Deps{
		Config: &config.Config{
			Home: t.TempDir(),
			Hub: config.HubConfig{
				DeploymentBackend: "probe",
			},
		},
	}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/infra/up", nil)
	rec := httptest.NewRecorder()

	h.infraUp(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["backend"] != "probe" {
		t.Fatalf("backend = %q, want probe", body["backend"])
	}
	if body["error"] == "" {
		t.Fatal("expected non-empty error")
	}
}
