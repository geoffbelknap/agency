package comms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoffbelknap/agency/internal/orchestrate"
)

type stubCommsClient struct {
	responses map[string][]byte
}

func (s *stubCommsClient) CommsRequest(_ context.Context, method, path string, _ interface{}) ([]byte, error) {
	return s.responses[method+" "+path], nil
}

type stubAgentLister struct {
	agents []orchestrate.AgentDetail
}

func (s *stubAgentLister) List(_ context.Context) ([]orchestrate.AgentDetail, error) {
	return s.agents, nil
}

func TestListChannelsExcludesArchivedByDefault(t *testing.T) {
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":               []byte(`[{"name":"general","state":"active"},{"name":"playwright-old","state":"archived"}]`),
			"GET /channels?member=_operator": []byte(`[{"name":"dm-playwright-agent","state":"archived"},{"name":"dm-henry","state":"active"}]`),
		}},
		AgentManager: &stubAgentLister{agents: []orchestrate.AgentDetail{{Name: "henry"}}},
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
		AgentManager: &stubAgentLister{},
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

func TestListChannelsExcludesOrphanDMsByDefault(t *testing.T) {
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":               []byte(`[{"name":"general","state":"active"}]`),
			"GET /channels?member=_operator": []byte(`[{"name":"dm-retired-agent","state":"active"},{"name":"dm-henry","state":"active"}]`),
		}},
		AgentManager: &stubAgentLister{agents: []orchestrate.AgentDetail{{Name: "henry"}}},
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

	for _, channel := range got {
		if channel["name"] == "dm-retired-agent" {
			t.Fatalf("orphaned DM leaked into default response: %v", channel)
		}
	}
}

func TestListChannelsIncludesOrphanDMsWhenArchivedRequested(t *testing.T) {
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":               []byte(`[{"name":"general","state":"active"}]`),
			"GET /channels?member=_operator": []byte(`[{"name":"dm-retired-agent","state":"active"}]`),
		}},
		AgentManager: &stubAgentLister{},
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
		t.Fatalf("expected orphan DM to be included with include_archived=true, got %d: %s", len(got), rec.Body.String())
	}
}

func TestListChannelsIncludesUnavailableDMsWhenRequested(t *testing.T) {
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":               []byte(`[{"name":"general","state":"active"}]`),
			"GET /channels?member=_operator": []byte(`[{"name":"dm-retired-agent","state":"active"}]`),
		}},
		AgentManager: &stubAgentLister{},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/comms/channels?include_unavailable=true", nil)
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
		t.Fatalf("expected unavailable DM to be included, got %d: %s", len(got), rec.Body.String())
	}
	for _, channel := range got {
		if channel["name"] == "dm-retired-agent" {
			if channel["availability"] != "unavailable" {
				t.Fatalf("expected unavailable DM to be tagged, got %v", channel)
			}
			return
		}
	}
	t.Fatalf("expected unavailable DM to be present, got %s", rec.Body.String())
}
