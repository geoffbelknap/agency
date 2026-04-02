// Package ws implements the WebSocket hub for real-time event broadcasting.
//
// The hub maintains a set of connected clients and fans out events from
// internal sources (comms messages, agent status changes, infrastructure
// health) to all subscribed WebSocket connections.
//
// Wire protocol: all frames are JSON with a common envelope:
//
//	{"v":1, "type":"event_type", "timestamp":"...", ...payload}
//
// See docs/specs/platform/gateway-api.md Part 4 for the full protocol spec.
package ws

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
)

// ── Event envelope ──────────────────────────────────────────────────────────

// Event is the common envelope for all WebSocket messages (server → client).
type Event struct {
	V         int    `json:"v"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`

	// ack
	Channels []string          `json:"channels,omitempty"`
	Unreads  map[string]int    `json:"unreads,omitempty"`

	// message
	Channel string                 `json:"channel,omitempty"`
	Message map[string]interface{} `json:"message,omitempty"`

	// agent_status
	Agent    string `json:"agent,omitempty"`
	Status   string `json:"status,omitempty"`
	Previous string `json:"previous,omitempty"`

	// infra_status
	Component string `json:"component,omitempty"`
	State     string `json:"state,omitempty"`
	Health    string `json:"health,omitempty"`

	// phase
	Phase     int    `json:"phase,omitempty"`
	PhaseName string `json:"name,omitempty"`

	// halt
	HaltType  string `json:"halt_type,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Initiator string `json:"initiator,omitempty"`

	// task_complete
	TaskID     string `json:"task_id,omitempty"`
	Turns      int    `json:"turns,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`

	// approval_request
	ApprovalID string `json:"approval_id,omitempty"`
	Content    string `json:"content,omitempty"`

	// deploy_progress
	DeploymentID string `json:"deployment_id,omitempty"`

	// agent_signal_*
	Data map[string]interface{} `json:"data,omitempty"`
}

// ── Client subscription ─────────────────────────────────────────────────────

// Subscription controls which events a client receives.
// By default all events are delivered (empty subscription = everything).
type Subscription struct {
	Channels []string `json:"channels,omitempty"`
	Agents   []string `json:"agents,omitempty"`
	Infra    *bool    `json:"infra,omitempty"`
}

// matches returns true if the event passes the subscription filter.
// An empty subscription matches everything.
func (s *Subscription) matches(e *Event) bool {
	if s == nil {
		return true
	}

	switch e.Type {
	case "ack", "pong":
		return true // always deliver control messages

	case "message":
		if len(s.Channels) == 0 {
			return true
		}
		for _, ch := range s.Channels {
			if ch == e.Channel {
				return true
			}
		}
		return false

	case "agent_status", "phase", "halt", "task_complete", "approval_request":
		if len(s.Agents) == 0 {
			return true
		}
		for _, a := range s.Agents {
			if a == e.Agent {
				return true
			}
		}
		return false

	case "infra_status":
		if s.Infra == nil {
			return true
		}
		return *s.Infra

	case "deploy_progress":
		return true

	default:
		// Route agent_signal_* events through agent subscription filter.
		// Unknown event types without agent_signal_ prefix are delivered to all.
		if strings.HasPrefix(e.Type, "agent_signal_") {
			if len(s.Agents) == 0 {
				return true
			}
			for _, a := range s.Agents {
				if a == e.Agent {
					return true
				}
			}
			return false
		}
		return true
	}
}

// ── Client ──────────────────────────────────────────────────────────────────

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingInterval   = 30 * time.Second
	maxMessageSize = 4096
	sendBufSize    = 256
)

// Client represents a single WebSocket connection to the hub.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	log  *log.Logger

	mu           sync.Mutex
	subscription *Subscription
}

