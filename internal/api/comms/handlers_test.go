package comms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubCommsClient struct {
	responses map[string][]byte
}

func (s *stubCommsClient) CommsRequest(_ context.Context, method, path string, _ interface{}) ([]byte, error) {
	return s.responses[method+" "+path], nil
}

func TestListChannelsExcludesArchivedByDefault(t *testing.T) {
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":               []byte(`[{"name":"general","state":"active"},{"name":"playwright-old","state":"archived"}]`),
			"GET /channels?member=_operator": []byte(`[{"name":"dm-playwright-agent","state":"archived"},{"name":"dm-henry","state":"active"}]`),
		}},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/comms/channels", nil)
	rec := httptest.NewRecorder()
	h.listChannels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 channels, got %d: %s", len(got), rec.Body.String())
	}
	for _, channel := range got {
		if channel["state"] == "archived" {
			t.Fatalf("archived channel leaked into default response: %v", channel)
		}
	}
}

func TestListChannelsIncludesArchivedWhenRequested(t *testing.T) {
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":               []byte(`[{"name":"general","state":"active"},{"name":"playwright-old","state":"archived"}]`),
			"GET /channels?member=_operator": []byte(`[]`),
		}},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/comms/channels?include_archived=true", nil)
	rec := httptest.NewRecorder()
	h.listChannels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected archived channel to be included, got %d: %s", len(got), rec.Body.String())
	}
}
