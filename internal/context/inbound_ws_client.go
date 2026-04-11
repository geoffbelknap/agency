package context

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// InboundWSClient wraps an enforcer-initiated websocket connection accepted by
// the gateway. The gateway writes constraint pushes to the socket and receives
// ack reports back on the same connection.
type InboundWSClient struct {
	agent string
	conn  *websocket.Conn
	log   *slog.Logger

	pushMu sync.Mutex

	mu     sync.RWMutex
	closed bool
	done   chan struct{}
	acks   chan AckReport

	onDisconnect func(agent string)
}

// NewInboundWSClient creates a client around an already-upgraded websocket
// connection and starts the ack read loop.
func NewInboundWSClient(agent string, conn *websocket.Conn, logger *slog.Logger) *InboundWSClient {
	if logger == nil {
		logger = slog.Default()
	}
	c := &InboundWSClient{
		agent: agent,
		conn:  conn,
		log:   logger,
		done:  make(chan struct{}),
		acks:  make(chan AckReport, 8),
	}
	go c.readPump()
	return c
}

// SetDisconnectCallback registers an optional callback that fires when the
// underlying websocket drops.
func (c *InboundWSClient) SetDisconnectCallback(fn func(agent string)) {
	c.onDisconnect = fn
}

func (c *InboundWSClient) readPump() {
	defer func() {
		c.closeInternal()
		if c.onDisconnect != nil {
			c.onDisconnect(c.agent)
		}
	}()

	for {
		var ack AckReport
		if err := c.conn.ReadJSON(&ack); err != nil {
			c.log.Warn("inbound ws client disconnected", "agent", c.agent, "err", err)
			return
		}
		select {
		case c.acks <- ack:
		case <-c.done:
			return
		}
	}
}

// Push sends a constraint change and waits for the matching ack report.
func (c *InboundWSClient) Push(change *ConstraintChange, timeout time.Duration) (*AckReport, error) {
	c.pushMu.Lock()
	defer c.pushMu.Unlock()

	if !c.Connected() {
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

	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := c.conn.WriteJSON(msg); err != nil {
		return nil, fmt.Errorf("ws_client: agent %q: write: %w", c.agent, err)
	}
	c.conn.SetWriteDeadline(time.Time{})

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-c.done:
			return nil, fmt.Errorf("ws_client: agent %q: connection closed", c.agent)
		case <-timer.C:
			return nil, fmt.Errorf("ws_client: agent %q: read ack: timeout", c.agent)
		case ack := <-c.acks:
			if ack.ChangeID == change.ChangeID && ack.Version == change.Version {
				return &ack, nil
			}
		}
	}
}

// Connected reports whether the websocket is still considered live.
func (c *InboundWSClient) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.closed
}

// Done returns a channel that closes when the websocket disconnects.
func (c *InboundWSClient) Done() <-chan struct{} {
	return c.done
}

// Close closes the websocket and marks the client disconnected.
func (c *InboundWSClient) Close() {
	c.closeInternal()
}

func (c *InboundWSClient) closeInternal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	_ = c.conn.Close()
}
