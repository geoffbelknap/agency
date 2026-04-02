package events

import (
	"github.com/geoffbelknap/agency/internal/config"
)

// BuildNotificationSubscriptions creates subscriptions from notification configs.
// Each notification config entry creates one subscription per event type in its events list.
func BuildNotificationSubscriptions(configs []config.NotificationConfig) []*Subscription {
	var subs []*Subscription

	for _, nc := range configs {
		destType := DestWebhook
		if nc.Type == "ntfy" {
			destType = DestNtfy
		}

		for _, eventType := range nc.Events {
			sub := &Subscription{
				SourceType: "platform", // notifications watch platform events
				EventType:  eventType,
				Destination: Destination{
					Type:   destType,
					Target: nc.URL,
				},
				Origin:    OriginNotification,
				OriginRef: nc.Name,
				Active:    true,
			}
			subs = append(subs, sub)
		}
	}

	return subs
}
