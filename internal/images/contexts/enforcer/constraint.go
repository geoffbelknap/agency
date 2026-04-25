package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ConstraintState holds the currently active constraint set. It is swapped
// atomically via atomic.Pointer so that readers (the Body REST endpoints)
// never see a partially updated state.
//
// ASK tenet 6: constraint changes are atomic and acknowledged.
type ConstraintState struct {
	Version     int                    `json:"version"`
	Constraints map[string]interface{} `json:"constraints"`
	Hash        string                 `json:"hash"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// ConstraintHandler manages the enforcer side of the constraint delivery
// protocol. It exposes:
//   - GET /ws                   — WebSocket endpoint for gateway push
//   - GET /constraints          — returns current constraint state to Body
//   - POST /constraints/ack     — Body submits hash ack
//
// The gateway connects to /ws and pushes WSPushMessage frames. The handler
// atomically swaps the constraint state and replies with an AckReport.
// The Body runtime polls GET /constraints and posts to /constraints/ack.
type ConstraintHandler struct {
	state atomic.Pointer[ConstraintState]
	audit *AuditLogger
	agent string

	// bodyNotifyURL is the workspace Body hook endpoint, e.g.
	// http://workspace:8090/hooks/constraint-change
	bodyNotifyURL string

	// Track the latest change for Body ack verification.
	mu             sync.RWMutex
	latestChangeID string
	latestVersion  int

	upgrader websocket.Upgrader
}

// NewConstraintHandler creates a constraint handler for the given agent.
func NewConstraintHandler(agent string, audit *AuditLogger, bodyNotifyURL string) *ConstraintHandler {
	ch := &ConstraintHandler{
		agent:         agent,
		audit:         audit,
		bodyNotifyURL: bodyNotifyURL,
		upgrader: websocket.Upgrader{
			// Only the gateway connects; no browser origin checks needed.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	// Initialize with empty state.
	empty := &ConstraintState{
		Constraints: make(map[string]interface{}),
		Hash:        hashConstraints(make(map[string]interface{})),
		UpdatedAt:   time.Now().UTC(),
	}
	ch.state.Store(empty)
	return ch
}

// RegisterRoutes registers constraint endpoints on the given mux.
// These endpoints bypass auth because:
//   - /ws is gateway-to-enforcer on the internal network (mediation boundary)
//   - /constraints and /constraints/ack are enforcer-to-Body on the agent-internal network
func (ch *ConstraintHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws", ch.handleWS)
	mux.HandleFunc("/constraints", ch.handleGetConstraints)
	mux.HandleFunc("/constraints/ack", ch.handleBodyAck)
}

// handleWS upgrades the connection and processes gateway constraint pushes.
// Each push atomically swaps the constraint state and returns an ack.
func (ch *ConstraintHandler) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := ch.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("constraint: ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("constraint: gateway connected via WebSocket")
	ch.serveConn(conn)
}

// ConnectGateway dials the gateway's authenticated websocket endpoint and
// reuses the same push/ack protocol used by the legacy inbound /ws listener.
func (ch *ConstraintHandler) ConnectGateway(gatewayURL, gatewayToken string) {
	if gatewayURL == "" || gatewayToken == "" {
		return
	}

	wsURL := gatewayConstraintWSURL(gatewayURL, ch.agent)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+gatewayToken)

	backoff := time.Second
	for {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if err != nil {
			log.Printf("constraint: gateway ws dial failed: %v", err)
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			continue
		}

		log.Printf("constraint: connected to gateway websocket")
		backoff = time.Second
		ch.serveConn(conn)
		conn.Close()
	}
}

func gatewayConstraintWSURL(gatewayURL, agent string) string {
	switch {
	case strings.HasPrefix(gatewayURL, "https://"):
		return "wss://" + strings.TrimPrefix(gatewayURL, "https://") + "/api/v1/agents/" + agent + "/context/ws"
	case strings.HasPrefix(gatewayURL, "http://"):
		return "ws://" + strings.TrimPrefix(gatewayURL, "http://") + "/api/v1/agents/" + agent + "/context/ws"
	default:
		return "ws://" + strings.TrimPrefix(gatewayURL, "ws://") + "/api/v1/agents/" + agent + "/context/ws"
	}
}

func (ch *ConstraintHandler) serveConn(conn *websocket.Conn) {
	for {
		var push wsPushMessage
		if err := conn.ReadJSON(&push); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("constraint: ws read error: %v", err)
			}
			return
		}
		ack := ch.applyPush(push)
		if err := conn.WriteJSON(ack); err != nil {
			log.Printf("constraint: ws write ack error: %v", err)
			return
		}
	}
}

func (ch *ConstraintHandler) applyPush(push wsPushMessage) ackReport {
	log.Printf("constraint: received push change_id_present=%t version=%d severity_present=%t",
		push.ChangeID != "", push.Version, push.Severity != "")

	computed := hashConstraints(push.Constraints)
	if computed != push.Hash {
		log.Printf("constraint: hash mismatch on push")
		ch.audit.Log(AuditEntry{
			Type:  "CONSTRAINT_HASH_MISMATCH",
			Agent: ch.agent,
		})
		return ackReport{
			Type:     "constraint_ack",
			Agent:    ch.agent,
			ChangeID: push.ChangeID,
			Version:  push.Version,
			Status:   "hash_mismatch",
			BodyHash: computed,
		}
	}

	newState := &ConstraintState{
		Version:     push.Version,
		Constraints: push.Constraints,
		Hash:        push.Hash,
		UpdatedAt:   time.Now().UTC(),
	}
	ch.state.Store(newState)

	ch.mu.Lock()
	ch.latestChangeID = push.ChangeID
	ch.latestVersion = push.Version
	ch.mu.Unlock()

	ch.audit.Log(AuditEntry{
		Type:  "CONSTRAINT_APPLIED",
		Agent: ch.agent,
	})
	go ch.notifyBody(push.ChangeID, push.Version)

	return ackReport{
		Type:     "constraint_ack",
		Agent:    ch.agent,
		ChangeID: push.ChangeID,
		Version:  push.Version,
		Status:   "acked",
		BodyHash: push.Hash,
	}
}

// handleGetConstraints returns the current constraint state to the Body runtime.
func (ch *ConstraintHandler) handleGetConstraints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	state := ch.state.Load()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

// handleBodyAck accepts a hash ack from the Body runtime. The Body proves
// it has correctly received and parsed the constraint set by echoing the hash.
func (ch *ConstraintHandler) handleBodyAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		ChangeID string `json:"change_id"`
		Version  int    `json:"version"`
		Hash     string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	state := ch.state.Load()
	if body.Hash != state.Hash {
		ch.audit.Log(AuditEntry{
			Type:  "CONSTRAINT_BODY_ACK_MISMATCH",
			Agent: ch.agent,
		})
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error":         "hash mismatch",
			"expected_hash": state.Hash,
			"received_hash": body.Hash,
		})
		return
	}

	ch.audit.Log(AuditEntry{
		Type:  "CONSTRAINT_BODY_ACK",
		Agent: ch.agent,
	})

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"ok"}`)
}

