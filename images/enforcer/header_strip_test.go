package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeaderStripperRemovesPlatformHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Agency-Platform") != "" {
			t.Error("X-Agency-Platform header should have been stripped")
		}
		w.WriteHeader(200)
	})

	stripper := NewHeaderStripper(inner)
	req := httptest.NewRequest("POST", "/channels/test/messages", nil)
	req.Header.Set("X-Agency-Platform", "true")
	rec := httptest.NewRecorder()
	stripper.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHeaderStripperRemovesCacheRelayHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Agency-Cache-Relay") != "" {
			t.Error("X-Agency-Cache-Relay header should have been stripped")
		}
		w.WriteHeader(200)
	})

	stripper := NewHeaderStripper(inner)
	req := httptest.NewRequest("POST", "/channels/test/messages", nil)
	req.Header.Set("X-Agency-Cache-Relay", "worker-1")
	rec := httptest.NewRecorder()
	stripper.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHeaderStripperPassesNormalHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type should be preserved")
		}
		w.WriteHeader(200)
	})

	stripper := NewHeaderStripper(inner)
	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	stripper.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
