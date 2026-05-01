package runtimebackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/geoffbelknap/agency/internal/pkg/pathsafety"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	defaultFirecrackerRootFSMiB = 1024
	firecrackerInitPath         = "/sbin/init-spike"
)

type FirecrackerImageStore struct {
	StateDir          string
	PodmanPath        string
	Mke2fsPath        string
	SizeMiB           int64
	VsockBridgeBinary string
	OverlayBaseDir    string
	RootFSOCIRef      string
	Platform          ocispec.Platform

	commands firecrackerImageCommands
}

type FirecrackerRootFS struct {
	ImageRef string
	Digest   string
	BasePath string
	Path     string
	InitPath string
}

type MicroVMImageStore = FirecrackerImageStore
type MicroVMRootFS = FirecrackerRootFS

type FirecrackerRootFSOverlay struct {
	HostPath  string `json:"hostPath"`
	GuestPath string `json:"guestPath"`
	Mode      string `json:"mode,omitempty"`
}

type firecrackerImageCommands interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	Run(ctx context.Context, name string, args ...string) error
	Export(ctx context.Context, podmanPath, id, stageDir string) error
}

type osFirecrackerImageCommands struct{}

func (s *FirecrackerImageStore) PrepareTaskRootFS(ctx context.Context, spec runtimecontract.RuntimeSpec) (FirecrackerRootFS, error) {
	runtimeID, err := pathsafety.Segment("firecracker runtime id", spec.RuntimeID)
	if err != nil {
		return FirecrackerRootFS{}, fmt.Errorf("firecracker image store: %w", err)
	}
	taskDir, err := pathsafety.Join(s.stateDir(), "tasks", runtimeID)
	if err != nil {
		return FirecrackerRootFS{}, err
	}
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return FirecrackerRootFS{}, fmt.Errorf("create firecracker task rootfs dir: %w", err)
	}
	taskPath, err := pathsafety.Join(taskDir, "rootfs.ext4")
	if err != nil {
		return FirecrackerRootFS{}, err
	}
	if imageRef, ok, err := s.rootFSOCIImageRef(spec.Package.Image); err != nil {
		return FirecrackerRootFS{}, err
	} else if ok {
		return s.prepareTaskRootFSFromOCI(ctx, imageRef, taskPath, spec.Package.Env)
	}
	if len(spec.Package.Env) > 0 {
		digest, err := s.resolveImageDigest(ctx, spec.Package.Image)
		if err != nil {
			return FirecrackerRootFS{}, err
		}
		if err := s.buildRootFS(ctx, spec.Package.Image, taskPath, spec.Package.Env); err != nil {
			return FirecrackerRootFS{}, err
		}
		return FirecrackerRootFS{ImageRef: spec.Package.Image, Digest: digest, Path: taskPath, InitPath: firecrackerInitPath}, nil
	}
	rootfs, err := s.Realize(ctx, spec.Package.Image)
	if err != nil {
		return FirecrackerRootFS{}, err
	}
	if err := copyFile(rootfs.BasePath, taskPath); err != nil {
		return FirecrackerRootFS{}, fmt.Errorf("copy firecracker task rootfs: %w", err)
	}
	rootfs.Path = taskPath
	return rootfs, nil
}

func (s *FirecrackerImageStore) prepareTaskRootFSFromOCI(ctx context.Context, imageRef, taskPath string, env map[string]string) (FirecrackerRootFS, error) {
	builder := &MicroVMOCIRootFSBuilder{
		StateDir:          s.stateDir(),
		Mke2fsPath:        s.mke2fsPath(),
		SizeMiB:           s.sizeMiB(),
		VsockBridgeBinary: s.VsockBridgeBinary,
		OverlayBaseDir:    s.overlayBaseDir(),
		Platform:          s.platform(),
	}
	result, err := builder.Build(ctx, imageRef, taskPath, env)
	if err != nil {
		return FirecrackerRootFS{}, fmt.Errorf("firecracker OCI rootfs: %w", err)
	}
	return FirecrackerRootFS{
		ImageRef: result.ImageRef,
		Digest:   result.Manifest.Digest.String(),
		Path:     result.RootFSPath,
		InitPath: result.InitPath,
	}, nil
}

