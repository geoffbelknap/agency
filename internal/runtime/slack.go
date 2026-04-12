package runtime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/models"
)

type slackApprovalRecord struct {
	ID                   string    `json:"id"`
	Status               string    `json:"status"`
	Channel              string    `json:"channel"`
	MessageTS            string    `json:"message_ts,omitempty"`
	OperationKind        string    `json:"operation_kind"`
	OperationTarget      string    `json:"operation_target"`
	OperationDescription string    `json:"operation_description,omitempty"`
	CreatedBy            string    `json:"created_by,omitempty"`
	ApprovedBy           string    `json:"approved_by,omitempty"`
	ApprovedAt           time.Time `json:"approved_at,omitempty"`
	CanceledAt           time.Time `json:"canceled_at,omitempty"`
	ConsentToken         string    `json:"consent_token,omitempty"`
	ExpiresAt            time.Time `json:"expires_at,omitempty"`
}

type slackIssuerConfig struct {
	SigningKeyCredref     string
	SigningKeyID          string
	MaxTokenTTLSeconds    int
	EligibleWitnessesGroup string
}

func executeSlackInteractivity(ctx context.Context, manifest *Manifest, instanceDir string, node *RuntimeNode, req AuthorityInvokeRequest) (*AuthorityExecutionResult, error) {
	switch req.Action {
	case "consent_open_approval_card":
		return slackOpenApproval(ctx, manifest, instanceDir, node, req)
	case "consent_poll_approval":
		return slackPollApproval(instanceDir, node, req)
	case "consent_cancel_approval":
		return slackCancelApproval(ctx, manifest, instanceDir, node, req)
	default:
		return executeHTTPJSON(ctx, manifest, node, req)
	}
}

func HandleAuthorityEvent(ctx context.Context, manifest *Manifest, instanceDir string, node *RuntimeNode, eventType string, event *models.Event) (any, error) {
	if node == nil || node.Executor == nil {
		return nil, fmt.Errorf("runtime node is not executable")
	}
	switch node.Executor.Kind {
	case "slack_interactivity":
		return handleSlackInteractivityEvent(ctx, manifest, instanceDir, node, eventType, event)
	default:
		return nil, fmt.Errorf("executor kind %q does not accept runtime events", node.Executor.Kind)
	}
}

func slackOpenApproval(ctx context.Context, manifest *Manifest, instanceDir string, node *RuntimeNode, req AuthorityInvokeRequest) (*AuthorityExecutionResult, error) {
	cfg, err := slackIssuerSettings(node)
	if err != nil {
		return nil, err
	}
	input := inputMap(req.Input)
	channel := strings.TrimSpace(stringValue(input["channel"]))
	operationKind := strings.TrimSpace(stringValue(input["operation_kind"]))
	operationTarget := strings.TrimSpace(stringValue(input["operation_target"]))
	if channel == "" || operationKind == "" || operationTarget == "" {
		return nil, fmt.Errorf("channel, operation_kind, and operation_target are required")
	}

	record := slackApprovalRecord{
		ID:                   newSlackApprovalID(),
		Status:               "pending",
		Channel:              channel,
		OperationKind:        operationKind,
		OperationTarget:      operationTarget,
		OperationDescription: strings.TrimSpace(stringValue(input["operation_description"])),
		CreatedBy:            req.Subject,
	}
	messageBody := map[string]any{
		"channel": channel,
		"text":    fmt.Sprintf("Approval requested: %s", record.OperationDescription),
		"blocks":  slackApprovalBlocks(record),
	}
	executed, err := slackPostJSON(ctx, manifest, node, node.Executor.Actions[req.Action], messageBody)
	if err != nil {
		return nil, err
	}
	if bodyMap, ok := executed.Body.(map[string]any); ok {
		record.MessageTS = strings.TrimSpace(stringValue(bodyMap["ts"]))
	}
	if err := saveSlackApproval(instanceDir, node.NodeID, record); err != nil {
		return nil, err
	}
	return &AuthorityExecutionResult{
		StatusCode: http.StatusOK,
		Body: map[string]any{
			"pending_approval_id": record.ID,
			"posted_message_ts":   record.MessageTS,
			"issuer_group":        cfg.EligibleWitnessesGroup,
		},
	}, nil
}

