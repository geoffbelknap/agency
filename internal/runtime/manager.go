package runtime

import (
	"fmt"
	"time"
)

type Manager struct{}

func (m Manager) StartAuthority(store *Store, manifest *Manifest, nodeID string) (*NodeStatus, error) {
	node, err := findAuthorityNode(manifest, nodeID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	status := NodeStatus{
		NodeID:      node.NodeID,
		State:       NodeStateActive,
		UpdatedAt:   now,
		StartedAt:   &now,
		RuntimePath: node.Materialization,
	}
	if err := store.SaveNodeStatus(status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (m Manager) StopAuthority(store *Store, manifest *Manifest, nodeID string) (*NodeStatus, error) {
	node, err := findAuthorityNode(manifest, nodeID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	status, err := store.LoadNodeStatus(nodeID)
	if err != nil {
		status = &NodeStatus{NodeID: nodeID, RuntimePath: node.Materialization}
	}
	status.State = NodeStateStopped
	status.UpdatedAt = now
	status.StoppedAt = &now
	if err := store.SaveNodeStatus(*status); err != nil {
		return nil, err
	}
	return status, nil
}

func (m Manager) Status(store *Store, manifest *Manifest, nodeID string) (*NodeStatus, error) {
	node, err := findAuthorityNode(manifest, nodeID)
	if err != nil {
		return nil, err
	}
	status, err := store.LoadNodeStatus(nodeID)
	if err == nil {
		return status, nil
	}
	return &NodeStatus{
		NodeID:      nodeID,
		State:       NodeStateMaterialized,
		UpdatedAt:   manifest.Metadata.CompiledAt,
		RuntimePath: node.Materialization,
	}, nil
}

func findAuthorityNode(manifest *Manifest, nodeID string) (*RuntimeNode, error) {
	for _, node := range manifest.Runtime.Nodes {
		if node.NodeID == nodeID && node.Kind == "connector.authority" {
			copy := node
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("authority node %q not found", nodeID)
}
