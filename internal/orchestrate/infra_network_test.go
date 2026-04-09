package orchestrate

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/network"
)

type mockDockerNetworkAPI struct {
	inspectFn func(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
	createFn  func(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
}

func (m *mockDockerNetworkAPI) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	if m.inspectFn != nil {
		return m.inspectFn(ctx, networkID, options)
	}
	return network.Inspect{}, nil
}

func (m *mockDockerNetworkAPI) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	if m.createFn != nil {
		return m.createFn(ctx, name, options)
	}
	return network.CreateResponse{ID: "created"}, nil
}

func (m *mockDockerNetworkAPI) NetworkRemove(context.Context, string) error {
	return nil
}

func TestEnsureInternalNetworkReadyCreatesAndVerifiesNetwork(t *testing.T) {
	var inspectCalls int
	cli := &mockDockerNetworkAPI{
		inspectFn: func(_ context.Context, _ string, _ network.InspectOptions) (network.Inspect, error) {
			inspectCalls++
			if inspectCalls < 3 {
				return network.Inspect{}, errors.New("network not found")
			}
			return network.Inspect{Name: "agency-agent-internal"}, nil
		},
	}

	if err := ensureInternalNetworkReady(context.Background(), cli, "agency-agent-internal"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inspectCalls < 3 {
		t.Fatalf("expected repeated verification, got %d inspect calls", inspectCalls)
	}
}

func TestEnsureInternalNetworkReadyAcceptsAlreadyExistingCreateRace(t *testing.T) {
	var inspectCalls int
	var createCalls int
	cli := &mockDockerNetworkAPI{
		inspectFn: func(_ context.Context, _ string, _ network.InspectOptions) (network.Inspect, error) {
			inspectCalls++
			if inspectCalls == 1 {
				return network.Inspect{}, errors.New("network not found")
			}
			return network.Inspect{Name: "agency-agent-internal"}, nil
		},
		createFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
			createCalls++
			return network.CreateResponse{}, errors.New("network with name agency-agent-internal already exists")
		},
	}

	if err := ensureInternalNetworkReady(context.Background(), cli, "agency-agent-internal"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("expected exactly one create call, got %d", createCalls)
	}
}

func TestEnsureInternalNetworkReadyPropagatesUnexpectedInspectErrors(t *testing.T) {
	cli := &mockDockerNetworkAPI{
		inspectFn: func(_ context.Context, _ string, _ network.InspectOptions) (network.Inspect, error) {
			return network.Inspect{}, errors.New("permission denied")
		},
	}

	if err := ensureInternalNetworkReady(context.Background(), cli, "agency-agent-internal"); err == nil {
		t.Fatal("expected inspect error")
	}
}