func slackPollApproval(instanceDir string, node *RuntimeNode, req AuthorityInvokeRequest) (*AuthorityExecutionResult, error) {
	id := strings.TrimSpace(stringValue(inputMap(req.Input)["pending_approval_id"]))
	if id == "" {
		return nil, fmt.Errorf("pending_approval_id is required")
	}
	record, err := loadSlackApproval(instanceDir, node.NodeID, id)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"status":              record.Status,
		"consent_token":       record.ConsentToken,
		"witnesses_collected": []string{},
	}
	if record.ApprovedBy != "" {
		body["witnesses_collected"] = []string{record.ApprovedBy}
	}
	if !record.ExpiresAt.IsZero() {
		body["expires_at"] = record.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return &AuthorityExecutionResult{StatusCode: http.StatusOK, Body: body}, nil
}

func slackCancelApproval(ctx context.Context, manifest *Manifest, instanceDir string, node *RuntimeNode, req AuthorityInvokeRequest) (*AuthorityExecutionResult, error) {
	id := strings.TrimSpace(stringValue(inputMap(req.Input)["pending_approval_id"]))
	if id == "" {
		return nil, fmt.Errorf("pending_approval_id is required")
	}
	record, err := loadSlackApproval(instanceDir, node.NodeID, id)
	if err != nil {
		return nil, err
	}
	record.Status = "canceled"
	record.CanceledAt = time.Now().UTC()
	if action, ok := node.Executor.Actions[req.Action]; ok && record.Channel != "" && record.MessageTS != "" {
		_, _ = slackPostJSON(ctx, manifest, node, action, map[string]any{
			"channel":  record.Channel,
			"ts":       record.MessageTS,
			"text":     "Approval request canceled.",
			"blocks":   []map[string]any{},
		})
	}
	if err := saveSlackApproval(instanceDir, node.NodeID, record); err != nil {
		return nil, err
	}
	return &AuthorityExecutionResult{StatusCode: http.StatusOK, Body: map[string]any{"status": record.Status}}, nil
}

func handleSlackInteractivityEvent(ctx context.Context, manifest *Manifest, instanceDir string, node *RuntimeNode, eventType string, event *models.Event) (any, error) {
	if eventType != "approval_action" {
		return nil, fmt.Errorf("unsupported slack runtime event %q", eventType)
	}
	actionID := strings.TrimSpace(stringValue(event.Data["action_id"]))
	var pendingID string
	if actions, ok := event.Data["actions"].([]any); ok && len(actions) > 0 {
		if first, ok := actions[0].(map[string]any); ok {
			pendingID = strings.TrimSpace(stringValue(first["value"]))
			if actionID == "" {
				actionID = strings.TrimSpace(stringValue(first["action_id"]))
			}
		}
	}
	if pendingID == "" {
		return nil, fmt.Errorf("approval action missing pending approval id")
	}
	record, err := loadSlackApproval(instanceDir, node.NodeID, pendingID)
	if err != nil {
		return nil, err
	}
	switch actionID {
	case "consent_approve":
		tokenValue, expiresAt, approvedBy, err := mintSlackConsentToken(manifest, node, event, record)
		if err != nil {
			return nil, err
		}
		record.Status = "approved"
		record.ApprovedBy = approvedBy
		record.ApprovedAt = time.Now().UTC()
		record.ConsentToken = tokenValue
		record.ExpiresAt = expiresAt
		if action, ok := node.Executor.Actions["consent_cancel_approval"]; ok && record.Channel != "" && record.MessageTS != "" {
			_, _ = slackPostJSON(ctx, manifest, node, action, map[string]any{
				"channel": record.Channel,
				"ts":      record.MessageTS,
				"text":    "Approval granted.",
				"blocks":  []map[string]any{},
			})
		}
	case "consent_reject":
		record.Status = "rejected"
		record.CanceledAt = time.Now().UTC()
	default:
		return nil, fmt.Errorf("unsupported slack approval action %q", actionID)
	}
	if err := saveSlackApproval(instanceDir, node.NodeID, record); err != nil {
		return nil, err
	}
	return map[string]any{"pending_approval_id": record.ID, "status": record.Status}, nil
}