func (s *FirecrackerImageStore) rootFSOCIImageRef(imageRef string) (string, bool, error) {
	configured := strings.TrimSpace(s.RootFSOCIRef)
	if configured != "" {
		ref, err := validateFirecrackerOCIImageRef(configured)
		return ref, true, err
	}
	imageRef = strings.TrimSpace(imageRef)
	if strings.Contains(imageRef, "@sha256:") {
		ref, err := validateFirecrackerOCIImageRef(imageRef)
		return ref, true, err
	}
	return "", false, nil
}

func validateFirecrackerOCIImageRef(imageRef string) (string, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return "", fmt.Errorf("firecracker rootfs OCI artifact is not configured")
	}
	if strings.HasSuffix(imageRef, ":latest") {
		return "", fmt.Errorf("firecracker rootfs OCI artifact must not use mutable :latest tag: %s", imageRef)
	}
	return imageRef, nil
}

func (s *FirecrackerImageStore) Realize(ctx context.Context, imageRef string) (FirecrackerRootFS, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return FirecrackerRootFS{}, fmt.Errorf("firecracker image store: image ref is required")
	}
	digest, err := s.resolveImageDigest(ctx, imageRef)
	if err != nil {
		return FirecrackerRootFS{}, err
	}
	imageDir, err := pathsafety.Join(s.stateDir(), "images")
	if err != nil {
		return FirecrackerRootFS{}, err
	}
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return FirecrackerRootFS{}, fmt.Errorf("create firecracker image cache dir: %w", err)
	}
	basePath, err := pathsafety.Join(imageDir, sanitizeFirecrackerDigest(digest)+".ext4")
	if err != nil {
		return FirecrackerRootFS{}, err
	}
	if info, err := os.Stat(basePath); err == nil && info.Size() > 0 {
		return FirecrackerRootFS{ImageRef: imageRef, Digest: digest, BasePath: basePath, Path: basePath, InitPath: firecrackerInitPath}, nil
	}
	if err := s.buildRootFS(ctx, imageRef, basePath, nil); err != nil {
		return FirecrackerRootFS{}, err
	}
	return FirecrackerRootFS{ImageRef: imageRef, Digest: digest, BasePath: basePath, Path: basePath, InitPath: firecrackerInitPath}, nil
}

func (s *FirecrackerImageStore) resolveImageDigest(ctx context.Context, imageRef string) (string, error) {
	digest, err := s.inspectDigest(ctx, imageRef, "{{.Digest}}")
	if err != nil {
		if pullErr := s.commandRunner().Run(ctx, s.podmanPath(), "pull", imageRef); pullErr != nil {
			return "", fmt.Errorf("pull OCI image %q: %w", imageRef, pullErr)
		}
		digest, err = s.inspectDigest(ctx, imageRef, "{{.Digest}}")
	}
	if err != nil || digest == "" || digest == "<no value>" {
		digest, err = s.inspectDigest(ctx, imageRef, "{{.Id}}")
	}
	if err != nil {
		return "", fmt.Errorf("inspect OCI image %q: %w", imageRef, err)
	}
	if digest == "" || digest == "<no value>" {
		return "", fmt.Errorf("inspect OCI image %q: missing digest", imageRef)
	}
	return digest, nil
}

