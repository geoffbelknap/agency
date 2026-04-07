package events

import (
	"testing"

	"log/slog"
	"github.com/geoffbelknap/agency/internal/models"
)

func TestBuildMissionSubscriptions_Connector(t *testing.T) {
	mission := &models.Mission{
		Name:       "review-prs",
		AssignedTo: "reviewer",
		Triggers: []models.MissionTrigger{
			{
				Source:    "connector",
				Connector: "github",
				EventType: "pull_request",
			},
		},
	}

	subs := BuildMissionSubscriptions(mission)
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}

	sub := subs[0]
	if sub.SourceType != "connector" {
		t.Errorf("expected source_type connector, got %s", sub.SourceType)
	}
	if sub.SourceName != "github" {
		t.Errorf("expected source_name github, got %s", sub.SourceName)
	}
	if sub.EventType != "pull_request" {
		t.Errorf("expected event_type pull_request, got %s", sub.EventType)
	}
	if sub.Destination.Type != DestAgent {
		t.Errorf("expected destination type agent, got %s", sub.Destination.Type)
	}
	if sub.Destination.Target != "reviewer" {
		t.Errorf("expected destination target reviewer, got %s", sub.Destination.Target)
	}
	if sub.Origin != OriginMission {
		t.Errorf("expected origin mission, got %s", sub.Origin)
	}
	if sub.OriginRef != "review-prs" {
		t.Errorf("expected origin_ref review-prs, got %s", sub.OriginRef)
	}
}

func TestBuildMissionSubscriptions_ChannelWithMatch(t *testing.T) {
	mission := &models.Mission{
		Name:       "monitor-ops",
		AssignedTo: "ops-bot",
		Triggers: []models.MissionTrigger{
			{
				Source:    "channel",
				Channel:  "ops-alerts",
				EventType: "message",
				Match:    "*deploy*",
			},
		},
	}

	subs := BuildMissionSubscriptions(mission)
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}

	sub := subs[0]
	if sub.SourceName != "ops-alerts" {
		t.Errorf("expected source_name ops-alerts, got %s", sub.SourceName)
	}
	if sub.Match != "*deploy*" {
		t.Errorf("expected match *deploy*, got %s", sub.Match)
	}
}

func TestBuildMissionSubscriptions_Schedule(t *testing.T) {
	mission := &models.Mission{
		Name:       "daily-report",
		AssignedTo: "reporter",
		Triggers: []models.MissionTrigger{
			{
				Source:    "schedule",
				Name:     "daily-9am",
				EventType: "fired",
			},
		},
	}

	subs := BuildMissionSubscriptions(mission)
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}

	if subs[0].SourceName != "daily-9am" {
		t.Errorf("expected source_name daily-9am, got %s", subs[0].SourceName)
	}
}

func TestBuildMissionSubscriptions_MultipleTriggers(t *testing.T) {
	mission := &models.Mission{
		Name:       "multi-trigger",
		AssignedTo: "worker",
		Triggers: []models.MissionTrigger{
			{Source: "connector", Connector: "github", EventType: "push"},
			{Source: "schedule", Name: "hourly", EventType: "fired"},
			{Source: "webhook", Name: "deploy-hook", EventType: "received"},
		},
	}

	subs := BuildMissionSubscriptions(mission)
	if len(subs) != 3 {
		t.Fatalf("expected 3 subscriptions, got %d", len(subs))
	}
}

func TestBuildMissionSubscriptions_NoTriggers(t *testing.T) {
	mission := &models.Mission{
		Name:       "no-triggers",
		AssignedTo: "idle",
	}

	subs := BuildMissionSubscriptions(mission)
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions, got %d", len(subs))
	}
}

func TestOnMissionAssigned(t *testing.T) {
	logger := slog.Default()
	bus := NewBus(logger, nil)

	mission := &models.Mission{
		Name:       "test-mission",
		AssignedTo: "test-agent",
		Triggers: []models.MissionTrigger{
			{Source: "connector", Connector: "github", EventType: "push"},
			{Source: "schedule", Name: "hourly", EventType: "fired"},
		},
	}

	OnMissionAssigned(bus, mission)

	all := bus.Subscriptions().List()
	// 2 system rules + 2 mission subs
	missionSubs := 0
	for _, s := range all {
		if s.Origin == OriginMission && s.OriginRef == "test-mission" {
			missionSubs++
		}
	}
	if missionSubs != 2 {
		t.Errorf("expected 2 mission subscriptions, got %d", missionSubs)
	}
}

func TestOnMissionPaused(t *testing.T) {
	logger := slog.Default()
	bus := NewBus(logger, nil)

	mission := &models.Mission{
		Name:       "pausable",
		AssignedTo: "agent",
		Triggers: []models.MissionTrigger{
			{Source: "connector", Connector: "github", EventType: "push"},
		},
	}

	OnMissionAssigned(bus, mission)
	OnMissionPaused(bus, "pausable")

	for _, s := range bus.Subscriptions().List() {
		if s.Origin == OriginMission && s.OriginRef == "pausable" {
			if s.Active {
				t.Error("expected subscription to be inactive after pause")
			}
		}
	}
}

func TestOnMissionResumed(t *testing.T) {
	logger := slog.Default()
	bus := NewBus(logger, nil)

	mission := &models.Mission{
		Name:       "resumable",
		AssignedTo: "agent",
		Triggers: []models.MissionTrigger{
			{Source: "connector", Connector: "github", EventType: "push"},
		},
	}

	OnMissionAssigned(bus, mission)
	OnMissionPaused(bus, "resumable")
	OnMissionResumed(bus, "resumable")

	for _, s := range bus.Subscriptions().List() {
		if s.Origin == OriginMission && s.OriginRef == "resumable" {
			if !s.Active {
				t.Error("expected subscription to be active after resume")
			}
		}
	}
}

func TestOnMissionCompleted(t *testing.T) {
	logger := slog.Default()
	bus := NewBus(logger, nil)

	mission := &models.Mission{
		Name:       "completable",
		AssignedTo: "agent",
		Triggers: []models.MissionTrigger{
			{Source: "connector", Connector: "github", EventType: "push"},
			{Source: "schedule", Name: "daily", EventType: "fired"},
		},
	}

	OnMissionAssigned(bus, mission)
	OnMissionCompleted(bus, "completable")

	for _, s := range bus.Subscriptions().List() {
		if s.Origin == OriginMission && s.OriginRef == "completable" {
			t.Error("expected no mission subscriptions after completion")
		}
	}
}
