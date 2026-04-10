package ws

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"log/slog"
)

const (
	commsReconnectMin  = 1 * time.Second
	commsReconnectMax  = 30 * time.Second
	commsReconnectMult = 2
)

func commsBridgeURL() string {
	port := os.Getenv("AGENCY_GATEWAY_PROXY_PORT")
	if port == "" {
		port = "8202"
	}
	return fmt.Sprintf("ws://localhost:%s/ws?agent=_gateway", port)
}

// promotableSignals are signal types that should be promoted to platform
// events for operator notification delivery. These represent conditions
// where the operator needs to know something went wrong.
var promotableSignals = map[string]bool{
	"error":      true, // LLM failures, budget exhaustion, enforcer unreachable
	"escalation": true, // XPIA detection, constraint violations
	"self_halt":  true, // Agent self-halted
}

func isPromotableSignal(signalType string) bool {
	return promotableSignals[signalType]
}

// StartCommsBridge connects to the comms WebSocket and bridges message events
// into the gateway WebSocket hub and event bus.
func StartCommsBridge(hub *Hub, logger *slog.Logger) {
	go commsBridgeLoop(hub, logger)
}

func commsBridgeLoop(hub *Hub, logger *slog.Logger) {
	backoff := commsReconnectMin

	for {
		err := commsBridgeOnce(hub, logger)
		if err != nil {
			logger.Warn("comms bridge disconnected", "err", err, "reconnect_in", backoff)
		} else {
			// Clean close — connection was healthy; reset backoff so the next
			// reconnect attempt starts at the minimum delay again.
			backoff = commsReconnectMin
			logger.Info("comms bridge connection closed, reconnecting", "reconnect_in", backoff)
		}

		time.Sleep(backoff)
		backoff *= commsReconnectMult
		if backoff > commsReconnectMax {
			backoff = commsReconnectMax
		}
	}
}

func commsBridgeOnce(hub *Hub, logger *slog.Logger) error {
	url := commsBridgeURL()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	logger.Info("comms bridge connected", "url", url)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		// Parse the comms event — could be a message, signal, or other event type.
		var msg map[string]interface{}
		if err := json.Unmarshal(raw, &msg); err != nil {
			logger.Debug("comms bridge: unparseable message", "err", err)
			continue
		}

		msgType, _ := msg["type"].(string)
		if msgType == "" {
			msgType = "message"
		}

		// Agent signals get broadcast directly with full payload.
		if strings.HasPrefix(msgType, "agent_signal_") {
			agent, _ := msg["agent"].(string)
			data, _ := msg["data"].(map[string]interface{})
			hub.BroadcastAgentSignal(agent, msgType, data)

			// Promote operator-alertable signals to platform events.
			// Signal type is encoded in the message type: "agent_signal_error" -> "error"
			signalType := strings.TrimPrefix(msgType, "agent_signal_")
			if isPromotableSignal(signalType) {
				hub.PublishAgentSignal(agent, signalType, data)
			}
			continue
		}

		// Standard message bridge.
		channel, _ := msg["channel"].(string)
		message, _ := msg["message"].(map[string]interface{})
		event := Event{
			V:         1,
			Type:      msgType,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Channel:   channel,
			Message:   message,
		}

		hub.Broadcast(event)

		// Publish to event bus for subscription matching.
		if channel != "" && message != nil {
			content, _ := message["content"].(string)
			author, _ := message["author"].(string)
			messageID, _ := message["id"].(string)
			hub.PublishChannelEvent(channel, messageID, content, author)
		}
	}
}
