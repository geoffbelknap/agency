package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Reconciler struct {
	Home string
}

func (r Reconciler) Reconcile(store *Store, m *Manifest) error {
	for _, node := range m.Runtime.Nodes {
		switch node.Kind {
		case "connector.authority":
			if err := store.SaveAuthorityConfig(node); err != nil {
				m.Status.ReconcileState = ReconcileStateFailed
				return err
			}
		case "connector.ingress":
			if err := store.SaveIngressConfig(node); err != nil {
				m.Status.ReconcileState = ReconcileStateFailed
				return err
			}
			if err := r.publishIngress(node); err != nil {
				m.Status.ReconcileState = ReconcileStateFailed
				return err
			}
		default:
			continue
		}
		if err := store.SaveNodeStatus(NodeStatus{
			NodeID:      node.NodeID,
			State:       NodeStateMaterialized,
			UpdatedAt:   time.Now().UTC(),
			RuntimePath: node.Materialization,
		}); err != nil {
			m.Status.ReconcileState = ReconcileStateFailed
			return err
		}
	}
	now := time.Now().UTC()
	m.Status.ReconcileState = ReconcileStateMaterialized
	m.Status.LastReconciledAt = &now
	return store.SaveManifest(m)
}

func (r Reconciler) publishIngress(node RuntimeNode) error {
	if node.Ingress == nil || strings.TrimSpace(node.Ingress.PublishedName) == "" {
		return fmt.Errorf("ingress node %q missing published_name", node.NodeID)
	}
	if strings.TrimSpace(r.Home) == "" {
		return nil
	}
	connectorsDir := filepath.Join(r.Home, "connectors")
	if err := os.MkdirAll(connectorsDir, 0o755); err != nil {
		return fmt.Errorf("create connectors dir: %w", err)
	}
	path := filepath.Join(connectorsDir, node.Ingress.PublishedName+".yaml")
	if err := os.WriteFile(path, []byte(node.Ingress.ConnectorYAML), 0o644); err != nil {
		return fmt.Errorf("publish ingress connector: %w", err)
	}
	return nil
}