func mintSlackConsentToken(manifest *Manifest, node *RuntimeNode, event *models.Event, record slackApprovalRecord) (string, time.Time, string, error) {
	cfg, err := slackIssuerSettings(node)
	if err != nil {
		return "", time.Time{}, "", err
	}
	privateKey, err := resolveSigningPrivateKey(cfg.SigningKeyCredref)
	if err != nil {
		return "", time.Time{}, "", err
	}
	approvedBy := slackUserID(event)
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(cfg.MaxTokenTTLSeconds) * time.Second)
	token := agencyconsent.Token{
		Version:         1,
		DeploymentID:    manifest.Source.ConsentDeploymentID,
		OperationKind:   record.OperationKind,
		OperationTarget: []byte(record.OperationTarget),
		Issuer:          node.Package.Name,
		Witnesses:       []string{approvedBy},
		IssuedAt:        now.UnixMilli(),
		ExpiresAt:       expiresAt.UnixMilli(),
		Nonce:           randomNonce(16),
		SigningKeyID:    cfg.SigningKeyID,
	}
	raw, err := token.MarshalCanonical()
	if err != nil {
		return "", time.Time{}, "", err
	}
	encoded, err := agencyconsent.EncodeSignedToken(agencyconsent.SignedToken{
		Token:     token,
		Signature: ed25519.Sign(privateKey, raw),
	})
	if err != nil {
		return "", time.Time{}, "", err
	}
	return encoded, expiresAt, approvedBy, nil
}

func slackIssuerSettings(node *RuntimeNode) (slackIssuerConfig, error) {
	if node == nil {
		return slackIssuerConfig{}, fmt.Errorf("runtime node is required")
	}
	if enabled, _ := node.Settings["consent_issuer"].(bool); !enabled {
		return slackIssuerConfig{}, fmt.Errorf("consent issuer is not enabled")
	}
	raw, ok := node.Settings["consent_issuer_config"].(map[string]any)
	if !ok {
		return slackIssuerConfig{}, fmt.Errorf("consent_issuer_config is required")
	}
	cfg := slackIssuerConfig{
		SigningKeyCredref:      strings.TrimSpace(stringValue(raw["signing_key_credref"])),
		SigningKeyID:           strings.TrimSpace(stringValue(raw["signing_key_id"])),
		EligibleWitnessesGroup: strings.TrimSpace(stringValue(raw["eligible_witnesses_group"])),
	}
	switch v := raw["max_token_ttl_seconds"].(type) {
	case int:
		cfg.MaxTokenTTLSeconds = v
	case int64:
		cfg.MaxTokenTTLSeconds = int(v)
	case float64:
		cfg.MaxTokenTTLSeconds = int(v)
	}
	if cfg.SigningKeyCredref == "" || cfg.SigningKeyID == "" {
		return slackIssuerConfig{}, fmt.Errorf("consent issuer signing key configuration is required")
	}
	if cfg.MaxTokenTTLSeconds <= 0 {
		cfg.MaxTokenTTLSeconds = 900
	}
	return cfg, nil
}

