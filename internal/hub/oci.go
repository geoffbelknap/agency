package hub

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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
	MediaTypeIndex     = "application/vnd.agency.hub.index.v1+yaml"

	// Annotation keys.
	AnnotationKind       = "agency.hub.kind"
	AnnotationReviewedBy = "agency.hub.reviewed_by"
)

// ociClient wraps an OCI registry base URL for pulling hub components.
type ociClient struct {
	registry string // e.g. "ghcr.io/geoffbelknap/agency-hub"
}

type ociIndex struct {
	SchemaVersion int                 `yaml:"schema_version"`
	Registry      string              `yaml:"registry"`
	Components    []ociIndexComponent `yaml:"components"`
}

type ociIndexComponent struct {
	Kind         string `yaml:"kind"`
	Name         string `yaml:"name"`
	Version      string `yaml:"version"`
	Ref          string `yaml:"ref"`
	Path         string `yaml:"path"`
	MetadataPath string `yaml:"metadata_path"`
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

	repoRef := c.registry + "/" + kind + "/" + name
	repo, err := c.newRepository(repoRef)
	if err != nil {
		return fmt.Errorf("oci: invalid reference %q:%s: %w", repoRef, version, err)
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
		return fmt.Errorf("oci: pull %s:%s: %w", repoRef, version, err)
	}

	return nil
}

