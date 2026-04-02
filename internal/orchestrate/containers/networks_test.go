package containers

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/network"
)

// mockNetworkAPI is a test double for NetworkAPI.
type mockNetworkAPI struct {
	createFn func(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	removeFn func(ctx context.Context, networkID string) error

	// Captured arguments from last call.
	lastCreateName    string
	lastCreateOptions network.CreateOptions
	lastRemoveID      string
}

func (m *mockNetworkAPI) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	m.lastCreateName = name
	m.lastCreateOptions = options
	if m.createFn != nil {
		return m.createFn(ctx, name, options)
	}
	return network.CreateResponse{ID: "net-id"}, nil
}

func (m *mockNetworkAPI) NetworkRemove(ctx context.Context, networkID string) error {
	m.lastRemoveID = networkID
	if m.removeFn != nil {
		return m.removeFn(ctx, networkID)
	}
	return nil
}

func TestCreateInternalNetwork_SetsInternalTrue(t *testing.T) {
	mock := &mockNetworkAPI{}
	if err := CreateInternalNetwork(context.Background(), mock, "agent-net", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.lastCreateOptions.Internal {
		t.Error("CreateInternalNetwork should set Internal: true")
	}
}

func TestCreateInternalNetwork_MergesAgencyManagedLabel(t *testing.T) {
	mock := &mockNetworkAPI{}
	labels := map[string]string{"agency.agent": "alice"}
	if err := CreateInternalNetwork(context.Background(), mock, "alice-net", labels); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastCreateOptions.Labels["agency.managed"] != "true" {
		t.Errorf("agency.managed label missing: %v", mock.lastCreateOptions.Labels)
	}
	if mock.lastCreateOptions.Labels["agency.agent"] != "alice" {
		t.Errorf("caller label not preserved: %v", mock.lastCreateOptions.Labels)
	}
}

func TestCreateEgressNetwork_DoesNotSetInternal(t *testing.T) {
	mock := &mockNetworkAPI{}
	if err := CreateEgressNetwork(context.Background(), mock, "egress-net", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastCreateOptions.Internal {
		t.Error("CreateEgressNetwork should NOT set Internal: true")
	}
}

func TestCreateEgressNetwork_MergesAgencyManagedLabel(t *testing.T) {
	mock := &mockNetworkAPI{}
	if err := CreateEgressNetwork(context.Background(), mock, "egress-net", map[string]string{"purpose": "egress"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastCreateOptions.Labels["agency.managed"] != "true" {
		t.Errorf("agency.managed label missing: %v", mock.lastCreateOptions.Labels)
	}
}

func TestRemoveNetwork_Success(t *testing.T) {
	removeCalled := false
	mock := &mockNetworkAPI{
		removeFn: func(_ context.Context, id string) error {
			removeCalled = true
			return nil
		},
	}
	if err := RemoveNetwork(context.Background(), mock, "my-net"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removeCalled {
		t.Error("NetworkRemove was not called")
	}
}

func TestRemoveNetwork_IgnoresNotFound(t *testing.T) {
	mock := &mockNetworkAPI{
		removeFn: func(_ context.Context, _ string) error {
			return errors.New("no such network: missing-net")
		},
	}
	if err := RemoveNetwork(context.Background(), mock, "missing-net"); err != nil {
		t.Errorf("expected nil for not-found, got: %v", err)
	}
}

func TestRemoveNetwork_PropagatesOtherErrors(t *testing.T) {
	otherErr := errors.New("internal docker error")
	mock := &mockNetworkAPI{
		removeFn: func(_ context.Context, _ string) error {
			return otherErr
		},
	}
	if err := RemoveNetwork(context.Background(), mock, "my-net"); !errors.Is(err, otherErr) {
		t.Errorf("expected %v, got %v", otherErr, err)
	}
}

func TestMergeLabels_NilInput(t *testing.T) {
	merged := mergeLabels(nil)
	if merged["agency.managed"] != "true" {
		t.Errorf("agency.managed missing from nil-input merge: %v", merged)
	}
}

func TestMergeLabels_DoesNotMutateInput(t *testing.T) {
	input := map[string]string{"key": "val"}
	mergeLabels(input)
	if _, exists := input["agency.managed"]; exists {
		t.Error("mergeLabels mutated the input map")
	}
}