func resolveSigningPrivateKey(credref string) (ed25519.PrivateKey, error) {
	cfgHome := os.Getenv("AGENCY_HOME")
	if strings.TrimSpace(cfgHome) == "" {
		return nil, fmt.Errorf("AGENCY_HOME is required")
	}
	secret, err := resolveCredrefValue(cfgHome, credref)
	if err != nil {
		return nil, err
	}
	for _, decode := range []func(string) ([]byte, error){
		base64.RawURLEncoding.DecodeString,
		base64.StdEncoding.DecodeString,
	} {
		raw, err := decode(strings.TrimSpace(secret))
		if err == nil && len(raw) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(raw), nil
		}
	}
	return nil, fmt.Errorf("invalid Ed25519 private key encoding")
}

func slackPostJSON(ctx context.Context, manifest *Manifest, node *RuntimeNode, action RuntimeHTTPAction, payload map[string]any) (*AuthorityExecutionResult, error) {
	baseURL := strings.TrimRight(node.Executor.BaseURL, "/")
	method := action.Method
	if strings.TrimSpace(method) == "" {
		method = http.MethodPost
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, baseURL+action.Path, mustJSONReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if node.Executor.Auth != nil {
		headerValue, err := resolveExecutorAuthHeader(ctx, manifest, node.Executor.Auth)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set(node.Executor.Auth.Header, headerValue)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	result := &AuthorityExecutionResult{StatusCode: resp.StatusCode, RawBody: string(rawBody)}
	var body any
	if len(rawBody) > 0 && json.Unmarshal(rawBody, &body) == nil {
		result.Body = body
	}
	return result, nil
}

func saveSlackApproval(instanceDir, nodeID string, record slackApprovalRecord) error {
	path := slackApprovalPath(instanceDir, nodeID, record.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadSlackApproval(instanceDir, nodeID, pendingID string) (slackApprovalRecord, error) {
	path := slackApprovalPath(instanceDir, nodeID, pendingID)
	data, err := os.ReadFile(path)
	if err != nil {
		return slackApprovalRecord{}, err
	}
	var record slackApprovalRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return slackApprovalRecord{}, err
	}
	return record, nil
}

func slackApprovalPath(instanceDir, nodeID, pendingID string) string {
	return filepath.Join(instanceDir, "runtime", "state", nodeID, pendingID+".json")
}

func newSlackApprovalID() string {
	return "approval_" + base64.RawURLEncoding.EncodeToString(randomNonce(8))
}

func randomNonce(size int) []byte {
	buf := make([]byte, size)
	_, _ = rand.Read(buf)
	return buf
}

func slackApprovalBlocks(record slackApprovalRecord) []map[string]any {
	return []map[string]any{
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": record.OperationDescription,
			},
		},
		{
			"type": "actions",
			"elements": []map[string]any{
				{
					"type":     "button",
					"action_id": "consent_approve",
					"text":     map[string]any{"type": "plain_text", "text": "Approve"},
					"style":    "primary",
					"value":    record.ID,
				},
				{
					"type":     "button",
					"action_id": "consent_reject",
					"text":     map[string]any{"type": "plain_text", "text": "Reject"},
					"style":    "danger",
					"value":    record.ID,
				},
			},
		},
	}
}

func slackUserID(event *models.Event) string {
	if event == nil {
		return ""
	}
	if user, ok := event.Data["user"].(map[string]any); ok {
		return strings.TrimSpace(stringValue(user["id"]))
	}
	return ""
}

func resolveCredrefValue(home, name string) (string, error) {
	bindingName := strings.TrimPrefix(strings.TrimSpace(name), "credref:")
	cfgPath := filepath.Join(home, "credentials", "store.enc")
	keyPath := filepath.Join(home, "credentials", ".key")
	backend, err := credstore.NewFileBackend(cfgPath, keyPath)
	if err != nil {
		return "", err
	}
	value, _, err := backend.Get(bindingName)
	if err != nil {
		return "", err
	}
	return value, nil
}

func mustJSONReader(payload map[string]any) io.Reader {
	raw, _ := json.Marshal(payload)
	return bytes.NewReader(raw)
}
