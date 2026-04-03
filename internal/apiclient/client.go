package apiclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Client is a thin REST client for the Agency gateway API.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient creates a client for the given gateway base URL.
// It loads the operator token from ~/.agency/config.yaml if present.
func NewClient(baseURL string) *Client {
	c := &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 300 * time.Second},
	}
	c.Token = loadToken()
	return c
}

// loadToken reads the operator token from ~/.agency/config.yaml.
func loadToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".agency", "config.yaml"))
	if err != nil {
		return ""
	}
	var cfg struct {
		Token string `yaml:"token"`
	}
	if yaml.Unmarshal(data, &cfg) == nil {
		return cfg.Token
	}
	return ""
}

// CheckGateway returns nil if the gateway is reachable, or an error with a
// helpful message if not.
func (c *Client) CheckGateway() error {
	conn, err := net.DialTimeout("tcp", c.BaseURL[len("http://"):], 2*time.Second)
	if err != nil {
		return fmt.Errorf("gateway not running at %s\nStart it with: agency serve", c.BaseURL)
	}
	conn.Close()
	return nil
}

// do executes an HTTP request with auth header and returns the response body.
func (c *Client) do(method, path string, body interface{}) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("X-Agency-Token", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gateway unreachable at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		// Try to extract error message from JSON response
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &errResp) == nil && errResp.Error != "" {
			return data, resp.StatusCode, fmt.Errorf("%s", errResp.Error)
		}
		return data, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, resp.StatusCode, nil
}

// Get performs a GET request and returns the raw response body.
func (c *Client) Get(path string) ([]byte, error) {
	data, _, err := c.do("GET", path, nil)
	return data, err
}

// Post performs a POST request and returns the raw response body.
func (c *Client) Post(path string, body interface{}) ([]byte, error) {
	data, _, err := c.do("POST", path, body)
	return data, err
}

// Delete performs a DELETE request and returns the raw response body.
func (c *Client) Delete(path string) ([]byte, error) {
	data, _, err := c.do("DELETE", path, nil)
	return data, err
}

// Put performs a PUT request and returns the raw response body.
func (c *Client) Put(path string, body interface{}) ([]byte, error) {
	data, _, err := c.do("PUT", path, body)
	return data, err
}

