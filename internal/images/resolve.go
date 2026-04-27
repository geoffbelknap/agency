package images

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"log/slog"
)

const registry = "ghcr.io/geoffbelknap"

const (
	OllamaUpstream = "ollama/ollama"
	OllamaVersion  = "0.9.3"
)

const buildContextTransformVersion = "named-context-dir-slash-v2"

var sourceImageDependencies = map[string][]string{
	"body":      {"python-base"},
	"comms":     {"python-base"},
	"knowledge": {"python-base"},
	"intake":    {"python-base"},
	"workspace": {"workspace-base"},
}

// Resolve ensures the container image for the named service is available locally.
//
// Resolution order:
//  1. Source tree build (if sourceDir is set — dev mode). Failure is fatal.
//  2. GHCR pull (release mode)
func Resolve(ctx context.Context, cli *runtimehost.RawClient, name, version, sourceDir, buildID string, logger *slog.Logger) error {
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

	// Check if existing local image is current — skip rebuild if buildID matches.
	exists, err := ImageExists(ctx, cli, localTag)
	if err != nil {
		return fmt.Errorf("check local image %s: %w", localTag, err)
	}
	if exists && sourceHash != "" {
		imgSourceHash := ImageLabel(ctx, cli, localTag, "agency.source.hash")
		if imgSourceHash != "" && imgSourceHash == sourceHash {
			return nil // Image source is current — skip rebuild even if gateway build ID changed.
		}
		if imgSourceHash != "" {
			logger.Info("image source stale, rebuilding", "image", localTag, "current", imgSourceHash, "want", sourceHash)
		}
	} else if exists && buildID != "" {
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
		for _, dep := range sourceImageDependencies[name] {
			if err := Resolve(ctx, cli, dep, version, sourceDir, buildID, logger); err != nil {
				return fmt.Errorf("resolve source dependency %s for %s: %w", dep, localTag, err)
			}
		}
		if err := BuildFromSource(ctx, cli, name, sourceDir, localTag, buildID, sourceHash, logger); err != nil {
			return fmt.Errorf("image %s: source build failed: %w", localTag, err)
		}
		// Also tag with buildID so old images are identifiable for cleanup
		if buildID != "" {
			versionTag := fmt.Sprintf("agency-%s:%s", name, buildID)
			cli.ImageTag(ctx, localTag, versionTag)
		}
		// Prune old images for this service (keep only current)
		PruneOldImages(ctx, cli, name, buildID, logger)
		return nil
	}

	// Release mode: GHCR pull
	if version != "" && version != "dev" {
		remoteTag := fmt.Sprintf("%s/agency-%s:v%s", registry, name, version)
		logger.Info("pulling image", "image", remoteTag)
		for attempt := 0; attempt < 2; attempt++ {
			if err := PullAndTag(ctx, cli, remoteTag, localTag); err == nil {
				// Prune old images now that we have a fresh pull
				PruneOldImages(ctx, cli, name, buildID, logger)
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
func ResolveUpstream(ctx context.Context, cli *runtimehost.RawClient, name, version, upstreamRef, buildID string, logger *slog.Logger) error {
	localTag := fmt.Sprintf("agency-%s:latest", name)

	// Check if existing local image is current — skip pull if buildID matches.
	exists, err := ImageExists(ctx, cli, localTag)
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
		if err := PullAndTag(ctx, cli, ghcrTag, localTag); err == nil {
			PruneOldImages(ctx, cli, name, buildID, logger)
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
		if err := PullAndTag(ctx, cli, upstreamRef, localTag); err == nil {
			PruneOldImages(ctx, cli, name, buildID, logger)
			return nil
		} else if logger != nil {
			logger.Warn("upstream pull failed", "image", upstreamRef, "err", err)
		}
	}

	return fmt.Errorf("image %s: upstream resolution failed (ghcr version=%q, upstream=%q)", localTag, version, upstreamRef)
}

type buildSpec struct {
	contextDir     string
	dockerfilePath string
	namedContexts  map[string]string
}

func sourceBuildSpec(name, sourceDir string) (buildSpec, error) {
	// Services that still need the repo root as build context.
	repoContextNames := map[string]bool{
		"intake": true,
	}

	// Services that build from a self-contained context plus shared image assets.
	sharedContextNames := map[string]bool{
		"body": true, "comms": true, "knowledge": true, "egress": true,
	}

	var spec buildSpec

	switch {
	case repoContextNames[name]:
		// Build context is repo root; Dockerfile is in images/{name}/.
		spec.contextDir = sourceDir
		spec.dockerfilePath = filepath.Join("images", name, "Dockerfile")
	case sharedContextNames[name]:
		// Build context is the image directory itself, with shared files sourced
		// from the top-level images/ directory via a named context.
		spec.contextDir = filepath.Join(sourceDir, "images", name)
		spec.dockerfilePath = "Dockerfile"
		spec.namedContexts = map[string]string{
			"shared": filepath.Join(sourceDir, "images"),
		}
	case name == "web":
		// agency-web lives in the top-level web/ directory rather than images/.
		spec.contextDir = filepath.Join(sourceDir, "web")
		spec.dockerfilePath = "Dockerfile"
	default:
		// Self-contained: build context is the image directory itself
		spec.contextDir = filepath.Join(sourceDir, "images", name)
		spec.dockerfilePath = "Dockerfile"
	}

	if _, err := os.Stat(filepath.Join(spec.contextDir, spec.dockerfilePath)); err != nil {
		return buildSpec{}, fmt.Errorf("source build for %s: Dockerfile not found at %s/%s", name, spec.contextDir, spec.dockerfilePath)
	}

	return spec, nil
}

// buildFromSource builds an image directly from the source tree.
func BuildFromSource(ctx context.Context, cli *runtimehost.RawClient, name, sourceDir, tag, buildID, sourceHash string, logger *slog.Logger) error {
	spec, err := sourceBuildSpec(name, sourceDir)
	if err != nil {
		return err
	}

	logger.Info("building image from source", "image", tag, "context", spec.contextDir, "source_hash", sourceHash)
	buildArgs := map[string]*string{}
	if buildID != "" {
		buildArgs["BUILD_ID"] = &buildID
	}
	if sourceHash != "" {
		buildArgs["SOURCE_HASH"] = &sourceHash
	}
	if len(spec.namedContexts) == 0 {
		contextDir, dockerfilePath := spec.contextDir, spec.dockerfilePath
		if cli != nil && cli.Backend() == runtimehost.BackendAppleContainer {
			var err error
			if name == "web" {
				contextDir, dockerfilePath, err = prepareAppleWebBuildContext(ctx, spec.contextDir, buildID)
			} else {
				contextDir, dockerfilePath, err = prepareAppleBuildContext(spec)
			}
			if err != nil {
				return err
			}
			defer os.RemoveAll(contextDir)
		}
		return dockerBuild(ctx, cli, contextDir, dockerfilePath, tag, buildArgs)
	}

	contextDir, dockerfilePath, err := prepareBuildContext(spec)
	if err != nil {
		return err
	}
	defer os.RemoveAll(contextDir)
	if cli != nil && cli.Backend() == runtimehost.BackendAppleContainer {
		if err := rewriteAppleBuildContextDockerfile(contextDir, dockerfilePath); err != nil {
			return err
		}
	}

	return dockerBuild(ctx, cli, contextDir, dockerfilePath, tag, buildArgs)
}

// SourceFingerprintForService returns the source fingerprint for a buildable service
// using the same source selection logic as image resolution.
func SourceFingerprintForService(name, sourceDir string) (string, error) {
	spec, err := sourceBuildSpec(name, sourceDir)
	if err != nil {
		return "", err
	}
	return sourceFingerprint(spec.contextDir, spec.dockerfilePath, spec.namedContexts)
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

func prepareBuildContext(spec buildSpec) (string, string, error) {
	tempDir, err := os.MkdirTemp("", "agency-image-build-*")
	if err != nil {
		return "", "", err
	}
	if err := copyDirContents(spec.contextDir, tempDir); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}

	defaultSources, namedSources, err := dockerfileSources(filepath.Join(spec.contextDir, spec.dockerfilePath))
	if err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	_ = defaultSources
	for name, ctxDir := range spec.namedContexts {
		sources := namedSources[name]
		if len(sources) == 0 {
			continue
		}
		if err := copySelectedPaths(ctxDir, filepath.Join(tempDir, "_ctx_"+name), sources); err != nil {
			os.RemoveAll(tempDir)
			return "", "", err
		}
	}

	originalDockerfile, err := os.ReadFile(filepath.Join(tempDir, spec.dockerfilePath))
	if err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	rewritten := rewriteDockerfileForNamedContexts(string(originalDockerfile), spec.namedContexts)
	if err := os.WriteFile(filepath.Join(tempDir, spec.dockerfilePath), []byte(rewritten), 0o644); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}

	return tempDir, spec.dockerfilePath, nil
}

func prepareAppleBuildContext(spec buildSpec) (string, string, error) {
	tempDir, err := os.MkdirTemp("", "agency-apple-image-build-*")
	if err != nil {
		return "", "", err
	}
	if err := copyDirContents(spec.contextDir, tempDir); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	if err := rewriteAppleBuildContextDockerfile(tempDir, spec.dockerfilePath); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	return tempDir, spec.dockerfilePath, nil
}

func prepareAppleWebBuildContext(ctx context.Context, webDir, buildID string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "npm", "run", "build")
	cmd.Dir = webDir
	cmd.Env = os.Environ()
	if buildID != "" {
		cmd.Env = append(cmd.Env, "VITE_BUILD_ID="+buildID)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("build web dist: %w: %s", err, strings.TrimSpace(string(output)))
	}

	tempDir, err := os.MkdirTemp("", "agency-apple-web-build-*")
	if err != nil {
		return "", "", err
	}
	for _, name := range []string{"nginx.conf", "agency-entrypoint.sh"} {
		if err := copyFile(filepath.Join(webDir, name), filepath.Join(tempDir, name)); err != nil {
			os.RemoveAll(tempDir)
			return "", "", err
		}
	}
	if err := copyDirContents(filepath.Join(webDir, "dist"), filepath.Join(tempDir, "dist")); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "Dockerfile"), []byte(appleWebDockerfile), 0o644); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	if err := rewriteAppleBuildContextDockerfile(tempDir, "Dockerfile"); err != nil {
		os.RemoveAll(tempDir)
		return "", "", err
	}
	return tempDir, "Dockerfile", nil
}