func (s *FirecrackerImageStore) inspectDigest(ctx context.Context, imageRef, format string) (string, error) {
	out, err := s.commandRunner().Output(ctx, s.podmanPath(), "image", "inspect", "--format", format, imageRef)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *FirecrackerImageStore) buildRootFS(ctx context.Context, imageRef, outPath string, env map[string]string) error {
	tmpBase, err := pathsafety.Join(s.stateDir(), "tmp")
	if err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(tmpBase, "rootfs-*")
	if err != nil {
		if mkErr := os.MkdirAll(tmpBase, 0o755); mkErr != nil {
			return fmt.Errorf("create firecracker image temp dir: %w", mkErr)
		}
		tmpDir, err = os.MkdirTemp(tmpBase, "rootfs-*")
	}
	if err != nil {
		return fmt.Errorf("create firecracker image temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	stageDir, err := pathsafety.Join(tmpDir, "stage")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("create firecracker rootfs stage: %w", err)
	}
	idBytes, err := s.commandRunner().Output(ctx, s.podmanPath(), "create", imageRef)
	if err != nil {
		return fmt.Errorf("create OCI image export source: %w", err)
	}
	id := strings.TrimSpace(string(idBytes))
	if id == "" {
		return fmt.Errorf("create OCI image export source: empty id")
	}
	defer s.commandRunner().Run(context.Background(), s.podmanPath(), "rm", id)

	if err := s.commandRunner().Export(ctx, s.podmanPath(), id, stageDir); err != nil {
		return fmt.Errorf("export OCI image filesystem: %w", err)
	}
	if err := applyFirecrackerRootFSOverlays(stageDir, env, s.overlayBaseDir()); err != nil {
		return err
	}
	command, err := s.imageCommand(ctx, imageRef)
	if err != nil {
		return err
	}
	if err := writeFirecrackerInit(stageDir, command, firecrackerGuestEnv(env)); err != nil {
		return err
	}
	if err := installFirecrackerVsockBridge(stageDir, s.VsockBridgeBinary); err != nil {
		return err
	}
	tmpImage, err := pathsafety.Join(tmpDir, "rootfs.ext4")
	if err != nil {
		return err
	}
	if err := s.commandRunner().Run(ctx, "truncate", "-s", fmt.Sprintf("%dM", s.sizeMiB()), tmpImage); err != nil {
		return fmt.Errorf("allocate firecracker rootfs image: %w", err)
	}
	if err := s.commandRunner().Run(ctx, s.mke2fsPath(), "-q", "-t", "ext4", "-d", stageDir, tmpImage); err != nil {
		return fmt.Errorf("build firecracker ext4 rootfs: %w", err)
	}
	if err := os.Rename(tmpImage, outPath); err != nil {
		return fmt.Errorf("commit firecracker rootfs image: %w", err)
	}
	return nil
}

func FirecrackerRootFSOverlaysEnvValue(overlays []FirecrackerRootFSOverlay) (string, error) {
	data, err := json.Marshal(overlays)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func firecrackerRootFSOverlaysFromEnv(env map[string]string) ([]FirecrackerRootFSOverlay, error) {
	raw := strings.TrimSpace(env[FirecrackerRootFSOverlaysEnv])
	if raw == "" {
		return nil, nil
	}
	var overlays []FirecrackerRootFSOverlay
	if err := json.Unmarshal([]byte(raw), &overlays); err != nil {
		return nil, fmt.Errorf("parse firecracker rootfs overlays: %w", err)
	}
	return overlays, nil
}

func applyFirecrackerRootFSOverlays(stageDir string, env map[string]string, overlayBaseDir string) error {
	overlays, err := firecrackerRootFSOverlaysFromEnv(env)
	if err != nil {
		return err
	}
	for _, overlay := range overlays {
		if err := applyFirecrackerRootFSOverlay(stageDir, overlayBaseDir, overlay); err != nil {
			return err
		}
	}
	return nil
}

func applyFirecrackerRootFSOverlay(stageDir, overlayBaseDir string, overlay FirecrackerRootFSOverlay) error {
	source, err := openFirecrackerOverlaySource(overlayBaseDir, overlay.HostPath)
	if err != nil {
		return err
	}
	defer source.Close()
	guestPath := filepath.Clean(overlay.GuestPath)
	if !filepath.IsAbs(guestPath) || guestPath == string(os.PathSeparator) {
		return fmt.Errorf("firecracker rootfs overlay: guest path must be absolute")
	}
	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("stat firecracker rootfs overlay source %s: %w", source.Path(), err)
	}
	target, err := safeGuestPath(stageDir, guestPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDirFromRoot(source.root, source.rel, target)
	}
	return copyRootFileToPath(source.root, source.rel, target, info.Mode().Perm())
}

func safeGuestPath(stageDir, guestPath string) (string, error) {
	rel := strings.TrimPrefix(filepath.Clean(guestPath), string(os.PathSeparator))
	if rel == "" || rel == "." {
		return "", fmt.Errorf("firecracker rootfs overlay: guest path must be below root")
	}
	return pathsafety.Join(stageDir, strings.Split(rel, string(os.PathSeparator))...)
}

type firecrackerOverlaySource struct {
	root *os.Root
	rel  string
	path string
}

func openFirecrackerOverlaySource(baseDir, raw string) (*firecrackerOverlaySource, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("firecracker rootfs overlay: base dir is required")
	}
	baseDir, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return nil, fmt.Errorf("firecracker rootfs overlay base dir: %w", err)
	}
	hostPath := strings.TrimSpace(raw)
	hostPath, err = cleanFirecrackerHostPath("firecracker rootfs overlay host path", hostPath)
	if err != nil {
		return nil, err
	}
	if hostPath == "" {
		return nil, fmt.Errorf("firecracker rootfs overlay: host path is required")
	}
	rel, err := filepath.Rel(baseDir, hostPath)
	if err != nil {
		return nil, fmt.Errorf("firecracker rootfs overlay path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("firecracker rootfs overlay host path must be under %s", baseDir)
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, fmt.Errorf("open firecracker rootfs overlay base dir: %w", err)
	}
	return &firecrackerOverlaySource{root: root, rel: rel, path: hostPath}, nil
}