// notifyBody sends a POST to the Body workspace hook to inform it that
// constraints have changed. The Body should then GET /constraints.
func (ch *ConstraintHandler) notifyBody(changeID string, version int) {
	if ch.bodyNotifyURL == "" {
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"type":      "constraint_change",
		"change_id": changeID,
		"version":   version,
		"agent":     ch.agent,
	})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(ch.bodyNotifyURL, "application/json",
		bytes.NewReader(payload))
	if err != nil {
		log.Printf("constraint: notify body failed: %v", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("constraint: notify body returned %d", resp.StatusCode)
	}
}

// -- Wire types matching the gateway protocol --

type wsPushMessage struct {
	Type        string                 `json:"type"`
	Agent       string                 `json:"agent"`
	ChangeID    string                 `json:"change_id"`
	Version     int                    `json:"version"`
	Severity    string                 `json:"severity"`
	Constraints map[string]interface{} `json:"constraints"`
	Hash        string                 `json:"hash"`
	Reason      string                 `json:"reason"`
	Timestamp   string                 `json:"timestamp"`
}

type ackReport struct {
	Type     string `json:"type"`
	Agent    string `json:"agent"`
	ChangeID string `json:"change_id"`
	Version  int    `json:"version"`
	Status   string `json:"status"`
	BodyHash string `json:"body_hash,omitempty"`
}

// hashConstraints computes SHA-256 of canonical JSON. Must match the gateway's
// HashConstraints in agency-gateway/internal/context/types.go:
//
//	json.Marshal produces sorted keys and compact encoding.
func hashConstraints(constraints map[string]interface{}) string {
	data, _ := json.Marshal(constraints)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
