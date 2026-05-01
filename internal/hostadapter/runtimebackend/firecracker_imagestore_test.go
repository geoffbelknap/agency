package runtimebackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestFirecrackerImageStoreRealizeBuildsAndCachesRootFS(t *testing.T) {
	stateDir := t.TempDir()
	commands := &fakeFirecrackerImageCommands{
		outputs: map[string][]byte{
			"podman image inspect --format {{.Digest}} agency-body:latest":                                      []byte("sha256:abc123\n"),
			"podman image inspect --format {{json .Config.Entrypoint}}|{{json .Config.Cmd}} agency-body:latest": []byte("null|[\"/app/entrypoint.sh\"]\n"),
			"podman create agency-body:latest":                                                                  []byte("source-id\n"),
		},
	}
	store := &FirecrackerImageStore{StateDir: stateDir, SizeMiB: 64, commands: commands}

	rootfs, err := store.Realize(context.Background(), "agency-body:latest")
	if err != nil {
		t.Fatalf("Realize returned error: %v", err)
	}
	if rootfs.Digest != "sha256:abc123" {
		t.Fatalf("digest = %q", rootfs.Digest)
	}
	wantBase := filepath.Join(stateDir, "images", "sha256-abc123.ext4")
	if rootfs.BasePath != wantBase || rootfs.Path != wantBase {
		t.Fatalf("rootfs paths = %#v, want %s", rootfs, wantBase)
	}
	if rootfs.InitPath != firecrackerInitPath {
		t.Fatalf("init path = %q, want %q", rootfs.InitPath, firecrackerInitPath)
	}
	if _, err := os.Stat(wantBase); err != nil {
		t.Fatalf("base rootfs missing: %v", err)
	}
	if !commands.exported["source-id"] {
		t.Fatal("expected image filesystem export")
	}
	if !commands.sawRun("truncate", "-s", "64M") {
		t.Fatalf("expected truncate command, got %#v", commands.runs)
	}
	if !commands.sawRun("mke2fs", "-q", "-t", "ext4") {
		t.Fatalf("expected mke2fs command, got %#v", commands.runs)
	}

	commands.reset()
	rootfs, err = store.Realize(context.Background(), "agency-body:latest")
	if err != nil {
		t.Fatalf("Realize cache hit returned error: %v", err)
	}
	if rootfs.BasePath != wantBase {
		t.Fatalf("cache hit base path = %q, want %q", rootfs.BasePath, wantBase)
	}
	if commands.exported["source-id"] {
		t.Fatal("cache hit rebuilt rootfs")
	}
}

func TestFirecrackerImageStorePullsWhenInspectMisses(t *testing.T) {
	commands := &fakeFirecrackerImageCommands{
		outputs: map[string][]byte{
			"podman image inspect --format {{.Id}} ghcr.io/example/agent:latest":                                          []byte("sha256:fallback\n"),
			"podman image inspect --format {{json .Config.Entrypoint}}|{{json .Config.Cmd}} ghcr.io/example/agent:latest": []byte("[\"/bin/agent\"]|[\"--serve\"]\n"),
			"podman create ghcr.io/example/agent:latest":                                                                  []byte("source-id\n"),
		},
		outputErrs: map[string]error{
			"podman image inspect --format {{.Digest}} ghcr.io/example/agent:latest": fmt.Errorf("not found"),
		},
	}
	store := &FirecrackerImageStore{StateDir: t.TempDir(), commands: commands}

	rootfs, err := store.Realize(context.Background(), "ghcr.io/example/agent:latest")
	if err != nil {
		t.Fatalf("Realize returned error: %v", err)
	}
	if rootfs.Digest != "sha256:fallback" {
		t.Fatalf("digest = %q, want fallback id", rootfs.Digest)
	}
	if !commands.sawRun("podman", "pull", "ghcr.io/example/agent:latest") {
		t.Fatalf("expected pull, got %#v", commands.runs)
	}
}

