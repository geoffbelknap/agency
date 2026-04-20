package comms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/go-chi/chi/v5"
)

type stubCommsClient struct {
	responses map[string][]byte
	requests  []string
}

func (s *stubCommsClient) CommsRequest(_ context.Context, method, path string, _ interface{}) ([]byte, error) {
	s.requests = append(s.requests, method+" "+path)
	return s.responses[method+" "+path], nil
}

type stubAgentLister struct {
	agents []orchestrate.AgentDetail
}

func (s *stubAgentLister) List(_ context.Context) ([]orchestrate.AgentDetail, error) {
	return s.agents, nil
}

type stubAgentNamer struct {
	names      []string
	listCalls  int
	namesCalls int
}

func (s *stubAgentNamer) List(_ context.Context) ([]orchestrate.AgentDetail, error) {
	s.listCalls++
	return nil, nil
}

func (s *stubAgentNamer) Names(_ context.Context) ([]string, error) {
	s.namesCalls++
	return s.names, nil
}

func TestListChannelsExcludesArchivedByDefault(t *testing.T) {
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":                  []byte(`[{"name":"general","state":"active"},{"name":"playwright-old","state":"archived"}]`),
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

func TestListChannelsUsesAgentNamesWithoutLoadingDetails(t *testing.T) {
	agents := &stubAgentNamer{names: []string{"henry"}}
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":                  []byte(`[{"name":"general","state":"active"}]`),
			"GET /channels?member=_operator": []byte(`[{"name":"dm-retired-agent","state":"active"},{"name":"dm-henry","state":"active"}]`),
		}},
		AgentManager: agents,
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/comms/channels", nil)
	rec := httptest.NewRecorder()
	h.listChannels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if agents.namesCalls != 1 {
		t.Fatalf("expected one Names call, got %d", agents.namesCalls)
	}
	if agents.listCalls != 0 {
		t.Fatalf("expected no List calls, got %d", agents.listCalls)
	}
}

func TestListChannelsIncludesArchivedWhenRequested(t *testing.T) {
	h := &handler{deps: Deps{
		Comms: &stubCommsClient{responses: map[string][]byte{
			"GET /channels":                  []byte(`[{"name":"general","state":"active"},{"name":"playwright-old","state":"archived"}]`),
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
			"GET /channels":                  []byte(`[{"name":"general","state":"active"}]`),
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
			"GET /channels":                  []byte(`[{"name":"general","state":"active"}]`),
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
			"GET /channels":                  []byte(`[{"name":"general","state":"active"}]`),
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

func TestReadMessagesForwardsEncodedSince(t *testing.T) {
	comms := &stubCommsClient{responses: map[string][]byte{
		"GET /channels/dm-henry/messages?limit=25&reader=_operator&since=2026-04-19T01%3A39%3A35.833388%2B00%3A00": []byte(`[]`),
	}}
	h := &handler{deps: Deps{Comms: comms}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/comms/channels/dm-henry/messages?since=2026-04-19T01%3A39%3A35.833388%2B00%3A00&limit=25", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "dm-henry")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.readMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(comms.requests) != 1 {
		t.Fatalf("expected one comms request, got %d", len(comms.requests))
	}
	want := "GET /channels/dm-henry/messages?limit=25&reader=_operator&since=2026-04-19T01%3A39%3A35.833388%2B00%3A00"
	if comms.requests[0] != want {
		t.Fatalf("expected %q, got %q", want, comms.requests[0])
	}
}
