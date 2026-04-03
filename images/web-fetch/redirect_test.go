package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirectToBlockedHost(t *testing.T) {
	bl := NewBlocklist()
	bl.AddDeny("evil.internal")

	cfg := Config{Fetch: FetchConfig{
		FollowRedirects: true,
		MaxRedirects:    5,
		TimeoutSeconds:  5,
	}}
	client := buildHTTPClient(cfg, bl)

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.internal/payload", http.StatusFound)
	}))
	defer redirectServer.Close()

	resp, err := client.Get(redirectServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 (redirect stopped), got %d", resp.StatusCode)
	}
}