// GetJSON performs a GET and unmarshals the response into v.
func (c *Client) GetJSON(path string, v interface{}) error {
	data, err := c.Get(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// PostJSON performs a POST and unmarshals the response into result.
func (c *Client) PostJSON(path string, body, result interface{}) error {
	data, err := c.Post(path, body)
	if err != nil {
		return err
	}
	if result != nil {
		return json.Unmarshal(data, result)
	}
	return nil
}

// DeleteJSON performs a DELETE and unmarshals the response into result.
func (c *Client) DeleteJSON(path string, result interface{}) error {
	data, err := c.Delete(path)
	if err != nil {
		return err
	}
	if result != nil {
		return json.Unmarshal(data, result)
	}
	return nil
}

// ── Health ──────────────────────────────────────────────────────────────────

func (c *Client) Health() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/health", &result)
	return result, err
}

// ── Agents ──────────────────────────────────────────────────────────────────

func (c *Client) ListAgents() ([]map[string]interface{}, error) {
	var agents []map[string]interface{}
	err := c.GetJSON("/api/v1/agents", &agents)
	return agents, err
}

func (c *Client) ShowAgent(name string) (map[string]interface{}, error) {
	var agent map[string]interface{}
	data, err := c.Get("/api/v1/agents/" + name)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(data, &agent)
	return agent, nil
}

// GetAgentBudget returns budget usage for an agent.
func (c *Client) GetAgentBudget(name string) (map[string]interface{}, error) {
	var budget map[string]interface{}
	data, err := c.Get("/api/v1/agents/" + name + "/budget")
	if err != nil {
		return nil, err
	}
	json.Unmarshal(data, &budget)
	return budget, nil
}

func (c *Client) CreateAgent(name, preset string) (map[string]string, error) {
	body := map[string]string{"name": name, "preset": preset}
	var result map[string]string
	err := c.PostJSON("/api/v1/agents", body, &result)
	return result, err
}

func (c *Client) DeleteAgent(name string) error {
	_, err := c.Delete("/api/v1/agents/" + name)
	return err
}

func (c *Client) StartAgent(name string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/agents/"+name+"/start", nil, &result)
	return result, err
}

// StartAgentStream starts an agent with streaming phase progress.
func (c *Client) StartAgentStream(name string, onProgress func(component, status string)) error {
	return c.streamPost("/api/v1/agents/"+name+"/start", onProgress)
}

func (c *Client) StopAgent(name string) error {
	body := map[string]string{"type": "immediate", "reason": "operator stop"}
	_, err := c.Post("/api/v1/agents/"+name+"/stop", body)
	return err
}

func (c *Client) RestartAgent(name string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/agents/"+name+"/restart", nil, &result)
	return result, err
}

func (c *Client) HaltAgent(name, haltType, reason string) (map[string]interface{}, error) {
	body := map[string]string{"type": haltType, "reason": reason}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/agents/"+name+"/halt", body, &result)
	return result, err
}

func (c *Client) ResumeAgent(name string) error {
	_, err := c.Post("/api/v1/agents/"+name+"/resume", map[string]string{})
	return err
}

func (c *Client) GrantAgent(name, capability string) (map[string]string, error) {
	body := map[string]string{"capability": capability}
	var result map[string]string
	err := c.PostJSON("/api/v1/agents/"+name+"/grant", body, &result)
	return result, err
}

func (c *Client) RevokeAgent(name, capability string) (map[string]string, error) {
	body := map[string]string{"capability": capability}
	var result map[string]string
	err := c.PostJSON("/api/v1/agents/"+name+"/revoke", body, &result)
	return result, err
}

// ── Channels ────────────────────────────────────────────────────────────────

func (c *Client) ListChannels() ([]map[string]interface{}, error) {
	var channels []map[string]interface{}
	err := c.GetJSON("/api/v1/channels", &channels)
	return channels, err
}

func (c *Client) ReadChannel(name string, limit int) ([]map[string]interface{}, error) {
	path := fmt.Sprintf("/api/v1/channels/%s/messages?limit=%d", name, limit)
	var messages []map[string]interface{}
	err := c.GetJSON(path, &messages)
	return messages, err
}

func (c *Client) SendMessage(channel, content string) (map[string]interface{}, error) {
	return c.SendMessageWithMetadata(channel, content, nil)
}

func (c *Client) SendMessageWithMetadata(channel, content string, metadata map[string]interface{}) (map[string]interface{}, error) {
	body := map[string]interface{}{"content": content, "author": "_operator"}
	if len(metadata) > 0 {
		body["metadata"] = metadata
	}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/channels/"+channel+"/messages", body, &result)
	return result, err
}

func (c *Client) SearchMessages(query, channel string) ([]map[string]interface{}, error) {
	params := url.Values{"q": {query}}
	if channel != "" {
		params.Set("channel", channel)
	}
	var results []map[string]interface{}
	err := c.GetJSON("/api/v1/channels/search?"+params.Encode(), &results)
	return results, err
}

func (c *Client) CreateChannel(name, topic string) (map[string]interface{}, error) {
	body := map[string]string{"name": name, "topic": topic}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/channels", body, &result)
	return result, err
}

func (c *Client) ArchiveChannel(name string) error {
	_, err := c.Post("/api/v1/channels/"+name+"/archive", nil)
	return err
}

// ── Infrastructure ──────────────────────────────────────────────────────────

// InfraStatusResponse wraps infrastructure status with gateway build info.
type InfraStatusResponse struct {
	Version             string              `json:"version"`
	BuildID             string              `json:"build_id"`
	GatewayURL          string              `json:"gateway_url"`
	WebURL              string              `json:"web_url"`
	Components          []map[string]string `json:"components"`
	InfraLLMDailyUsed   float64             `json:"infra_llm_daily_used"`
	InfraLLMDailyLimit  float64             `json:"infra_llm_daily_limit"`
}

func (c *Client) InfraStatus() (*InfraStatusResponse, error) {
	var resp InfraStatusResponse
	err := c.GetJSON("/api/v1/infra/status", &resp)
	return &resp, err
}

func (c *Client) InfraUp() error {
	_, err := c.Post("/api/v1/infra/up", nil)
	return err
}

// InfraUpStream starts infrastructure with streaming progress events.
func (c *Client) InfraUpStream(onProgress func(component, status string)) error {
	return c.streamPost("/api/v1/infra/up", onProgress)
}

// streamPost sends a POST with Accept: application/x-ndjson and calls
// onProgress for each progress event in the NDJSON stream.
func (c *Client) streamPost(path string, onProgress func(component, status string)) error {
	req, err := http.NewRequest("POST", c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/x-ndjson")
	if c.Token != "" {
		req.Header.Set("X-Agency-Token", c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("gateway unreachable at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	// Non-streaming error response (e.g. 404 agent not found)
	if resp.StatusCode >= 400 {
		var errResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
			if msg := errResp["error"]; msg != "" {
				return fmt.Errorf("%s", msg)
			}
		}
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	for dec.More() {
		var event map[string]interface{}
		if err := dec.Decode(&event); err != nil {
			return fmt.Errorf("decode progress: %w", err)
		}
		eventType, _ := event["type"].(string)
		switch eventType {
		case "progress":
			component, _ := event["component"].(string)
			status, _ := event["status"].(string)
			if onProgress != nil {
				onProgress(component, status)
			}
		case "phase":
			name, _ := event["name"].(string)
			desc, _ := event["description"].(string)
			if onProgress != nil {
				onProgress(name, desc)
			}
		case "error":
			errMsg, _ := event["error"].(string)
			return fmt.Errorf("%s", errMsg)
		case "done", "complete":
			return nil
		}
	}
	return nil
}

func (c *Client) InfraDown() error {
	_, err := c.Post("/api/v1/infra/down", nil)
	return err
}

// InfraDownStream stops infrastructure with streaming progress events.
func (c *Client) InfraDownStream(onProgress func(component, status string)) error {
	return c.streamPost("/api/v1/infra/down", onProgress)
}

func (c *Client) InfraRebuild(component string) error {
	_, err := c.Post("/api/v1/infra/rebuild/"+component, nil)
	return err
}

// InfraRebuildStream rebuilds a component with streaming progress events.
func (c *Client) InfraRebuildStream(component string, onProgress func(component, status string)) error {
	return c.streamPost("/api/v1/infra/rebuild/"+component, onProgress)
}

func (c *Client) InfraReload() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/infra/reload", nil, &result)
	return result, err
}

// ── Hub ─────────────────────────────────────────────────────────────────────

// HubUpdateReport mirrors hub.UpdateReport for client consumption.
type HubUpdateReport struct {
	Sources   []HubSourceUpdate     `json:"sources"`
	Available []HubAvailableUpgrade `json:"available,omitempty"`
	Warnings  []string              `json:"warnings,omitempty"`
}

type HubSourceUpdate struct {
	Name        string `json:"name"`
	OldCommit   string `json:"old_commit"`
	NewCommit   string `json:"new_commit"`
	CommitCount int    `json:"commit_count"`
}

type HubAvailableUpgrade struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Category         string `json:"category,omitempty"`
	InstalledVersion string `json:"installed_version"`
	AvailableVersion string `json:"available_version"`
	Summary          string `json:"summary,omitempty"`
}

func (c *Client) HubUpdate() (*HubUpdateReport, error) {
	var result HubUpdateReport
	err := c.PostJSON("/api/v1/hub/update", nil, &result)
	return &result, err
}

// HubUpgradeReport mirrors hub.UpgradeReport for client consumption.
type HubUpgradeReport struct {
	Files      []HubFileUpgrade      `json:"files,omitempty"`
	Components []HubComponentUpgrade `json:"components,omitempty"`
	Warnings   []string              `json:"warnings,omitempty"`
}

type HubFileUpgrade struct {
	Category string `json:"category"`
	Path     string `json:"path"`
	Status   string `json:"status"`
	Summary  string `json:"summary,omitempty"`
}

type HubComponentUpgrade struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

func (c *Client) HubOutdated() ([]HubAvailableUpgrade, error) {
	var result []HubAvailableUpgrade
	err := c.GetJSON("/api/v1/hub/outdated", &result)
	return result, err
}

func (c *Client) HubUpgrade(components []string) (*HubUpgradeReport, error) {
	var body interface{}
	if len(components) > 0 {
		body = map[string]interface{}{"components": components}
	}
	var result HubUpgradeReport
	err := c.PostJSON("/api/v1/hub/upgrade", body, &result)
	return &result, err
}

func (c *Client) HubSearch(query, kind string) ([]map[string]string, error) {
	params := url.Values{"q": {query}}
	if kind != "" {
		params.Set("kind", kind)
	}
	var results []map[string]string
	err := c.GetJSON("/api/v1/hub/search?"+params.Encode(), &results)
	return results, err
}

func (c *Client) HubInstall(name, kind, source string) error {
	body := map[string]string{"name": name, "kind": kind, "source": source}
	_, err := c.Post("/api/v1/hub/install", body)
	return err
}

func (c *Client) HubRemove(name, kind string) error {
	_, err := c.Delete("/api/v1/hub/" + name + "?kind=" + kind)
	return err
}

func (c *Client) HubList() ([]map[string]string, error) {
	var items []map[string]string
	err := c.GetJSON("/api/v1/hub/installed", &items)
	return items, err
}

func (c *Client) HubInfo(name, kind string) (map[string]interface{}, error) {
	params := ""
	if kind != "" {
		params = "?kind=" + kind
	}
	var info map[string]interface{}
	err := c.GetJSON("/api/v1/hub/"+name+"/info"+params, &info)
	return info, err
}

func (c *Client) HubShow(nameOrID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/hub/"+nameOrID, &result)
	return result, err
}

func (c *Client) HubActivate(nameOrID string) error {
	_, err := c.Post("/api/v1/hub/"+nameOrID+"/activate", nil)
	return err
}

func (c *Client) HubDeactivate(nameOrID string) error {
	_, err := c.Post("/api/v1/hub/"+nameOrID+"/deactivate", nil)
	return err
}

func (c *Client) HubInstances(kind string) ([]map[string]interface{}, error) {
	path := "/api/v1/hub/instances"
	if kind != "" {
		path += "?kind=" + kind
	}
	var result []map[string]interface{}
	err := c.GetJSON(path, &result)
	return result, err
}

// ── Capabilities ────────────────────────────────────────────────────────────

func (c *Client) CapList() ([]map[string]interface{}, error) {
	var caps []map[string]interface{}
	err := c.GetJSON("/api/v1/capabilities", &caps)
	return caps, err
}

func (c *Client) CapShow(name string) (map[string]interface{}, error) {
	var cap map[string]interface{}
	err := c.GetJSON("/api/v1/capabilities/"+name, &cap)
	return cap, err
}

func (c *Client) CapEnable(name, key string, agents []string) error {
	body := map[string]interface{}{"key": key, "agents": agents}
	_, err := c.Post("/api/v1/capabilities/"+name+"/enable", body)
	return err
}

func (c *Client) CapDisable(name string) error {
	_, err := c.Post("/api/v1/capabilities/"+name+"/disable", nil)
	return err
}

func (c *Client) CapAdd(kind, name string, spec map[string]interface{}) error {
	body := map[string]interface{}{"kind": kind, "name": name, "spec": spec}
	_, err := c.Post("/api/v1/capabilities", body)
	return err
}

func (c *Client) CapDelete(name string) error {
	_, err := c.Delete("/api/v1/capabilities/" + name)
	return err
}

// ── Agent Logs ──────────────────────────────────────────────────────────────

func (c *Client) AgentLogs(name, since, until string) ([]map[string]interface{}, error) {
	params := url.Values{}
	if since != "" {
		params.Set("since", since)
	}
	if until != "" {
		params.Set("until", until)
	}
	path := "/api/v1/agents/" + name + "/logs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var events []map[string]interface{}
	err := c.GetJSON(path, &events)
	return events, err
}

// ── Knowledge ───────────────────────────────────────────────────────────────

func (c *Client) KnowledgeQuery(text string) ([]byte, error) {
	return c.Post("/api/v1/knowledge/query", map[string]string{"text": text})
}

func (c *Client) KnowledgeWhoKnows(topic string) ([]byte, error) {
	return c.Get("/api/v1/knowledge/who-knows?topic=" + url.QueryEscape(topic))
}

func (c *Client) KnowledgeStats() ([]byte, error) {
	return c.Get("/api/v1/knowledge/stats")
}

func (c *Client) KnowledgeExport(format string) ([]byte, error) {
	if format == "" {
		format = "json"
	}
	return c.Get("/api/v1/knowledge/export?format=" + format)
}

func (c *Client) KnowledgeChanges(since string) ([]byte, error) {
	return c.Get("/api/v1/knowledge/changes?since=" + url.QueryEscape(since))
}

func (c *Client) KnowledgeContext(subject string) ([]byte, error) {
	return c.Get("/api/v1/knowledge/context?subject=" + url.QueryEscape(subject))
}

func (c *Client) KnowledgeNeighbors(nodeID string) ([]byte, error) {
	return c.Get("/api/v1/knowledge/neighbors?node_id=" + url.QueryEscape(nodeID))
}

func (c *Client) KnowledgePath(from, to string) ([]byte, error) {
	params := url.Values{"from": {from}, "to": {to}}
	return c.Get("/api/v1/knowledge/path?" + params.Encode())
}

func (c *Client) KnowledgeFlags() ([]byte, error) {
	return c.Get("/api/v1/knowledge/flags")
}

func (c *Client) KnowledgeRestore(nodeID string) ([]byte, error) {
	return c.Post("/api/v1/knowledge/restore", map[string]string{"node_id": nodeID})
}

func (c *Client) KnowledgeCurationLog() ([]byte, error) {
	return c.Get("/api/v1/knowledge/curation-log")
}

func (c *Client) KnowledgePending() ([]byte, error) {
	return c.Get("/api/v1/knowledge/pending")
}

func (c *Client) KnowledgeReview(id, action, reason string) ([]byte, error) {
	return c.Post("/api/v1/knowledge/review/"+id, map[string]string{
		"action": action,
		"reason": reason,
	})
}

// ── Knowledge Ontology ──────────────────────────────────────────────────────

func (c *Client) KnowledgeOntology() ([]byte, error) {
	return c.Get("/api/v1/knowledge/ontology")
}

func (c *Client) KnowledgeOntologyTypes() ([]byte, error) {
	return c.Get("/api/v1/knowledge/ontology/types")
}

func (c *Client) KnowledgeOntologyRelationships() ([]byte, error) {
	return c.Get("/api/v1/knowledge/ontology/relationships")
}

func (c *Client) KnowledgeOntologyValidate() ([]byte, error) {
	return c.Post("/api/v1/knowledge/ontology/validate", nil)
}

func (c *Client) KnowledgeOntologyMigrate(from, to string) ([]byte, error) {
	return c.Post("/api/v1/knowledge/ontology/migrate", map[string]string{
		"from": from,
		"to":   to,
	})
}

// ── Policy ──────────────────────────────────────────────────────────────────

func (c *Client) PolicyShow(agent string) (map[string]interface{}, error) {
	var policy map[string]interface{}
	data, err := c.Get("/api/v1/policy/" + agent)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(data, &policy)
	return policy, nil
}

func (c *Client) PolicyCheck(agent string) (map[string]interface{}, error) {
	var result map[string]interface{}
	// PolicyCheck calls the validate endpoint which checks the full policy chain
	err := c.PostJSON("/api/v1/policy/"+agent+"/validate", nil, &result)
	return result, err
}

func (c *Client) PolicyValidate(agent string) (map[string]interface{}, error) {
	return c.PolicyCheck(agent)
}

// ── Admin ───────────────────────────────────────────────────────────────────

func (c *Client) AdminDoctor() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/admin/doctor", &result)
	return result, err
}

func (c *Client) RoutingMetrics(agent, since, until string) (map[string]interface{}, error) {
	path := "/api/v1/routing/metrics?"
	if agent != "" {
		path += "agent=" + agent + "&"
	}
	if since != "" {
		path += "since=" + since + "&"
	}
	if until != "" {
		path += "until=" + until + "&"
	}
	var result map[string]interface{}
	err := c.GetJSON(path, &result)
	return result, err
}

func (c *Client) AdminDestroy() error {
	_, err := c.Post("/api/v1/admin/destroy", nil)
	return err
}

func (c *Client) AdminRebuild(agent string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/agents/"+url.PathEscape(agent)+"/rebuild", nil, &result)
	return result, err
}

func (c *Client) AdminTrust(action string, args map[string]string) (map[string]interface{}, error) {
	body := map[string]interface{}{"action": action, "args": args}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/admin/trust", body, &result)
	return result, err
}

func (c *Client) AdminAudit(agent string) ([]map[string]interface{}, error) {
	var events []map[string]interface{}
	err := c.GetJSON("/api/v1/admin/audit?agent="+url.QueryEscape(agent), &events)
	return events, err
}

func (c *Client) AdminEgress(agent string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/admin/egress?agent="+url.QueryEscape(agent), &result)
	return result, err
}

func (c *Client) AdminKnowledge(action string, args map[string]string) (map[string]interface{}, error) {
	body := map[string]interface{}{"action": action, "args": args}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/admin/knowledge", body, &result)
	return result, err
}

func (c *Client) AdminDepartment(action string, args map[string]string) (map[string]interface{}, error) {
	body := map[string]interface{}{"action": action, "args": args}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/admin/department", body, &result)
	return result, err
}

// ── Deploy ──────────────────────────────────────────────────────────────────

func (c *Client) Deploy(packPath string, credentials map[string]string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"pack_path":   packPath,
		"credentials": credentials,
	}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/deploy", body, &result)
	return result, err
}

func (c *Client) Teardown(packName string, del bool) error {
	body := map[string]bool{"delete": del}
	_, err := c.Post("/api/v1/teardown/"+packName, body)
	return err
}

// ── Teams ───────────────────────────────────────────────────────────────────

func (c *Client) TeamCreate(name string, agents []string) (map[string]string, error) {
	body := map[string]interface{}{"name": name, "agents": agents}
	var result map[string]string
	err := c.PostJSON("/api/v1/teams", body, &result)
	return result, err
}

func (c *Client) TeamList() ([]map[string]interface{}, error) {
	var teams []map[string]interface{}
	err := c.GetJSON("/api/v1/teams", &teams)
	return teams, err
}

func (c *Client) TeamShow(name string) (map[string]interface{}, error) {
	var team map[string]interface{}
	err := c.GetJSON("/api/v1/teams/"+name, &team)
	return team, err
}

func (c *Client) TeamActivity(name string) ([]map[string]interface{}, error) {
	var activity []map[string]interface{}
	err := c.GetJSON("/api/v1/teams/"+name+"/activity", &activity)
	return activity, err
}

// ── Connectors ──────────────────────────────────────────────────────────────

func (c *Client) ConnectorList() ([]map[string]interface{}, error) {
	var connectors []map[string]interface{}
	err := c.GetJSON("/api/v1/connectors", &connectors)
	return connectors, err
}

func (c *Client) ConnectorActivate(name string) error {
	_, err := c.Post("/api/v1/connectors/"+name+"/activate", nil)
	return err
}

func (c *Client) ConnectorDeactivate(name string) error {
	_, err := c.Post("/api/v1/connectors/"+name+"/deactivate", nil)
	return err
}

func (c *Client) ConnectorStatus(name string) (map[string]interface{}, error) {
	var status map[string]interface{}
	err := c.GetJSON("/api/v1/connectors/"+name+"/status", &status)
	return status, err
}

// ── Context / Constraints ────────────────────────────────────────────────────

// ContextPushRequest is the body for a constraint push operation.
type ContextPushRequest struct {
	Constraints interface{} `json:"constraints"`
	Reason      string      `json:"reason"`
	Severity    string      `json:"severity,omitempty"`
}

func (c *Client) ContextPush(agent string, req ContextPushRequest) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/agents/"+agent+"/context/push", req, &result)
	return result, err
}

