package images

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"log/slog"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

const registry = "ghcr.io/geoffbelknap"

const (
	OllamaUpstream = "ollama/ollama"
	OllamaVersion  = "0.9.3"
)

// Resolve ensures the Docker image for the named service is available locally.
//
// Resolution order:
//  1. Source tree build (if sourceDir is set — dev mode). Failure is fatal.
//  2. GHCR pull (release mode)
func Resolve(ctx context.Context, cli *client.Client, name, version, sourceDir, buildID string, logger *slog.Logger) error {
	localTag := fmt.Sprintf("agency-%s:latest", name)

	// Check if existing local image is current — skip rebuild if buildID matches.
	exists, err := imageExists(ctx, cli, localTag)
	if err != nil {
		return fmt.Errorf("check local image %s: %w", localTag, err)
	}
	if exists && buildID != "" {
		imgBuildID := ImageBuildLabel(ctx, cli, localTag)
		// BUILD_ID is content-aware for dirty trees, so an exact match means the
		// local image already reflects the current working copy.
		if imgBuildID != "" && imgBuildID == buildID {
			return nil // Image is current — skip rebuild
		}
		if imgBuildID != "" {
			logger.Info("image stale, rebuilding", "image", localTag, "current", imgBuildID, "want", buildID)
		}
	} else if exists && buildID == "" {
		return nil // No buildID to compare — assume current
	}

	// No source dir means we can't rebuild — if the image already exists locally,
	// use it as-is rather than pulling from GHCR (which may be a different version).
	// This covers images like agency-web that are built out-of-tree via `make web`.
	if sourceDir == "" && exists {
		logger.Info("image exists locally, no source dir to rebuild — using as-is", "image", localTag)
		return nil
	}

	// Dev mode: rebuild from source tree. Failure is fatal — no silent fallback.
	if sourceDir != "" {
		if err := buildFromSource(ctx, cli, name, sourceDir, localTag, buildID, logger); err != nil {
			return fmt.Errorf("image %s: source build failed: %w", localTag, err)
		}
		// Also tag with buildID so old images are identifiable for cleanup
		if buildID != "" {
			versionTag := fmt.Sprintf("agency-%s:%s", name, buildID)
			cli.ImageTag(ctx, localTag, versionTag)
		}
		// Prune old images for this service (keep only current)
		pruneOldImages(ctx, cli, name, buildID, logger)
		return nil
	}

	// Release mode: GHCR pull
	if version != "" && version != "dev" {
		remoteTag := fmt.Sprintf("%s/agency-%s:v%s", registry, name, version)
		logger.Info("pulling image", "image", remoteTag)
		for attempt := 0; attempt < 2; attempt++ {
			if err := pullAndTag(ctx, cli, remoteTag, localTag); err == nil {
				// Prune old images now that we have a fresh pull
				pruneOldImages(ctx, cli, name, buildID, logger)
				return nil
			} else {
				if attempt == 0 {
					logger.Warn("pull failed, retrying", "image", remoteTag, "err", err)
					time.Sleep(2 * time.Second)
				}
			}
		}
	}

	return fmt.Errorf("image %s: no resolution method available (source_dir=%q, version=%q)", localTag, sourceDir, version)
}

