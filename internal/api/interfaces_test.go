package api

import (
	"context"
	"testing"
)

type mockSignalSender struct {
	called   bool
	lastName string
	lastSig  string
}

func (m *mockSignalSender) SignalContainer(_ context.Context, name, signal string) error {
	m.called = true
	m.lastName = name
	m.lastSig = signal
	return nil
}

func TestSignalSender_MockImplementation(t *testing.T) {
	var s SignalSender = &mockSignalSender{}
	err := s.SignalContainer(context.Background(), "agent-enforcer", "SIGHUP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mock := s.(*mockSignalSender)
	if !mock.called || mock.lastName != "agent-enforcer" || mock.lastSig != "SIGHUP" {
		t.Errorf("mock not called correctly: %+v", mock)
	}
}
