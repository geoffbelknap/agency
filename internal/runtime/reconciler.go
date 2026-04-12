package runtime

import "time"

type Reconciler struct{}

func (r Reconciler) Reconcile(store *Store, m *Manifest) error {
	for _, node := range m.Runtime.Nodes {
		if node.Kind != "connector.authority" {
			continue
		}
		if err := store.SaveAuthorityConfig(node); err != nil {
			m.Status.ReconcileState = ReconcileStateFailed
			return err
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
