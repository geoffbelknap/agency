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

	"log/slog"
	"github.com/gorilla/websocket"

	"github.com/geoffbelknap/agency/internal/authz"
	"github.com/geoffbelknap/agency/internal/registry"
)

// Auditor writes system-level audit records. *logs.Writer satisfies this
// interface; kept minimal to avoid pulling the logs package into ws.
type Auditor interface {
	WriteSystem(event string, detail map[string]interface{}) error
}

// AppSubprotocol is the WebSocket subprotocol name this hub speaks. Clients
// that authenticate via Sec-WebSocket-Protocol (browsers) must include this
// entry alongside "bearer.<token>". The upgrader echoes only this name back,
// so the bearer entry is never reflected to the client.
const AppSubprotocol = "agency.v1"

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
	log  *slog.Logger

	// principal is the authenticated identity for this connection, resolved
	// at upgrade time from the bearer token. May be nil if the bearer token
	// validated but the registry could not resolve it to a principal
	// (backward-compatible transitional state).
	principal *registry.Principal

	// scope captures what events this client is authorized to observe.
	// Computed once at connect time from principal + registry. Events are
	// filtered against this scope before client.subscription preferences
	// are applied. An empty client subscription means "all events within
	// my scope" — never the full bus.
	scope *authz.Scope

	mu           sync.Mutex
	subscription *Subscription
}

// Principal returns the authenticated principal for this connection, or nil
// if none was resolved. Intended for use by scope filtering and audit logic.
func (c *Client) Principal() *registry.Principal {
	return c.principal
}

// allowsEvent chains scope and subscription preference: the scope determines
// what the client is authorized to receive; the subscription further narrows
// delivery by client preference. A missing scope defaults to allow-all for
// backward compatibility; see Scope doc for the nil-principal window.
func (c *Client) allowsEvent(e *Event) bool {
	if !scopeAllowsEvent(c.scope, e) {
		return false
	}
	c.mu.Lock()
	sub := c.subscription
	c.mu.Unlock()
	return sub.matches(e)
}

// scopeAllowsEvent consults the authz scope for an event. Events that are
// not keyed to an agent or channel (e.g. "ack", "pong") are always allowed
// by scope — subscription preference still applies.
func scopeAllowsEvent(s *authz.Scope, e *Event) bool {
	if s == nil || s.All {
		return true
	}
	switch e.Type {
	case "ack", "pong":
		return true
	case "message":
		return s.AllowsChannel(e.Channel)
	case "agent_status", "phase", "halt", "task_complete", "approval_request":
		return s.AllowsAgent(e.Agent)
	case "infra_status", "deploy_progress":
		return s.AllowsInfra()
	default:
		// agent_signal_* events are agent-scoped; everything else defaults
		// to agent-scoped if Agent is set, otherwise deny for unknown types
		// to stay fail-closed.
		if strings.HasPrefix(e.Type, "agent_signal_") {
			return s.AllowsAgent(e.Agent)
		}
		if e.Agent != "" {
			return s.AllowsAgent(e.Agent)
		}
		return false
	}
}

// scopeTarget tags which set of the Scope a name should be tested against.
type scopeTarget int

const (
	scopeChannel scopeTarget = iota
	scopeAgent
)

// filterByScope partitions names into those the scope admits and those it
// denies. If the scope is nil or All, every name is allowed.
func filterByScope(names []string, s *authz.Scope, target scopeTarget) (allowed, denied []string) {
	if len(names) == 0 {
		return nil, nil
	}
	if s == nil || s.All {
		return names, nil
	}
	for _, n := range names {
		var ok bool
		switch target {
		case scopeChannel:
			ok = s.AllowsChannel(n)
		case scopeAgent:
			ok = s.AllowsAgent(n)
		}
		if ok {
			allowed = append(allowed, n)
		} else {
			denied = append(denied, n)
		}
	}
	return allowed, denied
}

