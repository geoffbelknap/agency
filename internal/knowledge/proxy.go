package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// Import imports a knowledge graph from a JSON export.
func (p *Proxy) Import(ctx context.Context, data []byte) ([]byte, error) {
	return p.post(ctx, "/import", json.RawMessage(data))
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

// Classification returns the current classification config from the knowledge service.
func (p *Proxy) Classification(ctx context.Context) (json.RawMessage, error) {
	return p.getRaw(ctx, "/classification")
}

// Communities returns the list of communities from the knowledge service.
func (p *Proxy) Communities(ctx context.Context) (json.RawMessage, error) {
	return p.getRaw(ctx, "/communities")
}

// Community returns a single community by ID.
func (p *Proxy) Community(ctx context.Context, id string) (json.RawMessage, error) {
	return p.getRaw(ctx, "/community/"+url.PathEscape(id))
}

// Hubs returns knowledge hubs, optionally limited.
func (p *Proxy) Hubs(ctx context.Context, limit int) (json.RawMessage, error) {
	path := "/hubs"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	return p.getRaw(ctx, path)
}

// Get is an exported helper for arbitrary GET requests to the knowledge service.
func (p *Proxy) Get(ctx context.Context, path string) ([]byte, error) {
	return p.get(ctx, path)
}

// Post is an exported helper for arbitrary POST requests to the knowledge service.
func (p *Proxy) Post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	return p.post(ctx, path, body)
}

// Ingest sends content to the knowledge service for ingestion.
func (p *Proxy) Ingest(ctx context.Context, content, filename, contentType string, scope json.RawMessage) (json.RawMessage, error) {
	body := map[string]interface{}{
		"content":      content,
		"filename":     filename,
		"content_type": contentType,
	}
	if scope != nil {
		body["scope"] = json.RawMessage(scope)
	}
	b, err := p.post(ctx, "/ingest", body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// SaveInsight saves an agent-generated insight to the knowledge graph.
func (p *Proxy) SaveInsight(ctx context.Context, insight string, sourceNodes []string, confidence string, tags []string, agentName string) (json.RawMessage, error) {
	body := map[string]interface{}{
		"insight":      insight,
		"source_nodes": sourceNodes,
		"confidence":   confidence,
		"agent_name":   agentName,
	}
	if len(tags) > 0 {
		body["tags"] = tags
	}
	b, err := p.post(ctx, "/insight", body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
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

// MemoryProposals returns durable-memory proposals matching a review status.
func (p *Proxy) MemoryProposals(ctx context.Context, status string, limit int) ([]byte, error) {
	path := "/memory/proposals"
	params := []string{}
	if status != "" {
		params = append(params, "status="+urlEncode(status))
	}
	if limit > 0 {
		params = append(params, "limit="+strconv.Itoa(limit))
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}
	return p.get(ctx, path)
}

// ReviewMemoryProposal approves or rejects a durable-memory proposal by ID.
func (p *Proxy) ReviewMemoryProposal(ctx context.Context, id string, action string, reason string) ([]byte, error) {
	body := map[string]string{
		"action": action,
		"reason": reason,
	}
	return p.post(ctx, "/memory/proposals/"+urlEncode(id)+"/review", body)
}

// Principals returns the list of registered principals, optionally filtered by type.
func (p *Proxy) Principals(ctx context.Context, principalType string) ([]byte, error) {
	path := "/principals"
	if principalType != "" {
		path += "?type=" + urlEncode(principalType)
	}
	return p.get(ctx, path)
}

// RegisterPrincipal registers a new principal with the given type and name.
func (p *Proxy) RegisterPrincipal(ctx context.Context, principalType, name string) ([]byte, error) {
	body := map[string]string{"type": principalType, "name": name}
	return p.post(ctx, "/principals", body)
}

// ResolvePrincipal resolves a principal by UUID.
func (p *Proxy) ResolvePrincipal(ctx context.Context, uuid string) ([]byte, error) {
	return p.get(ctx, "/principals/"+urlEncode(uuid))
}

// Quarantine quarantines knowledge contributed by an agent, optionally since a timestamp.
// ASK tenet 16: quarantine is immediate, silent, and complete.
func (p *Proxy) Quarantine(ctx context.Context, agent, since string) (json.RawMessage, error) {
	body := map[string]string{"agent": agent}
	if since != "" {
		body["since"] = since
	}
	b, err := p.post(ctx, "/quarantine", body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// QuarantineRelease releases quarantined nodes by node ID or agent name.
func (p *Proxy) QuarantineRelease(ctx context.Context, nodeID, agent string) (json.RawMessage, error) {
	body := map[string]string{}
	if nodeID != "" {
		body["node_id"] = nodeID
	}
	if agent != "" {
		body["agent"] = agent
	}
	b, err := p.post(ctx, "/quarantine/release", body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// QuarantineList lists quarantined nodes, optionally filtered by agent.
func (p *Proxy) QuarantineList(ctx context.Context, agent string) (json.RawMessage, error) {
	path := "/quarantine"
	if agent != "" {
		path += "?agent=" + urlEncode(agent)
	}
	return p.getRaw(ctx, path)
}

// URLEncode is an exported helper for URL-encoding query parameter values.
func URLEncode(s string) string {
	return urlEncode(s)
}

// --- internal helpers ---

func (p *Proxy) getRaw(ctx context.Context, path string) (json.RawMessage, error) {
	b, err := p.get(ctx, path)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func (p *Proxy) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("knowledge GET %s: %w", path, err)
	}
	req.Header.Set("X-Agency-Platform", "true")
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
	req.Header.Set("X-Agency-Platform", "true")
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
