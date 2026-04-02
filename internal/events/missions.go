package events

import (
	"github.com/geoffbelknap/agency/internal/models"
)

// BuildMissionSubscriptions converts a mission's triggers into event subscriptions.
func BuildMissionSubscriptions(mission *models.Mission) []*Subscription {
	var subs []*Subscription

	for _, trigger := range mission.Triggers {
		sub := &Subscription{
			SourceType: trigger.Source,
			EventType:  trigger.EventType,
			Destination: Destination{
				Type:   DestAgent,
				Target: mission.AssignedTo,
			},
			Origin:    OriginMission,
			OriginRef: mission.Name,
			Active:    true,
		}

		// Derive source name from trigger fields based on source type.
		switch trigger.Source {
		case "connector":
			sub.SourceName = trigger.Connector
		case "channel":
			sub.SourceName = trigger.Channel
			sub.Match = trigger.Match
		case "schedule":
			sub.SourceName = trigger.Name
		case "webhook":
			sub.SourceName = trigger.Name
		case "platform":
			sub.SourceName = trigger.Name
		}

		subs = append(subs, sub)
	}

	return subs
}

// OnMissionAssigned adds subscriptions for the mission's triggers to the bus.
func OnMissionAssigned(bus *Bus, mission *models.Mission) {
	subs := BuildMissionSubscriptions(mission)
	for _, sub := range subs {
		bus.Subscriptions().Add(sub)
	}
}

// OnMissionPaused deactivates all subscriptions for the mission.
func OnMissionPaused(bus *Bus, missionName string) {
	bus.Subscriptions().DeactivateByOrigin(OriginMission, missionName)
}

// OnMissionResumed reactivates all subscriptions for the mission.
func OnMissionResumed(bus *Bus, missionName string) {
	bus.Subscriptions().ActivateByOrigin(OriginMission, missionName)
}

// OnMissionCompleted removes all subscriptions for the mission.
func OnMissionCompleted(bus *Bus, missionName string) {
	bus.Subscriptions().RemoveByOrigin(OriginMission, missionName)
}