// auditSubscribeDenial records a client's attempt to subscribe to something
// outside its scope. Silent to the client (T14 information asymmetry), but
// surfaced to operators via the system audit log.
func (c *Client) auditSubscribeDenial(target, name string) {
	auditor := c.hub.auditor()
	if auditor == nil {
		return
	}
	detail := map[string]interface{}{
		"target": target,
		"name":   name,
	}
	if c.principal != nil {
		detail["principal_type"] = c.principal.Type
		detail["principal_name"] = c.principal.Name
		if c.principal.UUID != "" {
			detail["principal_uuid"] = c.principal.UUID
		}
	}
	if err := auditor.WriteSystem("ws_subscribe_denied", detail); err != nil {
		c.log.Warn("ws subscribe denial audit write failed", "err", err)
	}
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
			// Intersect the requested subscription with the client's scope.
			// A client cannot widen its scope by subscribing — the request
			// is a preference within what they're already authorized to see.
			// Any requested entry that exceeds scope is dropped and audited.
			allowedChannels, deniedChannels := filterByScope(msg.Channels, c.scope, scopeChannel)
			allowedAgents, deniedAgents := filterByScope(msg.Agents, c.scope, scopeAgent)
			infra := msg.Infra
			if infra != nil && *infra && c.scope != nil && !c.scope.AllowsInfra() {
				f := false
				infra = &f
				c.auditSubscribeDenial("infra", "")
			}
			for _, name := range deniedChannels {
				c.auditSubscribeDenial("channel", name)
			}
			for _, name := range deniedAgents {
				c.auditSubscribeDenial("agent", name)
			}
			c.mu.Lock()
			c.subscription = &Subscription{
				Channels: allowedChannels,
				Agents:   allowedAgents,
				Infra:    infra,
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
// EventPublishFunc is called when a channel message arrives via the comms bridge,
// allowing the event bus to receive channel events.
type EventPublishFunc func(channel, messageID, content, author string)

type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan Event
	log        *slog.Logger

	mu                   sync.RWMutex
	eventPublisher       EventPublishFunc
	agentSignalPublisher AgentSignalPublishFunc
	taskCompleteHandler  TaskCompleteFunc

	// registry and auditWriter are optional dependencies used for scope
	// resolution (registry) and subscribe-denial audit (auditWriter). Both
	// are set via SetRegistry / SetAuditor after NewHub. When nil, scope
	// resolution falls back to allow-all (see authz.Scope).
	registry    *registry.Registry
	auditWriter Auditor
}

// SetRegistry attaches a registry used to resolve a principal's scope at
// connect time. Safe to call before clients connect; not meant for hot
// swapping.
func (h *Hub) SetRegistry(reg *registry.Registry) {
	h.mu.Lock()
	h.registry = reg
	h.mu.Unlock()
}

// SetAuditor attaches a system-level audit writer used to record subscribe
// attempts that exceed scope.
func (h *Hub) SetAuditor(a Auditor) {
	h.mu.Lock()
	h.auditWriter = a
	h.mu.Unlock()
}

// auditor returns the currently attached audit writer, or nil if none.
func (h *Hub) auditor() Auditor {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.auditWriter
}

// reg returns the currently attached registry, or nil if none.
func (h *Hub) reg() *registry.Registry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.registry
}

// NewHub creates a new Hub and starts its run loop.
func NewHub(logger *slog.Logger) *Hub {
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
				if !client.allowsEvent(&event) {
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
// than blocking the caller (which is often the comms bridge goroutine).
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

// SetEventPublisher sets a callback that is invoked when the comms bridge
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

// AgentSignalPublishFunc is called when the comms bridge receives an agent
// signal that should be promoted to a platform event.
type AgentSignalPublishFunc func(agent, signalType string, data map[string]interface{})

// SetAgentSignalPublisher sets a callback invoked when the comms bridge
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
	// When the client offers Sec-WebSocket-Protocol (browser auth path),
	// echo back ONLY the app protocol name. The gorilla upgrader picks
	// the first match from this list, so any "bearer.<token>" entry the
	// client sent is silently dropped from the response.
	Subprotocols: []string{AppSubprotocol},
}

// HandleWebSocket upgrades an HTTP connection to WebSocket and registers
// the client with the hub. It sends an ack event on successful connection.
//
// principal is the authenticated identity resolved by BearerAuth middleware.
// It may be nil if the middleware validated the token but the registry did
// not produce a Principal (transitional; future scope enforcement will
// require non-nil).
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request, principal *registry.Principal) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws upgrade failed", "err", err)
		return
	}

	// Resolve the client's authorization scope once per connection. The
	// registry may be nil in unit/integration test setups; the resolver
	// falls back to allow-all in that case.
	scope := authz.Resolve(principal, h.reg())

	client := &Client{
		hub:       h,
		conn:      conn,
		send:      make(chan []byte, sendBufSize),
		log:       h.log,
		principal: principal,
		scope:     scope,
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
