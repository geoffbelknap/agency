package events

import (
	"github.com/geoffbelknap/agency/internal/models"
)

// EmitAgentEvent publishes an agent lifecycle event.
func EmitAgentEvent(bus *Bus, eventType, agentName string, data map[string]interface{}) {
	if bus == nil {
		return
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	data["agent"] = agentName
	event := models.NewEvent(models.EventSourcePlatform, "gateway", eventType, data)
	bus.Publish(event)
}

// EmitMissionEvent publishes a mission lifecycle event.
func EmitMissionEvent(bus *Bus, eventType, missionName string, data map[string]interface{}) {
	if bus == nil {
		return
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	data["mission"] = missionName
	event := models.NewEvent(models.EventSourcePlatform, "gateway", eventType, data)
	bus.Publish(event)
}

// EmitCapabilityEvent publishes a capability change event.
func EmitCapabilityEvent(bus *Bus, eventType, agentName, capName string) {
	if bus == nil {
		return
	}
	data := map[string]interface{}{
		"agent":      agentName,
		"capability": capName,
	}
	event := models.NewEvent(models.EventSourcePlatform, "gateway", eventType, data)
	bus.Publish(event)
}

// EmitInfraEvent publishes an infrastructure event.
func EmitInfraEvent(bus *Bus, eventType string, data map[string]interface{}) {
	if bus == nil {
		return
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	event := models.NewEvent(models.EventSourcePlatform, "gateway", eventType, data)
	bus.Publish(event)
}