func rewriteAppleBuildContextDockerfile(contextDir, dockerfilePath string) error {
	path := filepath.Join(contextDir, dockerfilePath)
	originalDockerfile, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rewritten, err := rewriteDockerfileForAppleDirectoryCopies(string(originalDockerfile), contextDir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
		return err
	}
	return nil
}

const appleWebDockerfile = `FROM alpine:3.21

ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown

RUN apk add --no-cache nginx \
    && addgroup -g 101 -S nginx 2>/dev/null || true \
    && adduser -S -D -H -u 101 -h /var/cache/nginx -s /sbin/nologin -G nginx nginx 2>/dev/null || true \
    && mkdir -p /etc/nginx/conf.d /var/cache/nginx \
    && rm -f /etc/nginx/http.d/default.conf

RUN printf 'worker_processes auto;\n\
pid /tmp/nginx.pid;\n\
error_log /dev/stderr warn;\n\
events { worker_connections 1024; }\n\
http {\n\
    include /etc/nginx/mime.types;\n\
    default_type application/octet-stream;\n\
    log_format agency escape=json '"'"'{"ts":"$time_iso8601","level":"info","component":"web","msg":"access","method":"$request_method","path":"$uri","status":$status,"duration_s":$request_time,"bytes":$bytes_sent,"remote":"$remote_addr"}'"'"';\n\
    access_log /dev/stdout agency;\n\
    sendfile on;\n\
    keepalive_timeout 65;\n\
    client_body_temp_path /tmp/client_temp;\n\
    proxy_temp_path /tmp/proxy_temp;\n\
    fastcgi_temp_path /tmp/fastcgi_temp;\n\
    uwsgi_temp_path /tmp/uwsgi_temp;\n\
    scgi_temp_path /tmp/scgi_temp;\n\
    include /etc/nginx/conf.d/*.conf;\n\
}\n' > /etc/nginx/nginx.conf

COPY nginx.conf /etc/nginx/conf.d/agency-web.conf
COPY dist /usr/share/nginx/html
COPY agency-entrypoint.sh /agency-entrypoint.sh
RUN chmod 0644 /etc/nginx/conf.d/agency-web.conf \
    && chmod 0755 /agency-entrypoint.sh

LABEL agency.build.id="${BUILD_ID}"
LABEL agency.source.hash="${SOURCE_HASH}"

USER nginx
EXPOSE 8280

HEALTHCHECK --interval=5s --timeout=2s --start-period=2s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8280/health || exit 1

ENTRYPOINT ["/agency-entrypoint.sh"]
`

