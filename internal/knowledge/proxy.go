package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const baseURL = "http://localhost:8204"

// Proxy forwards requests to the knowledge service via localhost port binding.
type Proxy struct {
	client *http.Client
}

// NewProxy creates a new knowledge proxy.
func NewProxy() *Proxy {
	return &Proxy{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Query sends a text query to the knowledge service.
func (p *Proxy) Query(ctx context.Context, text string) ([]byte, error) {
	body := map[string]string{"query": text}
	return p.post(ctx, "/query", body)
}

// WhoKnows finds who knows about a topic.
func (p *Proxy) WhoKnows(ctx context.Context, topic string) ([]byte, error) {
	return p.get(ctx, "/who-knows?topic="+urlEncode(topic))
}

// Stats returns knowledge graph statistics.
func (p *Proxy) Stats(ctx context.Context) ([]byte, error) {
	return p.get(ctx, "/stats")
}

// Export exports the knowledge graph in a given format.
func (p *Proxy) Export(ctx context.Context, format string) ([]byte, error) {
	return p.get(ctx, "/export?format="+urlEncode(format))
}

// Changes returns changes since a given timestamp.
func (p *Proxy) Changes(ctx context.Context, since string) ([]byte, error) {
	return p.get(ctx, "/changes?since="+urlEncode(since))
}

// Context returns context for a subject.
func (p *Proxy) Context(ctx context.Context, subject string) ([]byte, error) {
	return p.get(ctx, "/context?subject="+urlEncode(subject))
}

// Neighbors returns neighboring nodes for a given node ID.
func (p *Proxy) Neighbors(ctx context.Context, nodeID string) ([]byte, error) {
	return p.get(ctx, "/neighbors?node_id="+urlEncode(nodeID))
}

// Path finds a path between two nodes.
func (p *Proxy) Path(ctx context.Context, from, to string) ([]byte, error) {
	return p.get(ctx, "/path?from="+urlEncode(from)+"&to="+urlEncode(to))
}

// Flags returns curation flags.
func (p *Proxy) Flags(ctx context.Context) ([]byte, error) {
	return p.get(ctx, "/curation/flags")
}

// Restore restores a curated node.
func (p *Proxy) Restore(ctx context.Context, nodeID string) ([]byte, error) {
	body := map[string]string{"node_id": nodeID}
	return p.post(ctx, "/curation/restore", body)
}

// CurationLog returns the curation log.
func (p *Proxy) CurationLog(ctx context.Context) ([]byte, error) {
	return p.get(ctx, "/curation/log")
}

// QueryByMission queries the knowledge graph filtered by mission_id tag.
// ASK tenet 24: knowledge access is bounded by authorization scope.
func (p *Proxy) QueryByMission(ctx context.Context, text string, missionID string) ([]byte, error) {
	body := map[string]string{
		"query":      text,
		"mission_id": missionID,
	}
	return p.post(ctx, "/query", body)
}

// ContributeWithMission contributes knowledge tagged with a mission_id.
func (p *Proxy) ContributeWithMission(ctx context.Context, content string, missionID string, agentName string) ([]byte, error) {
	body := map[string]interface{}{
		"content":    content,
		"mission_id": missionID,
		"agent":      agentName,
	}
	return p.post(ctx, "/contribute", body)
}

// QueryForAgent queries excluding nodes tagged with a mission_id that the
// querying agent is not assigned to.
func (p *Proxy) QueryForAgent(ctx context.Context, text string, agentMissionID string) ([]byte, error) {
	body := map[string]string{
		"query":            text,
		"agent_mission_id": agentMissionID,
	}
	return p.post(ctx, "/query", body)
}

// Get is an exported helper for arbitrary GET requests to the knowledge service.
func (p *Proxy) Get(ctx context.Context, path string) ([]byte, error) {
	return p.get(ctx, path)
}

// Post is an exported helper for arbitrary POST requests to the knowledge service.
func (p *Proxy) Post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	return p.post(ctx, path, body)
}

// Pending returns org-structural knowledge contributions awaiting operator review.
func (p *Proxy) Pending(ctx context.Context) ([]byte, error) {
	return p.get(ctx, "/pending")
}

// Review approves or rejects a pending org-structural contribution by ID.
// action must be "approve" or "reject".
func (p *Proxy) Review(ctx context.Context, id string, action string, reason string) ([]byte, error) {
	body := map[string]string{
		"action": action,
		"reason": reason,
	}
	return p.post(ctx, "/review/"+id, body)
}

// URLEncode is an exported helper for URL-encoding query parameter values.
func URLEncode(s string) string {
	return urlEncode(s)
}

// --- internal helpers ---

func (p *Proxy) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("knowledge GET %s: %w", path, err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("knowledge GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("knowledge GET %s: read body: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("knowledge returned %d: %s", resp.StatusCode, string(out))
	}
	return bytes.TrimSpace(out), nil
}

func (p *Proxy) post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("knowledge POST %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("knowledge POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("knowledge POST %s: read body: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("knowledge returned %d: %s", resp.StatusCode, string(out))
	}
	return bytes.TrimSpace(out), nil
}

func urlEncode(s string) string {
	r := strings.NewReplacer(
		" ", "%20",
		"&", "%26",
		"=", "%3D",
		"+", "%2B",
		"#", "%23",
		"?", "%3F",
		"/", "%2F",
	)
	return r.Replace(s)
}
