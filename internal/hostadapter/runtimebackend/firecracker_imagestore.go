package runtimebackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
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

	commands firecrackerImageCommands
}

type FirecrackerRootFS struct {
	ImageRef string
	Digest   string
	BasePath string
	Path     string
	InitPath string
}

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
	if strings.TrimSpace(spec.RuntimeID) == "" {
		return FirecrackerRootFS{}, fmt.Errorf("firecracker image store: runtime id is required")
	}
	taskDir := filepath.Join(s.stateDir(), "tasks", spec.RuntimeID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return FirecrackerRootFS{}, fmt.Errorf("create firecracker task rootfs dir: %w", err)
	}
	taskPath := filepath.Join(taskDir, "rootfs.ext4")
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

func (s *FirecrackerImageStore) Realize(ctx context.Context, imageRef string) (FirecrackerRootFS, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return FirecrackerRootFS{}, fmt.Errorf("firecracker image store: image ref is required")
	}
	digest, err := s.resolveImageDigest(ctx, imageRef)
	if err != nil {
		return FirecrackerRootFS{}, err
	}
	imageDir := filepath.Join(s.stateDir(), "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return FirecrackerRootFS{}, fmt.Errorf("create firecracker image cache dir: %w", err)
	}
	basePath := filepath.Join(imageDir, sanitizeFirecrackerDigest(digest)+".ext4")
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
	tmpDir, err := os.MkdirTemp(filepath.Join(s.stateDir(), "tmp"), "rootfs-*")
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Join(s.stateDir(), "tmp"), 0o755); mkErr != nil {
			return fmt.Errorf("create firecracker image temp dir: %w", mkErr)
		}
		tmpDir, err = os.MkdirTemp(filepath.Join(s.stateDir(), "tmp"), "rootfs-*")
	}
	if err != nil {
		return fmt.Errorf("create firecracker image temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	stageDir := filepath.Join(tmpDir, "stage")
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
	if err := applyFirecrackerRootFSOverlays(stageDir, env); err != nil {
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
	tmpImage := filepath.Join(tmpDir, "rootfs.ext4")
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

func applyFirecrackerRootFSOverlays(stageDir string, env map[string]string) error {
	overlays, err := firecrackerRootFSOverlaysFromEnv(env)
	if err != nil {
		return err
	}
	for _, overlay := range overlays {
		if err := applyFirecrackerRootFSOverlay(stageDir, overlay); err != nil {
			return err
		}
	}
	return nil
}

func applyFirecrackerRootFSOverlay(stageDir string, overlay FirecrackerRootFSOverlay) error {
	hostPath := strings.TrimSpace(overlay.HostPath)
	if hostPath == "" {
		return fmt.Errorf("firecracker rootfs overlay: host path is required")
	}
	guestPath := filepath.Clean(overlay.GuestPath)
	if !filepath.IsAbs(guestPath) || guestPath == string(os.PathSeparator) {
		return fmt.Errorf("firecracker rootfs overlay: guest path must be absolute")
	}
	info, err := os.Stat(hostPath)
	if err != nil {
		return fmt.Errorf("stat firecracker rootfs overlay source %s: %w", hostPath, err)
	}
	target := filepath.Join(stageDir, strings.TrimPrefix(guestPath, string(os.PathSeparator)))
	if info.IsDir() {
		return copyDir(hostPath, target)
	}
	return copyFileToPath(hostPath, target, info.Mode().Perm())
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
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
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		}
		return copyFileToPath(path, target, info.Mode().Perm())
	})
}

func copyFileToPath(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
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
	path := filepath.Join(stageDir, strings.TrimPrefix(firecrackerInitPath, "/"))
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
	target := filepath.Join(stageDir, "usr", "local", "bin", "agency-vsock-http-bridge")
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

func (s *FirecrackerImageStore) stateDir() string {
	if strings.TrimSpace(s.StateDir) != "" {
		return s.StateDir
	}
	return filepath.Join(os.TempDir(), "agency-firecracker")
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
		out.Close()
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
