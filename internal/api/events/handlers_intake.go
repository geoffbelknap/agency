package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
)

var intakeBaseURL = "http://localhost:8205"

func (h *handler) intakeItems(w http.ResponseWriter, r *http.Request) {
	connector := r.URL.Query().Get("connector")
	path := "/items"
	if connector != "" {
		path += "?connector=" + connector
	}

	out, err := serviceGet(r.Context(), "8205", path)
	if err != nil {
		writeJSON(w, 200, []interface{}{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(out)
}

func (h *handler) intakeStats(w http.ResponseWriter, r *http.Request) {
	out, err := serviceGet(r.Context(), "8205", "/stats")
	if err != nil {
		writeJSON(w, 200, map[string]interface{}{"total": 0, "pending": 0, "completed": 0})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(out)
}

func (h *handler) intakeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	connectorName, _ := payload["connector"].(string)
	if connectorName == "" {
		writeJSON(w, 400, map[string]string{"error": "connector required"})
		return
	}

	webhookURL := intakeBaseURL + "/webhooks/" + url.PathEscape(connectorName)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "intake unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	out, _ := io.ReadAll(resp.Body)

	// Publish connector event to event bus (Task 13: connector source integration)
	if resp.StatusCode < 400 && h.deps.EventBus != nil {
		if payload != nil {
			eventType, _ := payload["event_type"].(string)
			if eventType == "" {
				eventType = "webhook_received"
			}
			event := models.NewEvent(models.EventSourceConnector, connectorName, eventType, payload)
			h.deps.EventBus.Publish(event)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(out)
}

func (h *handler) relayWebhookDeliver(w http.ResponseWriter, r *http.Request) {
	localPath := strings.TrimSpace(r.Header.Get("X-Relay-Webhook-Local-Path"))
	if localPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-Relay-Webhook-Local-Path header is required"})
		return
	}
	if !strings.HasPrefix(localPath, "/webhooks/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid relay webhook path"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	webhookURL := intakeBaseURL + localPath
	if rawQuery := strings.TrimSpace(r.Header.Get("X-Relay-Webhook-Query")); rawQuery != "" {
		if strings.HasPrefix(rawQuery, "?") {
			webhookURL += rawQuery
		} else {
			webhookURL += "?" + rawQuery
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, webhookURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	copyRelayWebhookHeaders(req.Header, r.Header)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "intake unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func copyRelayWebhookHeaders(dst, src http.Header) {
	for k, values := range src {
		canonical := http.CanonicalHeaderKey(k)
		switch canonical {
		case "Authorization", "X-Agency-Token", "X-Agency-Caller", "X-Agency-Via":
			continue
		}
		if strings.HasPrefix(canonical, "X-Relay-Webhook-") {
			continue
		}
		for _, value := range values {
			dst.Add(k, value)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for k, values := range src {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, value := range values {
			dst.Add(k, value)
		}
	}
}

// serviceGet makes a GET request to an infra service via its localhost port.
func serviceGet(ctx context.Context, port, path string) ([]byte, error) {
	url := "http://localhost:" + port + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("service (port %s) returned %d", port, resp.StatusCode)
	}
	return out, nil
}