func (c *Client) ContextStatus(agent string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/v1/agents/"+agent+"/context/status", &result)
	return result, err
}

// ── Intake ──────────────────────────────────────────────────────────────────

func (c *Client) IntakeItems(connector string) ([]map[string]interface{}, error) {
	path := "/api/v1/intake/items"
	if connector != "" {
		path += "?connector=" + url.QueryEscape(connector)
	}
	var items []map[string]interface{}
	err := c.GetJSON(path, &items)
	return items, err
}

func (c *Client) IntakeStats() (map[string]interface{}, error) {
	var stats map[string]interface{}
	err := c.GetJSON("/api/v1/intake/stats", &stats)
	return stats, err
}

// ── Missions ─────────────────────────────────────────────────────────────

func (c *Client) MissionCreate(yamlData []byte) (map[string]interface{}, error) {
	req, err := http.NewRequest("POST", c.BaseURL+"/api/v1/missions", bytes.NewReader(yamlData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-yaml")
	if c.Token != "" {
		req.Header.Set("X-Agency-Token", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 400 {
		if msg, ok := result["error"].(string); ok {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	return result, nil
}

func (c *Client) MissionList() ([]map[string]interface{}, error) {
	var items []map[string]interface{}
	err := c.GetJSON("/api/v1/missions", &items)
	return items, err
}

func (c *Client) MissionShow(name string) (map[string]interface{}, error) {
	var info map[string]interface{}
	err := c.GetJSON("/api/v1/missions/"+name, &info)
	return info, err
}

func (c *Client) MissionUpdate(name string, yamlData []byte) (map[string]interface{}, error) {
	req, err := http.NewRequest("PUT", c.BaseURL+"/api/v1/missions/"+name, bytes.NewReader(yamlData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-yaml")
	if c.Token != "" {
		req.Header.Set("X-Agency-Token", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var errResult map[string]string
		json.Unmarshal(body, &errResult)
		if msg := errResult["error"]; msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	return result, nil
}

func (c *Client) MissionDelete(name string) error {
	_, err := c.Delete("/api/v1/missions/" + name)
	return err
}

func (c *Client) MissionAssign(name, target, targetType string) error {
	body := map[string]string{"target": target, "type": targetType}
	_, err := c.Post("/api/v1/missions/"+name+"/assign", body)
	return err
}

func (c *Client) MissionPause(name string) error {
	_, err := c.Post("/api/v1/missions/"+name+"/pause", nil)
	return err
}

func (c *Client) MissionResume(name string) error {
	_, err := c.Post("/api/v1/missions/"+name+"/resume", nil)
	return err
}

func (c *Client) MissionComplete(name string) error {
	_, err := c.Post("/api/v1/missions/"+name+"/complete", nil)
	return err
}

func (c *Client) MissionHistory(name string) ([]map[string]interface{}, error) {
	var items []map[string]interface{}
	err := c.GetJSON("/api/v1/missions/"+name+"/history", &items)
	return items, err
}

// ── Meeseeks ────────────────────────────────────────────────────────────────

func (c *Client) MeeseeksList(parent string) ([]map[string]interface{}, error) {
	path := "/api/v1/meeseeks"
	if parent != "" {
		path += "?parent=" + url.QueryEscape(parent)
	}
	var items []map[string]interface{}
	err := c.GetJSON(path, &items)
	return items, err
}

func (c *Client) MeeseeksShow(id string) (map[string]interface{}, error) {
	var info map[string]interface{}
	err := c.GetJSON("/api/v1/meeseeks/"+id, &info)
	return info, err
}

func (c *Client) MeeseeksKill(id string) error {
	_, err := c.Delete("/api/v1/meeseeks/" + id)
	return err
}

func (c *Client) MeeseeksKillByParent(agent string) (map[string]interface{}, error) {
	var result map[string]interface{}
	path := "/api/v1/meeseeks?parent=" + url.QueryEscape(agent)
	err := c.DeleteJSON(path, &result)
	return result, err
}

// ── Events ──────────────────────────────────────────────────────────────────

func (c *Client) EventList(sourceType, sourceName, eventType string, limit int) ([]map[string]interface{}, error) {
	params := url.Values{}
	if sourceType != "" {
		params.Set("source_type", sourceType)
	}
	if sourceName != "" {
		params.Set("source_name", sourceName)
	}
	if eventType != "" {
		params.Set("event_type", eventType)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/api/v1/events"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var items []map[string]interface{}
	err := c.GetJSON(path, &items)
	return items, err
}

func (c *Client) EventShow(id string) (map[string]interface{}, error) {
	var event map[string]interface{}
	err := c.GetJSON("/api/v1/events/"+id, &event)
	return event, err
}

func (c *Client) SubscriptionList() ([]map[string]interface{}, error) {
	var subs []map[string]interface{}
	err := c.GetJSON("/api/v1/subscriptions", &subs)
	return subs, err
}

// ── Webhooks ────────────────────────────────────────────────────────────────

func (c *Client) WebhookCreate(name, eventType string) (map[string]interface{}, error) {
	body := map[string]string{"name": name, "event_type": eventType}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/webhooks", body, &result)
	return result, err
}

func (c *Client) WebhookList() ([]map[string]interface{}, error) {
	var items []map[string]interface{}
	err := c.GetJSON("/api/v1/webhooks", &items)
	return items, err
}

func (c *Client) WebhookShow(name string) (map[string]interface{}, error) {
	var wh map[string]interface{}
	err := c.GetJSON("/api/v1/webhooks/"+name, &wh)
	return wh, err
}

func (c *Client) WebhookDelete(name string) error {
	_, err := c.Delete("/api/v1/webhooks/" + name)
	return err
}

func (c *Client) WebhookRotateSecret(name string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/webhooks/"+name+"/rotate-secret", nil, &result)
	return result, err
}

// ── Notifications ────────────────────────────────────────────────────────────

func (c *Client) NotificationList() ([]map[string]interface{}, error) {
	var items []map[string]interface{}
	err := c.GetJSON("/api/v1/notifications", &items)
	return items, err
}

func (c *Client) NotificationAdd(name, notifType, url string, notifEvents []string, headers map[string]string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"name": name,
		"url":  url,
	}
	if notifType != "" {
		body["type"] = notifType
	}
	if len(notifEvents) > 0 {
		body["events"] = notifEvents
	}
	if len(headers) > 0 {
		body["headers"] = headers
	}
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/notifications", body, &result)
	return result, err
}

func (c *Client) NotificationRemove(name string) error {
	_, err := c.Delete("/api/v1/notifications/" + name)
	return err
}

func (c *Client) NotificationTest(name string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/notifications/"+name+"/test", nil, &result)
	return result, err
}

// ── Credential store ────────────────────────────────────────────────────────

func (c *Client) CredentialSet(body map[string]interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/credentials", body, &result)
	return result, err
}

func (c *Client) CredentialList(params url.Values) ([]map[string]interface{}, error) {
	path := "/api/v1/credentials"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var items []map[string]interface{}
	err := c.GetJSON(path, &items)
	return items, err
}

func (c *Client) CredentialShow(name string, showValue bool) (map[string]interface{}, error) {
	path := "/api/v1/credentials/" + name
	if showValue {
		path += "?show_value=true"
	}
	var result map[string]interface{}
	err := c.GetJSON(path, &result)
	return result, err
}

func (c *Client) CredentialDelete(name string) error {
	_, err := c.Delete("/api/v1/credentials/" + name)
	return err
}

func (c *Client) CredentialRotate(name, value string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/credentials/"+name+"/rotate", map[string]interface{}{"value": value}, &result)
	return result, err
}

func (c *Client) CredentialTest(name string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/credentials/"+name+"/test", nil, &result)
	return result, err
}

func (c *Client) CredentialGroupCreate(body map[string]interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.PostJSON("/api/v1/credentials/groups", body, &result)
	return result, err
}