// readPump reads messages from the WebSocket connection and processes
// client → server events (subscribe, ping).
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.log.Warn("ws read error", "err", err)
			}
			return
		}

		var msg struct {
			Type     string   `json:"type"`
			Channels []string `json:"channels"`
			Agents   []string `json:"agents"`
			Infra    *bool    `json:"infra"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "subscribe":
			c.mu.Lock()
			c.subscription = &Subscription{
				Channels: msg.Channels,
				Agents:   msg.Agents,
				Infra:    msg.Infra,
			}
			c.mu.Unlock()

		case "ping":
			pong := Event{
				V:         1,
				Type:      "pong",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			data, _ := json.Marshal(pong)
			select {
			case c.send <- data:
			default:
			}
		}
	}
}

// writePump pumps messages from the send channel to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ── Hub ─────────────────────────────────────────────────────────────────────

// Hub maintains the set of active clients and broadcasts events to them.
// EventPublishFunc is called when a channel message arrives via comms relay,
// allowing the event bus to receive channel events.
type EventPublishFunc func(channel, messageID, content, author string)

type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan Event
	log        *log.Logger

	mu                   sync.RWMutex
	eventPublisher       EventPublishFunc
	agentSignalPublisher AgentSignalPublishFunc
	taskCompleteHandler  TaskCompleteFunc
}

// NewHub creates a new Hub and starts its run loop.
func NewHub(logger *log.Logger) *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Event, 256),
		log:        logger,
	}
	go h.run()
	return h
}

// run is the main hub loop — serializes register/unregister/broadcast.
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.log.Info("ws client connected", "clients", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.log.Info("ws client disconnected", "clients", len(h.clients))

		case event := <-h.broadcast:
			data, err := json.Marshal(event)
			if err != nil {
				h.log.Warn("ws marshal error", "err", err)
				continue
			}

			// Collect slow/full clients under RLock, then batch-remove under
			// a single write Lock.  Upgrading the lock inside the iteration
			// loop risks a double-close panic when another goroutine concurrently
			// modifies h.clients during the lock gap.
			var toDrop []*Client
			h.mu.RLock()
			for client := range h.clients {
				client.mu.Lock()
				sub := client.subscription
				client.mu.Unlock()

				if !sub.matches(&event) {
					continue
				}

				select {
				case client.send <- data:
				default:
					// Client send buffer full — mark for removal.
					toDrop = append(toDrop, client)
				}
			}
			h.mu.RUnlock()

			if len(toDrop) > 0 {
				h.mu.Lock()
				for _, c := range toDrop {
					if _, ok := h.clients[c]; ok {
						delete(h.clients, c)
						close(c.send)
					}
				}
				h.mu.Unlock()
			}
		}
	}
}

// Broadcast sends an event to all subscribed clients.
// If the broadcast channel is full the event is dropped with a warning rather
// than blocking the caller (which is often the comms relay goroutine).
func (h *Hub) Broadcast(e Event) {
	if e.V == 0 {
		e.V = 1
	}
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	select {
	case h.broadcast <- e:
	default:
		h.log.Warn("broadcast channel full, dropping event", "type", e.Type)
	}
}

// BroadcastAgentSignal broadcasts an agent signal event to subscribed clients.
// If the broadcast channel is full the event is dropped with a warning rather
// than blocking the caller.
func (h *Hub) BroadcastAgentSignal(agent, eventType string, data map[string]interface{}) {
	event := Event{
		V:         1,
		Type:      eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Agent:     agent,
		Data:      data,
	}
	select {
	case h.broadcast <- event:
	default:
		h.log.Warn("broadcast channel full, dropping event", "type", eventType, "agent", agent)
	}

	// Trigger task completion handler (e.g. success criteria evaluation)
	if eventType == "agent_signal_task_complete" {
		h.mu.RLock()
		fn := h.taskCompleteHandler
		h.mu.RUnlock()
		if fn != nil {
			go fn(agent, data)
		}
	}
}

// SetEventPublisher sets a callback that is invoked when the comms relay
// receives a channel message, allowing integration with the event bus.
func (h *Hub) SetEventPublisher(fn EventPublishFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.eventPublisher = fn
}

// PublishChannelEvent calls the registered event publisher if one is set.
func (h *Hub) PublishChannelEvent(channel, messageID, content, author string) {
	h.mu.RLock()
	fn := h.eventPublisher
	h.mu.RUnlock()
	if fn != nil {
		fn(channel, messageID, content, author)
	}
}

// AgentSignalPublishFunc is called when the comms relay receives an agent
// signal that should be promoted to a platform event.
type AgentSignalPublishFunc func(agent, signalType string, data map[string]interface{})

// SetAgentSignalPublisher sets a callback invoked when the comms relay
// receives an agent signal eligible for event bus promotion.
func (h *Hub) SetAgentSignalPublisher(fn AgentSignalPublishFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agentSignalPublisher = fn
}

// PublishAgentSignal calls the registered signal publisher if one is set.
func (h *Hub) PublishAgentSignal(agent, signalType string, data map[string]interface{}) {
	h.mu.RLock()
	fn := h.agentSignalPublisher
	h.mu.RUnlock()
	if fn != nil {
		fn(agent, signalType, data)
	}
}

// TaskCompleteFunc is called when an agent emits a task_complete signal.
type TaskCompleteFunc func(agent string, data map[string]interface{})

// SetTaskCompleteHandler sets a callback invoked on every task_complete signal.
func (h *Hub) SetTaskCompleteHandler(fn TaskCompleteFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.taskCompleteHandler = fn
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ── WebSocket upgrader ──────────────────────────────────────────────────────

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// The gateway is localhost-bound; origin checking is not needed.
		return true
	},
}

// HandleWebSocket upgrades an HTTP connection to WebSocket and registers
// the client with the hub. It sends an ack event on successful connection.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws upgrade failed", "err", err)
		return
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, sendBufSize),
		log:  h.log,
	}

	h.register <- client

	// Send ack event
	ack := Event{
		V:         1,
		Type:      "ack",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Channels:  []string{},
		Unreads:   map[string]int{},
	}
	data, _ := json.Marshal(ack)
	client.send <- data

	go client.writePump()
	go client.readPump()
}