// ResolveUpstream ensures the Docker image for an upstream service (not built from source) is
// available locally. Unlike Resolve(), there is no dev-mode source build path — the image is
// always pulled.
//
// Resolution order:
//  1. Local tag agency-<name>:latest exists and is current (buildID matches) — skip.
//  2. Pull from GHCR: ghcr.io/geoffbelknap/agency-<name>:v<version>, retag to agency-<name>:latest.
//  3. Fallback: pull directly from upstreamRef (e.g. "ollama/ollama:0.9.3"), retag to agency-<name>:latest.
//  4. Return error if all methods fail.
func ResolveUpstream(ctx context.Context, cli *client.Client, name, version, upstreamRef, buildID string, logger *slog.Logger) error {
	localTag := fmt.Sprintf("agency-%s:latest", name)

	// Check if existing local image is current — skip pull if buildID matches.
	exists, err := imageExists(ctx, cli, localTag)
	if err != nil {
		return fmt.Errorf("check local image %s: %w", localTag, err)
	}
	if exists && buildID != "" {
		imgBuildID := ImageBuildLabel(ctx, cli, localTag)
		if imgBuildID != "" && imgBuildID == buildID {
			return nil // Image is current — skip pull
		}
		if imgBuildID != "" && logger != nil {
			logger.Info("image stale, re-pulling", "image", localTag, "current", imgBuildID, "want", buildID)
		}
	} else if exists && buildID == "" {
		return nil // No buildID to compare — assume current
	}

	// Try GHCR first (our published mirror of the upstream image).
	if version != "" {
		ghcrTag := fmt.Sprintf("%s/agency-%s:v%s", registry, name, version)
		if logger != nil {
			logger.Info("pulling image from GHCR", "image", ghcrTag)
		}
		if err := pullAndTag(ctx, cli, ghcrTag, localTag); err == nil {
			pruneOldImages(ctx, cli, name, buildID, logger)
			return nil
		} else if logger != nil {
			logger.Warn("GHCR pull failed, falling back to upstream", "image", ghcrTag, "err", err)
		}
	}

	// Fallback: pull directly from upstream source.
	if upstreamRef != "" {
		if logger != nil {
			logger.Info("pulling image from upstream", "image", upstreamRef)
		}
		if err := pullAndTag(ctx, cli, upstreamRef, localTag); err == nil {
			pruneOldImages(ctx, cli, name, buildID, logger)
			return nil
		} else if logger != nil {
			logger.Warn("upstream pull failed", "image", upstreamRef, "err", err)
		}
	}

	return fmt.Errorf("image %s: upstream resolution failed (ghcr version=%q, upstream=%q)", localTag, version, upstreamRef)
}

// buildFromSource builds an image directly from the source tree.
// This is the dev-mode path — always builds fresh from current source.
func buildFromSource(ctx context.Context, cli *client.Client, name, sourceDir, tag, buildID string, logger *slog.Logger) error {
	// Services that need the repo root as build context
	// (they COPY agency_core/models/ and agency_core/exceptions.py)
	repoContextNames := map[string]bool{
		"body": true, "comms": true, "knowledge": true, "intake": true, "egress": true,
	}

	var contextDir, dockerfilePath string

	switch {
	case repoContextNames[name]:
		// Build context is repo root; Dockerfile is in images/{name}/
		contextDir = sourceDir
		dockerfilePath = filepath.Join("images", name, "Dockerfile")
	case name == "web":
		// agency-web lives in the top-level web/ directory rather than images/.
		contextDir = filepath.Join(sourceDir, "web")
		dockerfilePath = "Dockerfile"
	default:
		// Self-contained: build context is the image directory itself
		contextDir = filepath.Join(sourceDir, "images", name)
		dockerfilePath = "Dockerfile"
	}

	if _, err := os.Stat(filepath.Join(contextDir, dockerfilePath)); err != nil {
		return fmt.Errorf("source build for %s: Dockerfile not found at %s/%s", name, contextDir, dockerfilePath)
	}

	logger.Info("building image from source", "image", tag, "context", contextDir)
	// CACHE_BUST forces layer invalidation even when BuildKit ignores NoCache.
	// Using unix timestamp ensures the COPY and RUN layers always rebuild.
	cacheBust := fmt.Sprintf("%d", time.Now().Unix())
	buildArgs := map[string]*string{"CACHE_BUST": &cacheBust}
	if buildID != "" {
		buildArgs["BUILD_ID"] = &buildID
	}
	return dockerBuild(ctx, cli, contextDir, dockerfilePath, tag, buildArgs)
}

func imageExists(ctx context.Context, cli *client.Client, ref string) (bool, error) {
	if cli == nil {
		return false, fmt.Errorf("no Docker client")
	}
	_, _, err := cli.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		if strings.Contains(err.Error(), "No such image") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func pullAndTag(ctx context.Context, cli *client.Client, remoteRef, localTag string) error {
	reader, err := cli.ImagePull(ctx, remoteRef, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("pull stream: %w", err)
	}
	return cli.ImageTag(ctx, remoteRef, localTag)
}


// ImageBuildLabel reads the agency.build.id label from a Docker image.
// Returns empty string if the image cannot be inspected or the label is absent.
func ImageBuildLabel(ctx context.Context, cli *client.Client, ref string) string {
	inspect, _, err := cli.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		return ""
	}
	return inspect.Config.Labels["agency.build.id"]
}