func copyDirContents(srcDir, dstDir string) error {
	ignorePatterns := dockerIgnorePatterns(srcDir)
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if matchesIgnore(rel, info.IsDir(), ignorePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode())
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode())
	})
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}

func copySelectedPaths(srcDir, dstDir string, paths []string) error {
	files, err := expandFingerprintPaths(srcDir, paths)
	if err != nil {
		return err
	}
	for _, file := range files {
		rel, err := filepath.Rel(srcDir, file)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func rewriteDockerfileForNamedContexts(content string, namedContexts map[string]string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 4 {
			continue
		}
		instruction := strings.ToUpper(fields[0])
		if instruction != "COPY" && instruction != "ADD" {
			continue
		}
		if !strings.HasPrefix(fields[1], "--from=") {
			continue
		}
		ctxName := strings.TrimPrefix(fields[1], "--from=")
		if _, ok := namedContexts[ctxName]; !ok {
			continue
		}
		rewritten := []string{fields[0]}
		for _, source := range fields[2 : len(fields)-1] {
			if strings.HasPrefix(source, "--") {
				rewritten = append(rewritten, source)
				continue
			}
			trimmed := strings.Trim(source, `"'`)
			rewrittenSource := filepath.ToSlash(filepath.Join("_ctx_"+ctxName, trimmed))
			if strings.HasSuffix(trimmed, "/") && !strings.HasSuffix(rewrittenSource, "/") {
				rewrittenSource += "/"
			}
			rewritten = append(rewritten, rewrittenSource)
		}
		rewritten = append(rewritten, fields[len(fields)-1])
		lines[i] = strings.Join(rewritten, " ")
	}
	return strings.Join(lines, "\n")
}

func rewriteDockerfileForAppleDirectoryCopies(content, contextDir string) (string, error) {
	ignorePatterns := dockerIgnorePatterns(contextDir)
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		rewritten, expanded, err := expandAppleDirectoryCopyLine(line, contextDir, ignorePatterns)
		if err != nil {
			return "", err
		}
		if expanded {
			out = append(out, rewritten...)
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), nil
}

func expandAppleDirectoryCopyLine(line, contextDir string, ignorePatterns []string) ([]string, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil, false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 3 {
		return nil, false, nil
	}
	instruction := strings.ToUpper(fields[0])
	if instruction != "COPY" && instruction != "ADD" {
		return nil, false, nil
	}

	idx := 1
	var flags []string
	for idx < len(fields)-2 && strings.HasPrefix(fields[idx], "--") {
		if strings.HasPrefix(fields[idx], "--from=") {
			return nil, false, nil
		}
		flags = append(flags, fields[idx])
		idx++
	}
	if len(fields)-idx < 2 {
		return nil, false, nil
	}
	sources := fields[idx : len(fields)-1]
	dest := strings.Trim(fields[len(fields)-1], `"'`)

	var expanded []string
	changed := false
	for _, source := range sources {
		cleanSource := strings.Trim(source, `"'`)
		if cleanSource == "" || strings.ContainsAny(cleanSource, "*?[") {
			continue
		}
		fullSource := filepath.Clean(filepath.Join(contextDir, cleanSource))
		if !strings.HasPrefix(fullSource, filepath.Clean(contextDir)+string(os.PathSeparator)) && fullSource != filepath.Clean(contextDir) {
			return nil, false, fmt.Errorf("path %q escapes build context", source)
		}
		info, err := os.Stat(fullSource)
		if err != nil || !info.IsDir() {
			continue
		}
		files, err := appleDirectoryCopyFiles(contextDir, cleanSource, ignorePatterns)
		if err != nil {
			return nil, false, err
		}
		for _, rel := range files {
			target := appleDirectoryCopyTarget(cleanSource, rel, dest, len(sources) > 1)
			parts := append([]string{fields[0]}, flags...)
			parts = append(parts, filepath.ToSlash(filepath.Join(cleanSource, rel)), target)
			expanded = append(expanded, strings.Join(parts, " "))
		}
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	return expanded, true, nil
}

func appleDirectoryCopyFiles(contextDir, source string, ignorePatterns []string) ([]string, error) {
	source = strings.TrimSuffix(strings.Trim(source, `"'`), "/")
	root := filepath.Clean(filepath.Join(contextDir, source))
	var files []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relToContext, err := filepath.Rel(contextDir, path)
		if err != nil {
			return err
		}
		if relToContext == "." {
			return nil
		}
		if matchesIgnore(relToContext, d.IsDir(), ignorePatterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".pyc") {
			return nil
		}
		relToSource, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relToSource))
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func appleDirectoryCopyTarget(source, rel, dest string, multipleSources bool) string {
	dest = strings.Trim(dest, `"'`)
	rel = filepath.ToSlash(rel)
	if multipleSources {
		base := filepath.Base(strings.TrimSuffix(source, "/"))
		return filepath.ToSlash(filepath.Join(dest, base, rel))
	}
	return filepath.ToSlash(filepath.Join(dest, rel))
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
			if err := filepath.WalkDir(match, func(path string, d os.DirEntry, err error) error {
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

func ImageExists(ctx context.Context, cli *runtimehost.RawClient, ref string) (bool, error) {
	if cli == nil {
		return false, fmt.Errorf("no image backend client")
	}
	_, _, err := cli.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		if runtimehost.IsErrNotFound(err) {
			return false, nil
		}
		if strings.Contains(err.Error(), "No such image") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func imageExists(ctx context.Context, cli *runtimehost.RawClient, ref string) (bool, error) {
	return ImageExists(ctx, cli, ref)
}

func PullAndTag(ctx context.Context, cli *runtimehost.RawClient, remoteRef, localTag string) error {
	reader, err := cli.ImagePull(ctx, remoteRef, runtimehost.ImagePullOptions{})
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
func ImageBuildLabel(ctx context.Context, cli *runtimehost.RawClient, ref string) string {
	return ImageLabel(ctx, cli, ref, "agency.build.id")
}

// ImageLabel reads a Docker image label. Returns empty string if the image
// cannot be inspected or the label is absent.
func ImageLabel(ctx context.Context, cli *runtimehost.RawClient, ref, key string) string {
	inspect, _, err := cli.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		return ""
	}
	return inspect.Config.Labels[key]
}

func dockerBuild(ctx context.Context, cli *runtimehost.RawClient, contextDir, dockerfile, tag string, buildArgs map[string]*string) error {
	tarReader, err := createTar(contextDir)
	if err != nil {
		return fmt.Errorf("create build context tar: %w", err)
	}
	defer tarReader.Close()

	platform := defaultBuildPlatform()
	addDefaultPlatformBuildArgs(buildArgs, platform)

	resp, err := cli.ImageBuild(ctx, tarReader, runtimehost.ImageBuildOptions{
		Dockerfile:  dockerfile,
		Tags:        []string{tag},
		Remove:      true,
		ForceRemove: true,
		BuildArgs:   buildArgs,
		Platform:    defaultBuildPlatform(),
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

func addDefaultPlatformBuildArgs(buildArgs map[string]*string, platform string) {
	if buildArgs == nil {
		return
	}
	targetOS, targetArch, ok := strings.Cut(platform, "/")
	if !ok || targetOS == "" || targetArch == "" {
		return
	}
	defaults := map[string]string{
		"BUILDPLATFORM":  platform,
		"TARGETPLATFORM": platform,
		"TARGETOS":       targetOS,
		"TARGETARCH":     targetArch,
	}
	for name, value := range defaults {
		if _, exists := buildArgs[name]; !exists {
			v := value
			buildArgs[name] = &v
		}
	}
}

func defaultBuildPlatform() string {
	if platform := strings.TrimSpace(os.Getenv("DOCKER_DEFAULT_PLATFORM")); platform != "" {
		return platform
	}
	return "linux/" + runtime.GOARCH
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
func PruneOldImages(ctx context.Context, cli *runtimehost.RawClient, name, currentBuildID string, logger *slog.Logger) {
	if cli == nil {
		return
	}
	prefix := fmt.Sprintf("agency-%s:", name)
	currentLatest := prefix + "latest"
	currentVersionTag := prefix + currentBuildID

	images, err := cli.ImageList(ctx, runtimehost.ImageListOptions{})
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
			_, err := cli.ImageRemove(ctx, tag, runtimehost.ImageRemoveOptions{PruneChildren: true})
			if err == nil {
				logger.Info("pruned old image", "image", tag)
			}
		}
	}
}
