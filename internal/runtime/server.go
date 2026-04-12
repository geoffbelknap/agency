package runtime

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
)

func ServeAuthorityFromInstanceDir(ctx context.Context, instanceDir, nodeID string, port int) error {
	store := NewStore(instanceDir)
	manifest, err := store.LoadManifest()
	if err != nil {
		return err
	}
	if _, err := findAuthorityNode(manifest, nodeID); err != nil {
		return err
	}
	validator, err := LoadConsentValidator(instanceDir, manifest)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: AuthorityHandler{Manifest: manifest, Resolver: authzcore.Resolver{}, ConsentValidator: validator},
	}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	if err := store.SaveNodeStatus(NodeStatus{
		NodeID:      nodeID,
		State:       NodeStateActive,
		RuntimePath: filepath.Join("authority", nodeID+".yaml"),
	}); err != nil {
		return err
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
