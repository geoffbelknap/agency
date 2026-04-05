package hub

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	// OCI media types for hub components.
	MediaTypeComponent = "application/vnd.agency.hub.component.v1+yaml"
	MediaTypeMetadata  = "application/vnd.agency.hub.metadata.v1+yaml"

	// Annotation keys.
	AnnotationKind       = "agency.hub.kind"
	AnnotationReviewedBy = "agency.hub.reviewed_by"
)

// ociClient wraps an OCI registry base URL for pulling hub components.
type ociClient struct {
	registry string // e.g. "ghcr.io/geoffbelknap/agency-hub"
}

// newOCIClient creates a new OCI client for the given registry base.
func newOCIClient(registry string) *ociClient {
	return &ociClient{registry: registry}
}

// pullComponent pulls a single OCI artifact to the hub cache directory.
// Files are written to {cacheDir}/{sourceName}/{kind}s/{name}/ matching
// the git cache structure so discover() works unchanged.
func (c *ociClient) pullComponent(ctx context.Context, kind, name, version, cacheDir, sourceName string) error {
	if version == "" {
		version = "latest"
	}

	// Build the full OCI reference: {registry}/{kind}/{name}:{version}
	ref := c.registry + "/" + kind + "/" + name + ":" + version

	host := extractRegistryHost(c.registry)
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return fmt.Errorf("oci: invalid reference %q: %w", ref, err)
	}

	// Configure auth client for GHCR compatibility (anonymous pull for public repos).
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.DefaultCache,
		Credential: auth.StaticCredential(host, auth.Credential{}),
	}

	// Destination directory matching git cache structure.
	destDir := filepath.Join(cacheDir, sourceName, kind+"s", name)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("oci: mkdir %s: %w", destDir, err)
	}

	// Create a file store to receive pulled content.
	fs, err := file.New(destDir)
	if err != nil {
		return fmt.Errorf("oci: create file store: %w", err)
	}
	defer fs.Close()

	// Pull the artifact from the remote repository to the file store.
	_, err = oras.Copy(ctx, repo, version, fs, version, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("oci: pull %s: %w", ref, err)
	}

	return nil
}

// listTags returns available tags for a component in the registry.
func (c *ociClient) listTags(ctx context.Context, kind, name string) ([]string, error) {
	ref := c.registry + "/" + kind + "/" + name
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("oci: invalid reference %q: %w", ref, err)
	}

	host := extractRegistryHost(c.registry)
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.DefaultCache,
		Credential: auth.StaticCredential(host, auth.Credential{}),
	}

	var tags []string
	err = repo.Tags(ctx, "", func(t []string) error {
		tags = append(tags, t...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("oci: list tags for %s: %w", ref, err)
	}
	return tags, nil
}

// syncOCISource syncs all components from an OCI registry source into
// the hub cache. It uses the registry catalog API to enumerate repositories
// and pulls the latest version of each.
func (c *ociClient) syncOCISource(ctx context.Context, cacheDir, sourceName string) error {
	host := extractRegistryHost(c.registry)
	prefix := extractRepoPrefix(c.registry)

	reg, err := remote.NewRegistry(host)
	if err != nil {
		return fmt.Errorf("oci: connect to registry %s: %w", host, err)
	}

	reg.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.DefaultCache,
		Credential: auth.StaticCredential(host, auth.Credential{}),
	}

	// Enumerate all repositories under the registry prefix.
	var repos []string
	err = reg.Repositories(ctx, "", func(r []string) error {
		for _, name := range r {
			// Only include repos that match our prefix (e.g. "geoffbelknap/agency-hub/...")
			if strings.HasPrefix(name, prefix+"/") {
				repos = append(repos, name)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("oci: enumerate repositories: %w", err)
	}

	// Parse each repo path to extract kind and component name, then pull.
	for _, repoPath := range repos {
		// Strip the prefix to get "{kind}/{name}"
		relative := strings.TrimPrefix(repoPath, prefix+"/")
		parts := strings.SplitN(relative, "/", 2)
		if len(parts) != 2 {
			continue
		}
		kind := parts[0]
		name := parts[1]

		if err := c.pullComponent(ctx, kind, name, "latest", cacheDir, sourceName); err != nil {
			// Log but continue syncing other components.
			fmt.Fprintf(os.Stderr, "oci: warning: failed to pull %s/%s: %v\n", kind, name, err)
		}
	}

	return nil
}

// verifySignature checks the cosign keyless signature on an OCI artifact.
// Shells out to the cosign CLI binary. Returns nil if valid, error if unsigned,
// invalid, or cosign is not installed.
//
// Uses Sigstore public-good instance (Fulcio + Rekor) for keyless verification.
// Verifies the signature was created by GitHub Actions OIDC for the agency-hub repo.
func verifySignature(ctx context.Context, ref string) error {
	cosignPath, err := exec.LookPath("cosign")
	if err != nil {
		return fmt.Errorf("cosign not installed — required for signature verification (install: https://docs.sigstore.dev/cosign/system_config/installation/)")
	}

	args := []string{
		"verify",
		"--certificate-identity-regexp", "https://github.com/geoffbelknap/agency-hub/.*",
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		ref,
	}

	cmd := exec.CommandContext(ctx, cosignPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("signature verification failed for %s: %s", ref, string(output))
	}
	return nil
}

// cosignInstalled returns true if the cosign CLI is available.
func cosignInstalled() bool {
	_, err := exec.LookPath("cosign")
	return err == nil
}

// extractRegistryHost returns the hostname from a registry string.
// "ghcr.io/foo/bar" → "ghcr.io"
func extractRegistryHost(registry string) string {
	parts := strings.SplitN(registry, "/", 2)
	return parts[0]
}

// extractRepoPrefix returns everything after the hostname.
// "ghcr.io/foo/bar" → "foo/bar"
func extractRepoPrefix(registry string) string {
	parts := strings.SplitN(registry, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