// pullComponentEntry pulls one catalog-indexed component and writes it back to
// its hub-relative path. This avoids depending on registry catalog APIs and
// normalizes whatever title/path the registry stores for pulled layers.
func (c *ociClient) pullComponentEntry(ctx context.Context, entry ociIndexComponent, cacheDir, sourceName string) error {
	if entry.Kind == "" || entry.Name == "" || entry.Path == "" {
		return fmt.Errorf("oci: invalid catalog entry for %s/%s", entry.Kind, entry.Name)
	}
	version := entry.Version
	if version == "" {
		version = "latest"
	}
	ref := entry.Ref
	if ref == "" {
		ref = c.registry + "/" + entry.Kind + "/" + entry.Name + ":" + version
	}

	tmpDir, err := os.MkdirTemp("", "agency-hub-oci-*")
	if err != nil {
		return fmt.Errorf("oci: create temp pull dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoRef, reference, err := splitOCIRef(ref)
	if err != nil {
		return err
	}
	if reference == "" {
		reference = version
	}
	if err := c.pullArtifact(ctx, repoRef, reference, tmpDir); err != nil {
		return err
	}

	destBase := filepath.Join(cacheDir, sourceName)
	componentDest, err := safeCachePath(destBase, entry.Path)
	if err != nil {
		return err
	}
	if err := copyPulledFile(tmpDir, entry.Path, componentDest); err != nil {
		return fmt.Errorf("oci: copy component %s: %w", entry.Path, err)
	}
	if entry.MetadataPath != "" {
		metadataDest, err := safeCachePath(destBase, entry.MetadataPath)
		if err != nil {
			return err
		}
		if err := copyPulledFile(tmpDir, entry.MetadataPath, metadataDest); err != nil {
			return fmt.Errorf("oci: copy metadata %s: %w", entry.MetadataPath, err)
		}
	}
	return nil
}

func (c *ociClient) pullArtifact(ctx context.Context, repoRef, reference, destDir string) error {
	repo, err := c.newRepository(repoRef)
	if err != nil {
		return fmt.Errorf("oci: invalid reference %q: %w", repoRef, err)
	}

	fs, err := file.New(destDir)
	if err != nil {
		return fmt.Errorf("oci: create file store: %w", err)
	}
	defer fs.Close()

	_, err = oras.Copy(ctx, repo, reference, fs, reference, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("oci: pull %s:%s: %w", repoRef, reference, err)
	}
	return nil
}

func (c *ociClient) newRepository(repoRef string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(repoRef)
	if err != nil {
		return nil, err
	}
	host := extractRegistryHost(repoRef)
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.DefaultCache,
		Credential: auth.StaticCredential(host, auth.Credential{}),
	}
	return repo, nil
}

// listTags returns available tags for a component in the registry.
func (c *ociClient) listTags(ctx context.Context, kind, name string) ([]string, error) {
	ref := c.registry + "/" + kind + "/" + name
	repo, err := c.newRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("oci: invalid reference %q: %w", ref, err)
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
// the hub cache. It prefers the Agency Hub catalog artifact because GHCR does
// not expose registry catalog enumeration reliably for package namespaces.
// Registry catalog enumeration remains as a fallback for compatible registries.
func (c *ociClient) syncOCISource(ctx context.Context, cacheDir, sourceName string) error {
	if err := removeLegacyGitMetadata(filepath.Join(cacheDir, sourceName)); err != nil {
		return err
	}

	if index, err := c.pullIndex(ctx); err == nil {
		for _, entry := range index.Components {
			if err := c.pullComponentEntry(ctx, entry, cacheDir, sourceName); err != nil {
				fmt.Fprintf(os.Stderr, "oci: warning: failed to pull %s/%s: %v\n", entry.Kind, entry.Name, err)
			}
		}
		return nil
	} else {
		fmt.Fprintf(os.Stderr, "oci: warning: failed to pull catalog index: %v\n", err)
	}

	host := extractRegistryHost(c.registry)
	prefix := extractRepoPrefix(c.registry)

	reg, err := remote.NewRegistry(host)
	if err != nil {
		return fmt.Errorf("oci: connect to registry %s: %w", host, err)
	}

	reg.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.DefaultCache,
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

func removeLegacyGitMetadata(sourceDir string) error {
	gitDir := filepath.Join(sourceDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("oci: inspect legacy git metadata: %w", err)
	}
	if err := os.RemoveAll(gitDir); err != nil {
		return fmt.Errorf("oci: remove legacy git metadata: %w", err)
	}
	return nil
}

func (c *ociClient) pullIndex(ctx context.Context) (*ociIndex, error) {
	tmpDir, err := os.MkdirTemp("", "agency-hub-index-*")
	if err != nil {
		return nil, fmt.Errorf("oci: create temp index dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoRef := c.registry + "/index/catalog"
	if err := c.pullArtifact(ctx, repoRef, "latest", tmpDir); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "oci-index.yaml"))
	if err != nil {
		data, err = findFirstYAML(tmpDir)
	}
	if err != nil {
		return nil, fmt.Errorf("oci: read catalog index: %w", err)
	}

	var index ociIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("oci: parse catalog index: %w", err)
	}
	if index.SchemaVersion != 1 {
		return nil, fmt.Errorf("oci: unsupported catalog index schema_version %d", index.SchemaVersion)
	}
	return &index, nil
}

func findFirstYAML(root string) ([]byte, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found != "" {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".yaml") || strings.HasSuffix(info.Name(), ".yml") {
			found = path
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if found == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(found)
}

func copyPulledFile(srcRoot, relativePath, destPath string) error {
	safeSourcePath, err := safeCachePath(srcRoot, relativePath)
	if err != nil {
		return err
	}
	candidates := []string{
		safeSourcePath,
		filepath.Join(srcRoot, filepath.Base(relativePath)),
	}
	for _, candidate := range candidates {
		if err := copyFile(candidate, destPath); err == nil {
			return nil
		}
	}
	return fmt.Errorf("pulled file not found")
}

func safeCachePath(root, relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("oci: absolute catalog path %q is not allowed", relativePath)
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", fmt.Errorf("oci: catalog path %q escapes cache root", relativePath)
	}
	return filepath.Join(root, clean), nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func splitOCIRef(ref string) (repoRef, reference string, err error) {
	lastSlash := strings.LastIndex(ref, "/")
	lastColon := strings.LastIndex(ref, ":")
	if lastColon <= lastSlash {
		return ref, "", nil
	}
	reference = ref[lastColon+1:]
	if reference == "" {
		return "", "", fmt.Errorf("oci: invalid empty tag in %q", ref)
	}
	return ref[:lastColon], reference, nil
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

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		cmd := exec.CommandContext(ctx, cosignPath, args...)
		output, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("signature verification failed for %s: %s", ref, string(output))
		if !isTransientCosignVerifyError(string(output)) || attempt == 2 {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return lastErr
}

// cosignInstalled returns true if the cosign CLI is available.
func cosignInstalled() bool {
	_, err := exec.LookPath("cosign")
	return err == nil
}

func isTransientCosignVerifyError(output string) bool {
	lower := strings.ToLower(output)
	transientSignals := []string{
		"i/o timeout",
		"tls handshake timeout",
		"temporary failure in name resolution",
		"no such host",
		"connection reset by peer",
		"context deadline exceeded",
		"net/http: timeout awaiting response headers",
	}
	for _, signal := range transientSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
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
