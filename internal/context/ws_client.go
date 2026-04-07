package context

import (
	"fmt"
	"sync"
	"time"

	"log/slog"
	"github.com/gorilla/websocket"
)

// WSClient is a per-agent outbound WebSocket client. The gateway creates one
// per running agent to push constraint changes to the enforcer sidecar and
// receive ack reports.
//
// ASK tenet 6: constraint changes are atomic and acknowledged. WSClient
// delivers each change and blocks until the enforcer acks (or times out).
// ASK tenet 3: mediation is complete — the enforcer sidecar is the enforcement
// boundary; this client is the gateway side of that channel.
type WSClient struct {
	agent string
	url   string
	log   *slog.Logger

	mu   sync.Mutex
	conn *websocket.Conn

	closed bool
	done   chan struct{}

	reconnectMin  time.Duration
	reconnectMax  time.Duration
	reconnectMult int

	onDisconnect func(agent string)
	onReconnect  func(agent string)
}

// NewWSClient creates a new WSClient with sensible reconnect defaults.
// logger may be nil; a default logger is used in that case.
func NewWSClient(agent, url string, logger *slog.Logger) *WSClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &WSClient{
		agent:         agent,
		url:           url,
		log:           logger,
		done:          make(chan struct{}),
		reconnectMin:  1 * time.Second,
		reconnectMax:  30 * time.Second,
		reconnectMult: 2,
	}
}

// SetCallbacks registers optional callbacks for disconnect and reconnect events.
func (c *WSClient) SetCallbacks(onDisconnect, onReconnect func(agent string)) {
	c.onDisconnect = onDisconnect
	c.onReconnect = onReconnect
}

// Connect performs a single connection attempt to c.url.
// Returns an error if the dial fails or the client is already closed.
func (c *WSClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("ws_client: agent %q: already closed", c.agent)
	}

	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("ws_client: agent %q: dial %s: %w", c.agent, c.url, err)
	}
	c.conn = conn
	c.log.Info("ws_client: connected", "agent", c.agent, "url", c.url)
	return nil
}

// ConnectWithReconnect blocks and reconnects on disconnect using exponential
// backoff. Returns only when Close() is called.
func (c *WSClient) ConnectWithReconnect() {
	backoff := c.reconnectMin

	for {
		// Check if we've been closed before attempting.
		select {
		case <-c.done:
			return
		default:
		}

		if err := c.Connect(); err != nil {
			c.log.Warn("ws_client: connect failed", "agent", c.agent, "err", err, "retry_in", backoff)
		} else {
			backoff = c.reconnectMin // reset on successful connect

			if c.onReconnect != nil {
				c.onReconnect(c.agent)
			}

			// Hold connection open until it drops or we are closed.
			c.readUntilClose()

			if c.onDisconnect != nil {
				c.onDisconnect(c.agent)
			}
		}

		// Wait for backoff interval or until closed.
		select {
		case <-c.done:
			return
		case <-time.After(backoff):
		}

		backoff = time.Duration(int(backoff) * c.reconnectMult)
		if backoff > c.reconnectMax {
			backoff = c.reconnectMax
		}
	}
}

// readUntilClose detects connection loss by sending WebSocket pings and
// waiting for pongs. It blocks until the connection drops or Close() is called,
// then clears c.conn so ConnectWithReconnect can install a fresh one.
//
// Using pings avoids any read/write race with the Push method's synchronous
// ReadJSON call — gorilla only allows one concurrent reader, so we must not
// compete with Push for reads.
func (c *WSClient) readUntilClose() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
	}()

	// pingInterval controls how often we probe the connection.
	const pingInterval = 100 * time.Millisecond

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			// SetWriteDeadline on the ping write.
			conn.SetWriteDeadline(time.Now().Add(pingInterval))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				// Write failed — connection is gone.
				c.log.Warn("ws_client: connection lost (ping)", "agent", c.agent, "err", err)
				return
			}
			conn.SetWriteDeadline(time.Time{})

			// Wait briefly for pong.
			conn.SetReadDeadline(time.Now().Add(pingInterval * 2))
			_, _, err := conn.ReadMessage()
			conn.SetReadDeadline(time.Time{})
			if err != nil {
				if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
					c.log.Warn("ws_client: pong timeout", "agent", c.agent)
					return
				}
				// Normal close or other disconnect.
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					c.log.Warn("ws_client: connection lost", "agent", c.agent, "err", err)
				}
				return
			}
		}
	}
}

// Push sends a WSPushMessage derived from change to the enforcer and waits up
// to timeout for an AckReport response.
//
// The read deadline enforces the timeout. If the deadline fires before the
// enforcer responds, Push returns an error and the caller should escalate
// (e.g. MarkTimeout on the Manager).
func (c *WSClient) Push(change *ConstraintChange, timeout time.Duration) (*AckReport, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("ws_client: agent %q: not connected", c.agent)
	}

	msg := WSPushMessage{
		Type:        "constraint_push",
		Agent:       change.Agent,
		ChangeID:    change.ChangeID,
		Version:     change.Version,
		Severity:    change.Severity.String(),
		Constraints: change.Constraints,
		Hash:        change.Hash,
		Reason:      change.Reason,
		Timestamp:   change.Timestamp.UTC().Format(time.RFC3339),
	}

	if err := conn.WriteJSON(msg); err != nil {
		return nil, fmt.Errorf("ws_client: agent %q: write: %w", c.agent, err)
	}

	conn.SetReadDeadline(time.Now().Add(timeout))
	defer conn.SetReadDeadline(time.Time{}) // clear deadline after read

	var ack AckReport
	if err := conn.ReadJSON(&ack); err != nil {
		return nil, fmt.Errorf("ws_client: agent %q: read ack: %w", c.agent, err)
	}

	return &ack, nil
}

// Connected reports whether a live connection is held.
func (c *WSClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil && !c.closed
}

// Close shuts down the client. Safe to call multiple times.
func (c *WSClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true
	close(c.done)

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}
