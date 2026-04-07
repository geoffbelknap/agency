package comms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClient_CommsRequest_GET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/channels" {
			t.Errorf("expected /channels, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	data, err := c.CommsRequest(context.Background(), "GET", "/channels", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("unexpected body: %s", string(data))
	}
}

func TestHTTPClient_CommsRequest_POST_WithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content-type")
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "general" {
			t.Errorf("expected name=general, got %s", body["name"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"created":true}`))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	data, err := c.CommsRequest(context.Background(), "POST", "/channels", map[string]string{"name": "general"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"created":true}` {
		t.Errorf("unexpected body: %s", string(data))
	}
}

func TestHTTPClient_CommsRequest_PlatformHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Agency-Platform") != "true" {
			t.Errorf("expected X-Agency-Platform header on grant-access path")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.CommsRequest(context.Background(), "POST", "/channels/general/grant-access", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_CommsRequest_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`not found`))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.CommsRequest(context.Background(), "GET", "/missing", nil)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
