package context

import "time"

// DeliveryClient is the transport abstraction used by the constraint manager.
// It may be backed by an outbound dialer or by an inbound agent-initiated
// websocket connection.
type DeliveryClient interface {
	Push(change *ConstraintChange, timeout time.Duration) (*AckReport, error)
	Connected() bool
	Close()
}
