package artifacts

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	OllamaUpstream = "ollama/ollama"
	OllamaVersion  = "0.9.3"
)

const buildContextTransformVersion = "named-context-dir-slash-v2"

var sourceDependencies = map[string][]string{
	"body":      {"python-base"},
	"comms":     {"python-base"},
	"knowledge": {"python-base"},
	"intake":    {"python-base"},
	"workspace": {"workspace-base"},
}

type Resolver interface {
	Exists(ctx context.Context, ref string) (bool, error)
	Label(ctx context.Context, ref, key string) string
	BuildLabel(ctx context.Context, ref string) string
	BuildServiceFromSource(ctx context.Context, name, sourceDir, tag, buildID, sourceHash string, logger *slog.Logger) error
	Tag(ctx context.Context, sourceRef, targetRef string) error
	PullAndTag(ctx context.Context, remoteRef, localRef string) error
	PruneOld(ctx context.Context, name, currentBuildID string, logger *slog.Logger)
}

func Resolve(ctx context.Context, resolver Resolver, name, version, sourceDir, buildID, registry string, logger *slog.Logger) error {
	localTag := fmt.Sprintf("agency-%s:latest", name)
	var sourceHash string

	if sourceDir != "" {
		spec, err := sourceBuildSpec(name, sourceDir)
		if err != nil {
			return err
		}
		hash, err := sourceFingerprint(spec.contextDir, spec.dockerfilePath, spec.namedContexts)
		if err != nil {
			return fmt.Errorf("fingerprint image %s source: %w", localTag, err)
		}
		sourceHash = hash
	}

	exists, err := resolver.Exists(ctx, localTag)
	if err != nil {
		return fmt.Errorf("check local image %s: %w", localTag, err)
	}
	if exists && sourceHash != "" {
		imgSourceHash := resolver.Label(ctx, localTag, "agency.source.hash")
		if imgSourceHash != "" && imgSourceHash == sourceHash {
			return nil
		}
		if imgSourceHash != "" && logger != nil {
			logger.Info("image source stale, rebuilding", "image", localTag, "current", imgSourceHash, "want", sourceHash)
		}
	} else if exists && buildID != "" {
		imgBuildID := resolver.BuildLabel(ctx, localTag)
		if imgBuildID != "" && imgBuildID == buildID {
			return nil
		}
		if imgBuildID != "" && logger != nil {
			logger.Info("image stale, rebuilding", "image", localTag, "current", imgBuildID, "want", buildID)
		}
	} else if exists && buildID == "" {
		return nil
	}

	if sourceDir == "" && exists {
		if logger != nil {
			logger.Info("image exists locally, no source dir to rebuild - using as-is", "image", localTag)
		}
		return nil
	}

	if sourceDir != "" {
		for _, dep := range sourceDependencies[name] {
			if err := Resolve(ctx, resolver, dep, version, sourceDir, buildID, registry, logger); err != nil {
				return fmt.Errorf("resolve source dependency %s for %s: %w", dep, localTag, err)
			}
		}
		if err := resolver.BuildServiceFromSource(ctx, name, sourceDir, localTag, buildID, sourceHash, logger); err != nil {
			return fmt.Errorf("image %s: source build failed: %w", localTag, err)
		}
		if buildID != "" {
			versionTag := fmt.Sprintf("agency-%s:%s", name, buildID)
			_ = resolver.Tag(ctx, localTag, versionTag)
		}
		resolver.PruneOld(ctx, name, buildID, logger)
		return nil
	}

	if version != "" && version != "dev" {
		remoteTag := fmt.Sprintf("%s/agency-%s:v%s", registry, name, version)
		if logger != nil {
			logger.Info("pulling image", "image", remoteTag)
		}
		for attempt := 0; attempt < 2; attempt++ {
			if err := resolver.PullAndTag(ctx, remoteTag, localTag); err == nil {
				resolver.PruneOld(ctx, name, buildID, logger)
				return nil
			} else if attempt == 0 && logger != nil {
				logger.Warn("pull failed, retrying", "image", remoteTag, "err", err)
			}
		}
	}

	return fmt.Errorf("image %s: no resolution method available (source_dir=%q, version=%q)", localTag, sourceDir, version)
}

func ResolveUpstream(ctx context.Context, resolver Resolver, name, version, upstreamRef, buildID, registry string, logger *slog.Logger) error {
	localTag := fmt.Sprintf("agency-%s:latest", name)
	exists, err := resolver.Exists(ctx, localTag)
	if err != nil {
		return fmt.Errorf("check local image %s: %w", localTag, err)
	}
	if exists && buildID != "" {
		imgBuildID := resolver.BuildLabel(ctx, localTag)
		if imgBuildID != "" && imgBuildID == buildID {
			return nil
		}
		if imgBuildID != "" && logger != nil {
			logger.Info("image stale, re-pulling", "image", localTag, "current", imgBuildID, "want", buildID)
		}
	} else if exists && buildID == "" {
		return nil
	}

	if version != "" {
		ghcrTag := fmt.Sprintf("%s/agency-%s:v%s", registry, name, version)
		if logger != nil {
			logger.Info("pulling image from registry", "image", ghcrTag)
		}
		if err := resolver.PullAndTag(ctx, ghcrTag, localTag); err == nil {
			resolver.PruneOld(ctx, name, buildID, logger)
			return nil
		} else if logger != nil {
			logger.Warn("registry pull failed, falling back to upstream", "image", ghcrTag, "err", err)
		}
	}

	if upstreamRef != "" {
		if logger != nil {
			logger.Info("pulling image from upstream", "image", upstreamRef)
		}
		if err := resolver.PullAndTag(ctx, upstreamRef, localTag); err == nil {
			resolver.PruneOld(ctx, name, buildID, logger)
			return nil
		} else if logger != nil {
			logger.Warn("upstream pull failed", "image", upstreamRef, "err", err)
		}
	}

	return fmt.Errorf("image %s: upstream resolution failed (registry version=%q, upstream=%q)", localTag, version, upstreamRef)
}