func TestFirecrackerImageStoreRootFSOCIRefSelection(t *testing.T) {
	store := &FirecrackerImageStore{RootFSOCIRef: "ghcr.io/example/agency-runtime-body:v1"}
	ref, ok, err := store.rootFSOCIImageRef("agency-body:latest")
	if err != nil {
		t.Fatalf("rootFSOCIImageRef returned error: %v", err)
	}
	if !ok || ref != "ghcr.io/example/agency-runtime-body:v1" {
		t.Fatalf("rootFSOCIImageRef = %q, %v", ref, ok)
	}

	store.RootFSOCIRef = ""
	ref, ok, err = store.rootFSOCIImageRef("ghcr.io/example/agency-runtime-body@sha256:abc123")
	if err != nil {
		t.Fatalf("digest rootFSOCIImageRef returned error: %v", err)
	}
	if !ok || ref != "ghcr.io/example/agency-runtime-body@sha256:abc123" {
		t.Fatalf("digest rootFSOCIImageRef = %q, %v", ref, ok)
	}

	ref, ok, err = store.rootFSOCIImageRef("agency-body:latest")
	if err != nil {
		t.Fatalf("legacy rootFSOCIImageRef returned error: %v", err)
	}
	if ok || ref != "" {
		t.Fatalf("legacy rootFSOCIImageRef = %q, %v; want no OCI path", ref, ok)
	}
}

func TestFirecrackerImageStoreRejectsMutableOCIRef(t *testing.T) {
	store := &FirecrackerImageStore{RootFSOCIRef: "ghcr.io/example/agency-runtime-body:latest"}
	_, _, err := store.rootFSOCIImageRef("agency-body:latest")
	if err == nil || !strings.Contains(err.Error(), "must not use mutable :latest") {
		t.Fatalf("rootFSOCIImageRef error = %v", err)
	}
}

func TestFirecrackerImageStorePrepareTaskRootFSCopiesBase(t *testing.T) {
	stateDir := t.TempDir()
	commands := &fakeFirecrackerImageCommands{
		outputs: map[string][]byte{
			"podman image inspect --format {{.Digest}} agency-body:latest":                                      []byte("sha256:abc123\n"),
			"podman image inspect --format {{json .Config.Entrypoint}}|{{json .Config.Cmd}} agency-body:latest": []byte("null|[\"/app/entrypoint.sh\"]\n"),
			"podman create agency-body:latest":                                                                  []byte("source-id\n"),
		},
	}
	store := &FirecrackerImageStore{StateDir: stateDir, commands: commands}
	rootfs, err := store.PrepareTaskRootFS(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package:   runtimecontract.RuntimePackageSpec{Image: "agency-body:latest"},
	})
	if err != nil {
		t.Fatalf("PrepareTaskRootFS returned error: %v", err)
	}
	wantPath := filepath.Join(stateDir, "tasks", "alice", "rootfs.ext4")
	if rootfs.Path != wantPath {
		t.Fatalf("task path = %q, want %q", rootfs.Path, wantPath)
	}
	data, err := os.ReadFile(rootfs.Path)
	if err != nil {
		t.Fatalf("read task rootfs: %v", err)
	}
	if string(data) != "ext4" {
		t.Fatalf("task rootfs contents = %q", string(data))
	}
}

func TestFirecrackerImageStorePrepareTaskRootFSInjectsEnv(t *testing.T) {
	stateDir := t.TempDir()
	commands := &fakeFirecrackerImageCommands{
		outputs: map[string][]byte{
			"podman image inspect --format {{.Digest}} agency-body:latest":                                      []byte("sha256:abc123\n"),
			"podman image inspect --format {{json .Config.Entrypoint}}|{{json .Config.Cmd}} agency-body:latest": []byte("null|[\"/app/entrypoint.sh\"]\n"),
			"podman create agency-body:latest":                                                                  []byte("source-id\n"),
		},
	}
	store := &FirecrackerImageStore{StateDir: stateDir, commands: commands}
	rootfs, err := store.PrepareTaskRootFS(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package: runtimecontract.RuntimePackageSpec{
			Image: "agency-body:latest",
			Env: map[string]string{
				"AGENCY_AGENT_NAME": "alice",
				"invalid-key":       "ignored",
			},
		},
	})
	if err != nil {
		t.Fatalf("PrepareTaskRootFS returned error: %v", err)
	}
	if rootfs.BasePath != "" {
		t.Fatalf("base path = %q, want empty for env-injected task rootfs", rootfs.BasePath)
	}
	if rootfs.Path != filepath.Join(stateDir, "tasks", "alice", "rootfs.ext4") {
		t.Fatalf("task path = %q", rootfs.Path)
	}
	if !commands.exported["source-id"] {
		t.Fatal("expected image filesystem export for env-injected task rootfs")
	}
}

