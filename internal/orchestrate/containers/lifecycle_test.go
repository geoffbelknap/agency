package containers

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

// mockDockerAPI is a test double for DockerAPI with injectable function fields.
type mockDockerAPI struct {
	createFn func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *specs.Platform, containerName string) (container.CreateResponse, error)
	startFn  func(ctx context.Context, containerID string, options container.StartOptions) error
	stopFn   func(ctx context.Context, containerID string, options container.StopOptions) error
	removeFn func(ctx context.Context, containerID string, options container.RemoveOptions) error
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

	mock := &mockDockerAPI{
		stopFn: func(_ context.Context, _ string, _ container.StopOptions) error {
			stopCalled = true
			return nil
		},
		removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			removeCalled = true
			return nil
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
}

func TestStopAndRemove_IgnoresNotFoundOnStop(t *testing.T) {
	mock := &mockDockerAPI{
		stopFn: func(_ context.Context, _ string, _ container.StopOptions) error {
			return errors.New("no such container: missing")
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
	}

	if err := StopAndRemove(context.Background(), mock, "already-gone", 5); err != nil {
		t.Errorf("expected nil error for not-found remove, got: %v", err)
	}
}