func dockerBuild(ctx context.Context, cli *client.Client, contextDir, dockerfile, tag string, buildArgs map[string]*string) error {
	tarReader, err := createTar(contextDir)
	if err != nil {
		return fmt.Errorf("create build context tar: %w", err)
	}
	defer tarReader.Close()

	resp, err := cli.ImageBuild(ctx, tarReader, types.ImageBuildOptions{
		Dockerfile:  dockerfile,
		Tags:        []string{tag},
		Remove:      true,
		ForceRemove: true,
		BuildArgs:   buildArgs,
	})
	if err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	defer resp.Body.Close()

	// Read build output and check for errors — Docker returns build errors
	// in the response stream, not as HTTP errors.
	dec := json.NewDecoder(resp.Body)
	for dec.More() {
		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			return fmt.Errorf("decode build output: %w", err)
		}
		if msg.Error != "" {
			return fmt.Errorf("docker build error: %s", msg.Error)
		}
	}
	return nil
}

// dockerIgnorePatterns returns the exclusion patterns from .dockerignore in dir.
func dockerIgnorePatterns(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, ".dockerignore"))
	if err != nil {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// matchesIgnore checks if a relative path matches any .dockerignore pattern.
// Follows Docker's .dockerignore semantics:
//   - "*.txt" matches only at root level (not in subdirectories)
//   - "dir/" matches a directory and all its contents
//   - Exact names match at root level and as path prefixes
func matchesIgnore(rel string, isDir bool, patterns []string) bool {
	for _, p := range patterns {
		// Directory pattern (trailing /)
		if strings.HasSuffix(p, "/") {
			dirPat := strings.TrimSuffix(p, "/")
			if rel == dirPat || strings.HasPrefix(rel, dirPat+"/") {
				return true
			}
			continue
		}
		// Glob pattern — match against full relative path (root-level only,
		// consistent with Docker's .dockerignore behavior)
		if matched, _ := filepath.Match(p, rel); matched {
			return true
		}
		// Exact match or directory prefix match
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

func createTar(dir string) (io.ReadCloser, error) {
	ignorePatterns := dockerIgnorePatterns(dir)
	r, w := io.Pipe()
	go func() {
		tw := tar.NewWriter(w)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			// Skip __pycache__, .pyc, .git (hardcoded — always excluded)
			if info.IsDir() && (rel == "__pycache__" || rel == ".git") {
				return filepath.SkipDir
			}
			if strings.HasSuffix(rel, ".pyc") {
				return nil
			}
			// Skip paths matching .dockerignore patterns
			if matchesIgnore(rel, info.IsDir(), ignorePatterns) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = rel
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			// LimitReader prevents "write too long" if the file grows between
			// Walk (which records size) and Copy (which reads the actual bytes).
			_, err = io.Copy(tw, io.LimitReader(f, info.Size()))
			return err
		})
		tw.Close()
		w.CloseWithError(err)
	}()
	return r, nil
}

// pruneOldImages removes old images for a service, keeping only the current buildID.
// This prevents unbounded image accumulation from repeated rebuilds.
func pruneOldImages(ctx context.Context, cli *client.Client, name, currentBuildID string, logger *slog.Logger) {
	if cli == nil {
		return
	}
	prefix := fmt.Sprintf("agency-%s:", name)
	currentLatest := prefix + "latest"
	currentVersionTag := prefix + currentBuildID

	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return
	}
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if !strings.HasPrefix(tag, prefix) {
				continue
			}
			if tag == currentLatest || tag == currentVersionTag {
				continue
			}
			// Old image for this service — remove it
			_, err := cli.ImageRemove(ctx, tag, image.RemoveOptions{PruneChildren: true})
			if err == nil {
				logger.Info("pruned old image", "image", tag)
			}
		}
	}
}
