package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"github.com/geoffbelknap/agency/internal/models"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
)

type RuntimeDelivery struct {
	Instances *instancepkg.Store
	Manager   runtimeManager
	Client    *http.Client
}

type runtimeManager interface {
	Status(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error)
	StartAuthority(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error)
}

func NewRuntimeDelivery(instances *instancepkg.Store) *RuntimeDelivery {
	return &RuntimeDelivery{
		Instances: instances,
		Manager:   runpkg.Manager{},
		Client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (rd *RuntimeDelivery) Deliver(sub *Subscription, event *models.Event) error {
	if rd == nil || rd.Instances == nil {
		return fmt.Errorf("runtime delivery is not configured")
	}
	instanceID, nodeID, err := splitRuntimeTarget(sub.Destination.Target)
	if err != nil {
		return err
	}
	instanceDir, err := rd.Instances.InstanceDir(instanceID)
	if err != nil {
		return err
	}
	store := runpkg.NewStore(instanceDir)
	manifest, err := store.LoadManifest()
	if err != nil {
		return err
	}
	status, err := rd.Manager.Status(store, manifest, nodeID)
	if err != nil {
		return err
	}
	if status.State != runpkg.NodeStateActive || strings.TrimSpace(status.URL) == "" {
		status, err = rd.Manager.StartAuthority(store, manifest, nodeID)
		if err != nil {
			return err
		}
	}

	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(status.URL, "/")+"/events/"+event.EventType, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := rd.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("runtime delivery failed: %s", strings.TrimSpace(string(msg)))
	}
	return nil
}

func (rd *RuntimeDelivery) client() *http.Client {
	if rd.Client != nil {
		return rd.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func splitRuntimeTarget(target string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(target), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid runtime target %q", target)
	}
	return parts[0], parts[1], nil
}
