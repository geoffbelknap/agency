package events

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"github.com/geoffbelknap/agency/internal/models"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
)

type stubRuntimeManager struct {
	status       *runpkg.NodeStatus
	startedState *runpkg.NodeStatus
	starts       int
}

func (m *stubRuntimeManager) Status(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error) {
	if m.status != nil {
		return m.status, nil
	}
	return &runpkg.NodeStatus{NodeID: nodeID, State: runpkg.NodeStateMaterialized}, nil
}

func (m *stubRuntimeManager) StartAuthority(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error) {
	m.starts++
	return m.startedState, nil
}

func TestRuntimeDeliveryStartsRuntimeAndForwardsEvent(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	home := t.TempDir()
	instances := instancepkg.NewStore(filepath.Join(home, "instances"))
	if err := instances.Create(t.Context(), &instancepkg.Instance{
		ID:   "inst_123",
		Name: "slack-interactivity",
		Source: instancepkg.InstanceSource{
			Package: instancepkg.PackageRef{Kind: "connector", Name: "slack-interactivity", Version: "1.0.0"},
		},
	}); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	instanceDir, err := instances.InstanceDir("inst_123")
	if err != nil {
		t.Fatalf("InstanceDir(): %v", err)
	}
	rtStore := runpkg.NewStore(instanceDir)
	manifest := &runpkg.Manifest{
		APIVersion: runpkg.ManifestAPIVersion,
		Kind:       runpkg.ManifestKind,
		Metadata: runpkg.ManifestMeta{
			ManifestID:   "mf_123",
			InstanceID:   "inst_123",
			InstanceName: "slack-interactivity",
			CompiledAt:   time.Now().UTC(),
			Planner:      runpkg.PlannerVersion,
		},
		Runtime: runpkg.RuntimeSpec{
			Nodes: []runpkg.RuntimeNode{{
				NodeID:          "slack_authority",
				Kind:            "connector.authority",
				Materialization: "authority/slack_authority.yaml",
				Executor:        &runpkg.RuntimeExecutor{Kind: "slack_interactivity", BaseURL: "https://slack.com"},
			}},
		},
	}
	if err := os.MkdirAll(filepath.Join(instanceDir, "runtime"), 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	if err := rtStore.SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest(): %v", err)
	}

	manager := &stubRuntimeManager{
		status: &runpkg.NodeStatus{
			NodeID: "slack_authority",
			State:  runpkg.NodeStateMaterialized,
		},
		startedState: &runpkg.NodeStatus{
			NodeID: "slack_authority",
			State:  runpkg.NodeStateActive,
			URL:    server.URL,
		},
	}
	delivery := &RuntimeDelivery{
		Instances: instances,
		Manager:   manager,
	}
	event := models.NewEvent(models.EventSourceConnector, "slack-interactivity", "approval_action", map[string]any{"action_id": "consent_approve"})
	sub := &Subscription{
		SourceType: models.EventSourceConnector,
		SourceName: "slack-interactivity",
		EventType:  "approval_action",
		Origin:     OriginInstance,
		OriginRef:  "inst_123",
		Active:     true,
		Destination: Destination{
			Type:   DestRuntime,
			Target: "inst_123/slack_authority",
		},
	}

	if err := delivery.Deliver(sub, event); err != nil {
		t.Fatalf("Deliver(): %v", err)
	}
	if manager.starts != 1 {
		t.Fatalf("starts = %d, want 1", manager.starts)
	}
	if requestedPath != "/events/approval_action" {
		t.Fatalf("requestedPath = %q", requestedPath)
	}
}