func (s *firecrackerOverlaySource) Close() error {
	return s.root.Close()
}

func (s *firecrackerOverlaySource) Path() string {
	return s.path
}

func (s *firecrackerOverlaySource) Stat() (os.FileInfo, error) {
	return s.root.Stat(s.rel)
}

func copyDirFromRoot(root *os.Root, src, dst string) error {
	return fs.WalkDir(root.FS(), src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := root.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		}
		return copyRootFileToPath(root, path, target, info.Mode().Perm())
	})
}

func copyRootFileToPath(root *os.Root, src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := root.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (s *FirecrackerImageStore) imageCommand(ctx context.Context, imageRef string) ([]string, error) {
	out, err := s.commandRunner().Output(ctx, s.podmanPath(), "image", "inspect", "--format", "{{json .Config.Entrypoint}}|{{json .Config.Cmd}}", imageRef)
	if err != nil {
		return nil, fmt.Errorf("inspect OCI image command %q: %w", imageRef, err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("inspect OCI image command %q: unexpected output %q", imageRef, strings.TrimSpace(string(out)))
	}
	entrypoint, err := parseOCICommandPart(parts[0])
	if err != nil {
		return nil, fmt.Errorf("parse OCI image entrypoint: %w", err)
	}
	cmd, err := parseOCICommandPart(parts[1])
	if err != nil {
		return nil, fmt.Errorf("parse OCI image cmd: %w", err)
	}
	return append(entrypoint, cmd...), nil
}

func parseOCICommandPart(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		return list, nil
	}
	var shell string
	if err := json.Unmarshal([]byte(raw), &shell); err != nil {
		return nil, err
	}
	if strings.TrimSpace(shell) == "" {
		return nil, nil
	}
	return []string{"/bin/sh", "-c", shell}, nil
}

func writeFirecrackerInit(stageDir string, command []string, env map[string]string) error {
	path, err := safeGuestPath(stageDir, firecrackerInitPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create firecracker init dir: %w", err)
	}
	var commandLine string
	if len(command) > 0 {
		quoted := make([]string, 0, len(command))
		for _, arg := range command {
			quoted = append(quoted, shellQuote(arg))
		}
		commandLine = "set -- " + strings.Join(quoted, " ") + "\n"
	}
	envLines := firecrackerInitEnvLines(env)
	script := "#!/bin/sh\nset -eu\nmount -t proc proc /proc || true\nmount -t sysfs sysfs /sys || true\n" + envLines + "if [ -x /usr/local/bin/agency-vsock-http-bridge ]; then\n  /usr/local/bin/agency-vsock-http-bridge &\nfi\n" + commandLine + "if [ \"$#\" -gt 0 ]; then\n  exec \"$@\"\nfi\nexec /bin/sh\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write firecracker init: %w", err)
	}
	return nil
}