func TestApplyFirecrackerRootFSOverlays(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "nested", "config.yaml"), []byte("config"), 0o640); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(dir, "routing.yaml")
	if err := os.WriteFile(srcFile, []byte("routing"), 0o644); err != nil {
		t.Fatal(err)
	}
	value, err := FirecrackerRootFSOverlaysEnvValue([]FirecrackerRootFSOverlay{
		{HostPath: srcDir, GuestPath: "/agency/enforcer/config", Mode: "ro"},
		{HostPath: srcFile, GuestPath: "/agency/enforcer/routing.yaml", Mode: "ro"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stageDir := filepath.Join(dir, "stage")
	if err := applyFirecrackerRootFSOverlays(stageDir, map[string]string{FirecrackerRootFSOverlaysEnv: value}, dir); err != nil {
		t.Fatalf("applyFirecrackerRootFSOverlays returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(stageDir, "agency", "enforcer", "config", "nested", "config.yaml"))
	if err != nil {
		t.Fatalf("read overlaid dir file: %v", err)
	}
	if string(data) != "config" {
		t.Fatalf("overlaid dir file = %q", string(data))
	}
	data, err = os.ReadFile(filepath.Join(stageDir, "agency", "enforcer", "routing.yaml"))
	if err != nil {
		t.Fatalf("read overlaid file: %v", err)
	}
	if string(data) != "routing" {
		t.Fatalf("overlaid file = %q", string(data))
	}
}

func TestApplyFirecrackerRootFSOverlaysRejectsRelativeGuestPath(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	value, err := FirecrackerRootFSOverlaysEnvValue([]FirecrackerRootFSOverlay{
		{HostPath: src, GuestPath: "relative"},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = applyFirecrackerRootFSOverlays(t.TempDir(), map[string]string{FirecrackerRootFSOverlaysEnv: value}, filepath.Dir(src))
	if err == nil || !strings.Contains(err.Error(), "guest path must be absolute") {
		t.Fatalf("applyFirecrackerRootFSOverlays error = %v", err)
	}
}

func TestApplyFirecrackerRootFSOverlaysRejectsHostPathOutsideBase(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	value, err := FirecrackerRootFSOverlaysEnvValue([]FirecrackerRootFSOverlay{
		{HostPath: src, GuestPath: "/agency/src"},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = applyFirecrackerRootFSOverlays(t.TempDir(), map[string]string{FirecrackerRootFSOverlaysEnv: value}, base)
	if err == nil || !strings.Contains(err.Error(), "host path must be under") {
		t.Fatalf("applyFirecrackerRootFSOverlays error = %v", err)
	}
}

func TestWriteFirecrackerInitExecsOCICommand(t *testing.T) {
	stageDir := t.TempDir()
	if err := writeFirecrackerInit(stageDir, []string{"/bin/sh", "-c", "echo 'hello'"}, map[string]string{
		"AGENCY_AGENT_NAME": "alice",
		"PATH":              "/usr/local/bin:/usr/bin",
		"invalid-key":       "ignored",
	}); err != nil {
		t.Fatalf("writeFirecrackerInit returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(stageDir, strings.TrimPrefix(firecrackerInitPath, "/")))
	if err != nil {
		t.Fatalf("read init: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`export AGENCY_AGENT_NAME='alice'`,
		`export PATH='/usr/local/bin:/usr/bin'`,
		"/usr/local/bin/agency-vsock-http-bridge &",
		`set -- '/bin/sh' '-c' 'echo '"'"'hello'"'"''`,
		`exec "$@"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("init missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "invalid-key") {
		t.Fatalf("init exported invalid env key:\n%s", text)
	}
}

func TestParseOCICommandPart(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want []string
	}{
		{`null`, nil},
		{`["/bin/app","--serve"]`, []string{"/bin/app", "--serve"}},
		{`"/bin/app --serve"`, []string{"/bin/sh", "-c", "/bin/app --serve"}},
	} {
		got, err := parseOCICommandPart(tt.raw)
		if err != nil {
			t.Fatalf("parseOCICommandPart(%s) returned error: %v", tt.raw, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("parseOCICommandPart(%s) = %#v, want %#v", tt.raw, got, tt.want)
		}
	}
}

func TestInstallFirecrackerVsockBridge(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "bridge")
	if err := os.WriteFile(binary, []byte("bridge"), 0o755); err != nil {
		t.Fatal(err)
	}
	stageDir := filepath.Join(dir, "stage")
	if err := installFirecrackerVsockBridge(stageDir, binary); err != nil {
		t.Fatalf("installFirecrackerVsockBridge returned error: %v", err)
	}
	target := filepath.Join(stageDir, "usr", "local", "bin", "agency-vsock-http-bridge")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed bridge: %v", err)
	}
	if string(data) != "bridge" {
		t.Fatalf("installed bridge = %q", string(data))
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("bridge mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestOSFirecrackerImageCommandsExport(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	stage := filepath.Join(dir, "stage")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	podman := filepath.Join(dir, "podman")
	script := "#!/bin/sh\nif [ \"$1\" != \"export\" ]; then exit 2; fi\nexec tar -C " + shellQuote(src) + " -cf - .\n"
	if err := os.WriteFile(podman, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (osFirecrackerImageCommands{}).Export(context.Background(), podman, "source-id", stage); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(stage, "hello.txt"))
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("exported file = %q", string(data))
	}
}

func TestSanitizeFirecrackerDigest(t *testing.T) {
	if got := sanitizeFirecrackerDigest("registry.example/agent@sha256:abc"); got != "registry.example-agent-sha256-abc" {
		t.Fatalf("sanitizeFirecrackerDigest() = %q", got)
	}
}

type fakeFirecrackerImageCommands struct {
	outputs    map[string][]byte
	outputErrs map[string]error
	runs       [][]string
	exported   map[string]bool
}

func (f *fakeFirecrackerImageCommands) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	_ = ctx
	key := commandKey(name, args...)
	if err := f.outputErrs[key]; err != nil {
		return nil, err
	}
	out, ok := f.outputs[key]
	if !ok {
		return nil, fmt.Errorf("unexpected output command %q", key)
	}
	return out, nil
}

func (f *fakeFirecrackerImageCommands) Run(ctx context.Context, name string, args ...string) error {
	_ = ctx
	f.runs = append(f.runs, append([]string{name}, args...))
	if name == "mke2fs" {
		if len(args) == 0 {
			return fmt.Errorf("mke2fs missing output")
		}
		return os.WriteFile(args[len(args)-1], []byte("ext4"), 0o644)
	}
	return nil
}

func (f *fakeFirecrackerImageCommands) Export(ctx context.Context, podmanPath, id, stageDir string) error {
	_ = ctx
	_ = podmanPath
	if f.exported == nil {
		f.exported = map[string]bool{}
	}
	f.exported[id] = true
	if err := os.MkdirAll(filepath.Join(stageDir, "bin"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stageDir, "bin", "sh"), []byte("sh"), 0o755)
}

func (f *fakeFirecrackerImageCommands) sawRun(want ...string) bool {
	for _, got := range f.runs {
		if len(got) < len(want) {
			continue
		}
		if reflect.DeepEqual(got[:len(want)], want) {
			return true
		}
	}
	return false
}

func (f *fakeFirecrackerImageCommands) reset() {
	f.runs = nil
	f.exported = map[string]bool{}
}

func commandKey(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), " ")
}
