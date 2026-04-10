package containers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

// mockDockerAPI is a test double for DockerAPI with injectable function fields.
type mockDockerAPI struct {
	createFn  func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *specs.Platform, containerName string) (container.CreateResponse, error)
	inspectFn func(ctx context.Context, containerID string) (container.InspectResponse, error)
	startFn   func(ctx context.Context, containerID string, options container.StartOptions) error
	stopFn    func(ctx context.Context, containerID string, options container.StopOptions) error
	removeFn  func(ctx context.Context, containerID string, options container.RemoveOptions) error
}

func (m *mockDockerAPI) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *specs.Platform, containerName string) (container.CreateResponse, error) {
	if m.createFn != nil {
		return m.createFn(ctx, config, hostConfig, networkingConfig, platform, containerName)
	}
	return container.CreateResponse{ID: "test-id"}, nil
}

func (m *mockDockerAPI) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	if m.startFn != nil {
		return m.startFn(ctx, containerID, options)
	}
	return nil
}

func (m *mockDockerAPI) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	if m.inspectFn != nil {
		return m.inspectFn(ctx, containerID)
	}
	return container.InspectResponse{}, errors.New("no such container: missing")
}

func (m *mockDockerAPI) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	if m.stopFn != nil {
		return m.stopFn(ctx, containerID, options)
	}
	return nil
}

func (m *mockDockerAPI) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, containerID, options)
	}
	return nil
}

func TestCreateAndStart_Success(t *testing.T) {
	const wantID = "abc123"
	mock := &mockDockerAPI{
		createFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *specs.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: wantID}, nil
		},
	}

	id, err := CreateAndStart(context.Background(), mock, "test", &container.Config{}, &container.HostConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != wantID {
		t.Errorf("id = %q, want %q", id, wantID)
	}
}

func TestCreateAndStart_CleansUpOnStartFailure(t *testing.T) {
	const createdID = "cleanup-me"
	startErr := errors.New("start failed")
	removeCalled := false
	removedID := ""

	mock := &mockDockerAPI{
		createFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *specs.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: createdID}, nil
		},
		startFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return startErr
		},
		removeFn: func(_ context.Context, id string, _ container.RemoveOptions) error {
			removeCalled = true
			removedID = id
			return nil
		},
	}

	_, err := CreateAndStart(context.Background(), mock, "test", &container.Config{}, &container.HostConfig{}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !removeCalled {
		t.Error("ContainerRemove was not called after start failure")
	}
	if removedID != createdID {
		t.Errorf("removed container ID = %q, want %q", removedID, createdID)
	}
}

func TestCreateAndStart_ReturnsCreateError(t *testing.T) {
	createErr := errors.New("image not found")
	mock := &mockDockerAPI{
		createFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *specs.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{}, createErr
		},
	}

	_, err := CreateAndStart(context.Background(), mock, "test", &container.Config{}, &container.HostConfig{}, nil)
	if !errors.Is(err, createErr) {
		t.Errorf("error = %v, want %v", err, createErr)
	}
}

func TestStopAndRemove_Success(t *testing.T) {
	stopCalled := false
	removeCalled := false
	inspectCalls := 0

	mock := &mockDockerAPI{
		stopFn: func(_ context.Context, _ string, _ container.StopOptions) error {
			stopCalled = true
			return nil
		},
		removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			removeCalled = true
			return nil
		},
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			inspectCalls++
			return container.InspectResponse{}, errors.New("no such container: missing")
		},
	}

	if err := StopAndRemove(context.Background(), mock, "my-container", 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stopCalled {
		t.Error("ContainerStop was not called")
	}
	if !removeCalled {
		t.Error("ContainerRemove was not called")
	}
	if inspectCalls != 1 {
		t.Errorf("ContainerInspect calls = %d, want 1", inspectCalls)
	}
}

func TestStopAndRemove_IgnoresNotFoundOnStop(t *testing.T) {
	mock := &mockDockerAPI{
		stopFn: func(_ context.Context, _ string, _ container.StopOptions) error {
			return errors.New("no such container: missing")
		},
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{}, errors.New("no such container: missing")
		},
	}

	if err := StopAndRemove(context.Background(), mock, "missing", 5); err != nil {
		t.Errorf("expected nil error for not-found, got: %v", err)
	}
}

func TestStopAndRemove_IgnoresNotFoundOnRemove(t *testing.T) {
	mock := &mockDockerAPI{
		removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			return errors.New("no such container: already-gone")
		},
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{}, errors.New("no such container: missing")
		},
	}

	if err := StopAndRemove(context.Background(), mock, "already-gone", 5); err != nil {
		t.Errorf("expected nil error for not-found remove, got: %v", err)
	}
}

func TestStopAndRemove_RemovesWhenStopReportsAlreadyStopped(t *testing.T) {
	removeCalled := false
	inspectCalls := 0
	mock := &mockDockerAPI{
		stopFn: func(_ context.Context, _ string, _ container.StopOptions) error {
			return errors.New("container is already stopped")
		},
		removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			removeCalled = true
			return nil
		},
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			inspectCalls++
			return container.InspectResponse{}, errors.New("no such container: missing")
		},
	}

	if err := StopAndRemove(context.Background(), mock, "exited-container", 5); err != nil {
		t.Fatalf("expected remove success to clear stop error, got: %v", err)
	}
	if !removeCalled {
		t.Fatal("ContainerRemove was not called")
	}
	if inspectCalls != 1 {
		t.Fatalf("ContainerInspect calls = %d, want 1", inspectCalls)
	}
}

func TestStopAndRemove_ReturnsRemoveErrorAfterStopError(t *testing.T) {
	removeErr := errors.New("remove failed")
	mock := &mockDockerAPI{
		stopFn: func(_ context.Context, _ string, _ container.StopOptions) error {
			return errors.New("container is already stopped")
		},
		removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			return removeErr
		},
	}

	if err := StopAndRemove(context.Background(), mock, "stuck-container", 5); !errors.Is(err, removeErr) {
		t.Fatalf("expected remove error, got: %v", err)
	}
}

func TestStopAndRemove_WaitsUntilContainerIsGone(t *testing.T) {
	inspectCalls := 0
	mock := &mockDockerAPI{
		removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			return nil
		},
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			inspectCalls++
			if inspectCalls < 3 {
				return container.InspectResponse{}, nil
			}
			return container.InspectResponse{}, errors.New("no such container: missing")
		},
	}

	if err := StopAndRemove(context.Background(), mock, "lingering-container", 5); err != nil {
		t.Fatalf("expected wait-for-removal success, got: %v", err)
	}
	if inspectCalls != 3 {
		t.Fatalf("ContainerInspect calls = %d, want 3", inspectCalls)
	}
}

func TestWaitUntilRemoved_TimesOutWhenContainerPersists(t *testing.T) {
	mock := &mockDockerAPI{
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{}, nil
		},
	}

	err := waitUntilRemoved(context.Background(), mock, "stuck-container", 50*time.Millisecond)
	if err == nil || err.Error() != "timed out waiting for container removal" {
		t.Fatalf("expected timeout waiting for removal, got: %v", err)
	}
}

func TestWaitUntilRemoved_ReturnsInspectError(t *testing.T) {
	inspectErr := errors.New("docker connection lost")
	mock := &mockDockerAPI{
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{}, inspectErr
		},
	}

	err := waitUntilRemoved(context.Background(), mock, "broken-container", time.Second)
	if !errors.Is(err, inspectErr) {
		t.Fatalf("expected inspect error, got: %v", err)
	}
}
