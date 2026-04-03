package docker

import (
	"errors"
	"testing"
)

func TestStatus_InitiallyAvailable(t *testing.T) {
	s := NewStatus(&Client{})
	if !s.Available() {
		t.Error("should be available with non-nil client")
	}
}

func TestStatus_InitiallyUnavailable(t *testing.T) {
	s := NewStatus(nil)
	if s.Available() {
		t.Error("should be unavailable with nil client")
	}
}

func TestStatus_DetectsFailure(t *testing.T) {
	s := NewStatus(&Client{})
	s.RecordError(errors.New("Cannot connect to the Docker daemon"))
	if s.Available() {
		t.Error("should be unavailable after Docker error")
	}
}

func TestStatus_ConnectionRefused(t *testing.T) {
	s := NewStatus(&Client{})
	s.RecordError(errors.New("dial tcp: connection refused"))
	if s.Available() {
		t.Error("should be unavailable after connection refused")
	}
}

func TestStatus_NonDockerErrorDoesNotFlip(t *testing.T) {
	s := NewStatus(&Client{})
	s.RecordError(errors.New("container not found"))
	if !s.Available() {
		t.Error("non-Docker errors should not flip availability")
	}
}

func TestStatus_NilErrorDoesNotFlip(t *testing.T) {
	s := NewStatus(&Client{})
	s.RecordError(nil)
	if !s.Available() {
		t.Error("nil error should not flip availability")
	}
}

func TestStatus_RecoveryOnSuccess(t *testing.T) {
	s := NewStatus(nil)
	s.RecordSuccess()
	if !s.Available() {
		t.Error("should recover on success")
	}
}

func TestStatus_ReconnectCallbackFires(t *testing.T) {
	fired := false
	s := NewStatus(nil)
	s.OnReconnect = func() { fired = true }
	s.RecordSuccess()
	if !fired {
		t.Error("OnReconnect should fire on recovery")
	}
}

func TestStatus_ReconnectCallbackDoesNotFireWhenAlreadyAvailable(t *testing.T) {
	fired := false
	s := NewStatus(&Client{})
	s.OnReconnect = func() { fired = true }
	s.RecordSuccess()
	if fired {
		t.Error("OnReconnect should not fire when already available")
	}
}
