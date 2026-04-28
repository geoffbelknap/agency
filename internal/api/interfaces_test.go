package api

import (
	"context"
	"testing"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type mockSignalSender struct {
	called   bool
	lastName string
	lastSig  string
}

func (m *mockSignalSender) Signal(_ context.Context, ref runtimecontract.InstanceRef, signal string) error {
	m.called = true
	m.lastName = ref.RuntimeID + ":" + string(ref.Role)
	m.lastSig = signal
	return nil
}

func TestSignalSender_MockImplementation(t *testing.T) {
	var s SignalSender = &mockSignalSender{}
	err := s.Signal(context.Background(), runtimecontract.InstanceRef{RuntimeID: "agent", Role: runtimecontract.RoleEnforcer}, "SIGHUP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mock := s.(*mockSignalSender)
	if !mock.called || mock.lastName != "agent:enforcer" || mock.lastSig != "SIGHUP" {
		t.Errorf("mock not called correctly: %+v", mock)
	}
}