type buildSpec struct {
	contextDir     string
	dockerfilePath string
	namedContexts  map[string]string
}

func SourceFingerprintForService(name, sourceDir string) (string, error) {
	spec, err := sourceBuildSpec(name, sourceDir)
	if err != nil {
		return "", err
	}
	return sourceFingerprint(spec.contextDir, spec.dockerfilePath, spec.namedContexts)
}

func sourceBuildSpec(name, sourceDir string) (buildSpec, error) {
	repoContextNames := map[string]bool{"intake": true}
	sharedContextNames := map[string]bool{"body": true, "comms": true, "knowledge": true, "egress": true}

	var spec buildSpec
	switch {
	case repoContextNames[name]:
		spec.contextDir = sourceDir
		spec.dockerfilePath = filepath.Join("images", name, "Dockerfile")
	case sharedContextNames[name]:
		spec.contextDir = filepath.Join(sourceDir, "images", name)
		spec.dockerfilePath = "Dockerfile"
		spec.namedContexts = map[string]string{"shared": filepath.Join(sourceDir, "images")}
	case name == "web":
		spec.contextDir = filepath.Join(sourceDir, "web")
		spec.dockerfilePath = "Dockerfile"
	default:
		spec.contextDir = filepath.Join(sourceDir, "images", name)
		spec.dockerfilePath = "Dockerfile"
	}

	if _, err := os.Stat(filepath.Join(spec.contextDir, spec.dockerfilePath)); err != nil {
		return buildSpec{}, fmt.Errorf("source build for %s: Dockerfile not found at %s/%s", name, spec.contextDir, spec.dockerfilePath)
	}
	return spec, nil
}

func sourceFingerprint(contextDir, dockerfilePath string, namedContexts map[string]string) (string, error) {
	dockerfileFullPath := filepath.Join(contextDir, dockerfilePath)
	sources, namedSources, err := dockerfileSources(dockerfileFullPath)
	if err != nil {
		return "", err
	}
	files, err := expandFingerprintPaths(contextDir, append([]string{dockerfilePath}, sources...))
	if err != nil {
		return "", err
	}
	type fingerprintFile struct {
		baseDir string
		path    string
		prefix  string
	}
	var allFiles []fingerprintFile
	for _, file := range files {
		allFiles = append(allFiles, fingerprintFile{baseDir: contextDir, path: file})
	}
	for name, ctxDir := range namedContexts {
		ctxSources := namedSources[name]
		if len(ctxSources) == 0 {
			continue
		}
		ctxFiles, err := expandFingerprintPaths(ctxDir, ctxSources)
		if err != nil {
			return "", err
		}
		for _, file := range ctxFiles {
			allFiles = append(allFiles, fingerprintFile{baseDir: ctxDir, path: file, prefix: name + ":"})
		}
	}
	sort.Slice(allFiles, func(i, j int) bool {
		left := allFiles[i].prefix + allFiles[i].path
		right := allFiles[j].prefix + allFiles[j].path
		return left < right
	})

	h := sha256.New()
	if len(namedSources) > 0 {
		h.Write([]byte("build-context-transform"))
		h.Write([]byte{0})
		h.Write([]byte(buildContextTransformVersion))
		h.Write([]byte{0})
	}
	for _, file := range allFiles {
		rel, err := filepath.Rel(file.baseDir, file.path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(file.path)
		if err != nil {
			return "", err
		}
		h.Write([]byte(file.prefix + rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func dockerfileSources(path string) ([]string, map[string][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var sources []string
	namedSources := map[string][]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		instruction := strings.ToUpper(fields[0])
		if instruction != "COPY" && instruction != "ADD" {
			continue
		}
		start := 1
		namedTarget := ""
		if strings.HasPrefix(fields[1], "--from=") {
			ctxName := strings.TrimPrefix(fields[1], "--from=")
			start = 2
			if ctxName != "shared" {
				continue
			}
			namedTarget = ctxName
		}
		for _, source := range fields[start : len(fields)-1] {
			if strings.HasPrefix(source, "--") {
				continue
			}
			trimmed := strings.Trim(source, `"'`)
			if namedTarget != "" {
				namedSources[namedTarget] = append(namedSources[namedTarget], trimmed)
				continue
			}
			sources = append(sources, trimmed)
		}
	}
	return sources, namedSources, scanner.Err()
}

func expandFingerprintPaths(contextDir string, paths []string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	for _, p := range paths {
		full := filepath.Clean(filepath.Join(contextDir, p))
		if !strings.HasPrefix(full, filepath.Clean(contextDir)+string(os.PathSeparator)) && full != filepath.Clean(contextDir) {
			return nil, fmt.Errorf("path %q escapes build context", p)
		}
		matches := []string{full}
		if strings.ContainsAny(p, "*?[") {
			globMatches, err := filepath.Glob(full)
			if err != nil {
				return nil, err
			}
			matches = globMatches
		}
		for _, match := range matches {
			if err := filepath.WalkDir(match, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				base := d.Name()
				if d.IsDir() {
					switch base {
					case ".git", "node_modules", "__pycache__", "dist", "coverage", "test-results", "playwright-report":
						return filepath.SkipDir
					}
					return nil
				}
				if seen[path] {
					return nil
				}
				seen[path] = true
				files = append(files, path)
				return nil
			}); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
		}
	}
	return files, nil
}