func firecrackerInitEnvLines(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		if validShellEnvName(key) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString("export ")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(shellQuote(env[key]))
		b.WriteString("\n")
	}
	return b.String()
}

func validShellEnvName(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z' && i > 0:
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func installFirecrackerVsockBridge(stageDir, binaryPath string) error {
	binaryPath = strings.TrimSpace(binaryPath)
	if binaryPath == "" {
		return nil
	}
	binaryPath, err := cleanFirecrackerHostPath("firecracker vsock bridge binary path", binaryPath)
	if err != nil {
		return err
	}
	target, err := pathsafety.Join(stageDir, "usr", "local", "bin", "agency-vsock-http-bridge")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create firecracker vsock bridge dir: %w", err)
	}
	if err := copyFile(binaryPath, target); err != nil {
		return fmt.Errorf("install firecracker vsock bridge: %w", err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return fmt.Errorf("chmod firecracker vsock bridge: %w", err)
	}
	return nil
}

func cleanFirecrackerHostPath(kind, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("%s contains NUL", kind)
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s must be absolute", kind)
	}
	return filepath.Clean(value), nil
}

func (s *FirecrackerImageStore) stateDir() string {
	if strings.TrimSpace(s.StateDir) != "" {
		return s.StateDir
	}
	return filepath.Join(os.TempDir(), "agency-firecracker")
}

func (s *FirecrackerImageStore) overlayBaseDir() string {
	if strings.TrimSpace(s.OverlayBaseDir) != "" {
		return s.OverlayBaseDir
	}
	return s.stateDir()
}

func (s *FirecrackerImageStore) platform() ocispec.Platform {
	if s.Platform.OS != "" && s.Platform.Architecture != "" {
		return s.Platform
	}
	return ocispec.Platform{OS: "linux", Architecture: "amd64"}
}

func (s *FirecrackerImageStore) podmanPath() string {
	if strings.TrimSpace(s.PodmanPath) != "" {
		return s.PodmanPath
	}
	return "podman"
}

func (s *FirecrackerImageStore) mke2fsPath() string {
	if strings.TrimSpace(s.Mke2fsPath) != "" {
		return s.Mke2fsPath
	}
	return "mke2fs"
}

func (s *FirecrackerImageStore) sizeMiB() int64 {
	if s.SizeMiB > 0 {
		return s.SizeMiB
	}
	return defaultFirecrackerRootFSMiB
}

func (s *FirecrackerImageStore) commandRunner() firecrackerImageCommands {
	if s.commands != nil {
		return s.commands
	}
	return osFirecrackerImageCommands{}
}

func sanitizeFirecrackerDigest(digest string) string {
	replacer := strings.NewReplacer(":", "-", "/", "-", "@", "-", "\\", "-")
	return replacer.Replace(strings.TrimSpace(digest))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		if closeErr := out.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return out.Close()
}

func (osFirecrackerImageCommands) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (osFirecrackerImageCommands) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func (osFirecrackerImageCommands) Export(ctx context.Context, podmanPath, id, stageDir string) error {
	podman := exec.CommandContext(ctx, podmanPath, "export", id)
	tar := exec.CommandContext(ctx, "tar", "-xf", "-", "-C", stageDir)
	pipe, err := podman.StdoutPipe()
	if err != nil {
		return err
	}
	tar.Stdin = pipe
	tar.Stderr = os.Stderr
	podman.Stderr = os.Stderr
	if err := tar.Start(); err != nil {
		return err
	}
	if err := podman.Start(); err != nil {
		tar.Wait()
		return err
	}
	podmanErr := podman.Wait()
	tarErr := tar.Wait()
	if podmanErr != nil {
		return podmanErr
	}
	return tarErr
}
