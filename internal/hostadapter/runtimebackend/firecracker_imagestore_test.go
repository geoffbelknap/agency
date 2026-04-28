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
			"podman image inspect --format {{.Digest}} agency-body:latest": []byte("sha256:abc123\n"),
			"podman create agency-body:latest":                             []byte("source-id\n"),
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
			"podman image inspect --format {{.Id}} ghcr.io/example/agent:latest": []byte("sha256:fallback\n"),
			"podman create ghcr.io/example/agent:latest":                         []byte("source-id\n"),
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

func TestFirecrackerImageStorePrepareTaskRootFSCopiesBase(t *testing.T) {
	stateDir := t.TempDir()
	commands := &fakeFirecrackerImageCommands{
		outputs: map[string][]byte{
			"podman image inspect --format {{.Digest}} agency-body:latest": []byte("sha256:abc123\n"),
			"podman create agency-body:latest":                             []byte("source-id\n"),
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
