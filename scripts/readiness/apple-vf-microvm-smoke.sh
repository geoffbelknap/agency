#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HELPER_BIN="${AGENCY_APPLE_VF_HELPER_BIN:-$ROOT/tools/apple-vf-helper/.build/release/agency-apple-vf-helper}"
KERNEL_PATH="${AGENCY_APPLE_VF_KERNEL:-$HOME/.agency/runtime/apple-vf-microvm/artifacts/Image}"
MKE2FS_PATH="${AGENCY_MKE2FS:-}"
ENFORCER_BIN="${AGENCY_APPLE_VF_ENFORCER_BIN:-/tmp/agency-enforcer-host}"
ENFORCER_OCI_REF="${AGENCY_APPLE_VF_ENFORCER_OCI_REF:-}"
VSOCK_BRIDGE_BIN="${AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN:-/tmp/agency-vsock-http-bridge-linux-arm64}"
AGENT_NAME="${AGENT_NAME:-apple-vf-smoke-$(date +%s)}"
ROOTFS_SIZE_MIB="${AGENCY_APPLE_VF_ROOTFS_SIZE_MIB:-1024}"
ROOTFS_OCI_REF="${AGENCY_APPLE_VF_ROOTFS_OCI_REF:-}"
BUILD_HELPER=1
KEEP_HOME=0
SMOKE_HOME=""
GO_SMOKE=""

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/apple-vf-microvm-smoke.sh [options]

Runs a disposable macOS Apple VF microVM smoke:
  1. verifies/builds the signed Apple VF helper
  2. verifies the Linux kernel artifact
  3. builds the host enforcer and Linux ARM64 guest vsock bridge
  4. builds an ORAS/ext4 workload rootfs without a local container runtime
  5. starts the host-process enforcer and Apple VF workload VM
  6. waits for the body runtime to connect to real comms
  7. stops the VM/enforcer and removes disposable state

Options:
  --home PATH             Use a specific disposable Agency home.
  --agent NAME            Agent name for the disposable smoke runtime.
  --helper-bin PATH       Signed agency-apple-vf-helper path.
  --kernel PATH           Linux ARM64 kernel Image path.
  --mke2fs PATH           mke2fs path. Defaults to PATH lookup, then Homebrew e2fsprogs.
  --enforcer-bin PATH     Host-process enforcer output path.
  --enforcer-oci-ref REF  Versioned enforcer OCI artifact reference. When set,
                          extracts darwin/arm64 /usr/local/bin/enforcer and
                          uses it as the host-process enforcer binary.
  --vsock-bridge-bin PATH Linux ARM64 agency-vsock-http-bridge output path.
  --rootfs-oci-ref REF    Versioned OCI artifact reference for the body rootfs source.
  --rootfs-size-mib N     Rootfs image size. Defaults to 1024.
  --skip-helper-build     Reuse --helper-bin instead of building/signing it.
  --keep-home             Keep the disposable Agency home after the run.

Environment:
  AGENCY_APPLE_VF_HELPER_BIN
  AGENCY_APPLE_VF_KERNEL
  AGENCY_MKE2FS
  AGENCY_APPLE_VF_ENFORCER_BIN
  AGENCY_APPLE_VF_ENFORCER_OCI_REF
  AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN
  AGENCY_APPLE_VF_ROOTFS_OCI_REF
  AGENCY_APPLE_VF_ROOTFS_SIZE_MIB
EOF
}

log() {
  printf '==> %s\n' "$1"
}

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

cleanup() {
  if [[ -n "$GO_SMOKE" ]]; then
    rm -f "$GO_SMOKE"
  fi
  if [[ "$KEEP_HOME" != "1" && -n "$SMOKE_HOME" && "$SMOKE_HOME" == /tmp/agency-apple-vf-smoke.* ]]; then
    rm -rf "$SMOKE_HOME"
  fi
}
trap cleanup EXIT INT TERM HUP

while [[ $# -gt 0 ]]; do
  case "$1" in
    --home)
      [[ $# -ge 2 ]] || fail "--home requires a path"
      SMOKE_HOME="$2"
      shift 2
      ;;
    --agent)
      [[ $# -ge 2 ]] || fail "--agent requires a name"
      AGENT_NAME="$2"
      shift 2
      ;;
    --helper-bin)
      [[ $# -ge 2 ]] || fail "--helper-bin requires a path"
      HELPER_BIN="$2"
      shift 2
      ;;
    --kernel)
      [[ $# -ge 2 ]] || fail "--kernel requires a path"
      KERNEL_PATH="$2"
      shift 2
      ;;
    --mke2fs)
      [[ $# -ge 2 ]] || fail "--mke2fs requires a path"
      MKE2FS_PATH="$2"
      shift 2
      ;;
    --enforcer-bin)
      [[ $# -ge 2 ]] || fail "--enforcer-bin requires a path"
      ENFORCER_BIN="$2"
      shift 2
      ;;
    --enforcer-oci-ref)
      [[ $# -ge 2 ]] || fail "--enforcer-oci-ref requires a ref"
      ENFORCER_OCI_REF="$2"
      shift 2
      ;;
    --vsock-bridge-bin)
      [[ $# -ge 2 ]] || fail "--vsock-bridge-bin requires a path"
      VSOCK_BRIDGE_BIN="$2"
      shift 2
      ;;
    --rootfs-size-mib)
      [[ $# -ge 2 ]] || fail "--rootfs-size-mib requires a value"
      ROOTFS_SIZE_MIB="$2"
      shift 2
      ;;
    --rootfs-oci-ref)
      [[ $# -ge 2 ]] || fail "--rootfs-oci-ref requires a value"
      ROOTFS_OCI_REF="$2"
      shift 2
      ;;
    --skip-helper-build)
      BUILD_HELPER=0
      shift
      ;;
    --keep-home)
      KEEP_HOME=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

if [[ "$(uname -s)" != "Darwin" ]]; then
  fail "apple-vf-microvm smoke requires macOS"
fi
if [[ "$(uname -m)" != "arm64" ]]; then
  fail "apple-vf-microvm smoke requires Apple silicon arm64"
fi

if [[ -z "$MKE2FS_PATH" ]]; then
  if command -v mke2fs >/dev/null 2>&1; then
    MKE2FS_PATH="$(command -v mke2fs)"
  elif [[ -x /opt/homebrew/opt/e2fsprogs/sbin/mke2fs ]]; then
    MKE2FS_PATH="/opt/homebrew/opt/e2fsprogs/sbin/mke2fs"
  else
    fail "mke2fs not found; install e2fsprogs or pass --mke2fs"
  fi
fi

[[ -r "$KERNEL_PATH" ]] || fail "Apple VF kernel Image is not readable at $KERNEL_PATH"
[[ -x "$MKE2FS_PATH" ]] || fail "mke2fs is not executable at $MKE2FS_PATH"
[[ -n "$ROOTFS_OCI_REF" ]] || fail "Apple VF rootfs OCI artifact is not configured; pass --rootfs-oci-ref or set AGENCY_APPLE_VF_ROOTFS_OCI_REF"

cd "$ROOT"

if [[ "$BUILD_HELPER" == "1" ]]; then
  log "Building signed Apple VF helper"
  "$ROOT/scripts/readiness/apple-vf-helper-build.sh" >/dev/null
fi
[[ -x "$HELPER_BIN" ]] || fail "Apple VF helper is not executable at $HELPER_BIN"

if [[ -n "$ENFORCER_OCI_REF" ]]; then
  log "Extracting host-process enforcer from OCI artifact"
  go run ./cmd/runtime-oci-artifact \
    --extract-ref "$ENFORCER_OCI_REF" \
    --extract-path /usr/local/bin/enforcer \
    --output "$ENFORCER_BIN" \
    --platform darwin/arm64
else
  log "Building host-process enforcer"
  (cd "$ROOT/images/enforcer" && go build -o "$ENFORCER_BIN" .)
fi
[[ -x "$ENFORCER_BIN" ]] || fail "host enforcer build did not produce $ENFORCER_BIN"

log "Building Linux ARM64 guest vsock bridge"
env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o "$VSOCK_BRIDGE_BIN" ./cmd/agency-vsock-http-bridge
[[ -x "$VSOCK_BRIDGE_BIN" ]] || fail "vsock bridge build did not produce $VSOCK_BRIDGE_BIN"

if [[ -z "$SMOKE_HOME" ]]; then
  SMOKE_HOME="$(mktemp -d /tmp/agency-apple-vf-smoke.XXXXXX)"
else
  mkdir -p "$SMOKE_HOME"
fi
log "Using disposable Agency home: $SMOKE_HOME"

GO_SMOKE="$(mktemp "$ROOT/apple-vf-microvm-smoke-XXXXXX.go")"
cat >"$GO_SMOKE" <<'GOEOF'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	cfg := smokeConfig{
		repo:          mustEnv("AGENCY_APPLE_VF_SMOKE_REPO"),
		home:          mustEnv("AGENCY_APPLE_VF_SMOKE_HOME"),
		agent:         mustEnv("AGENCY_APPLE_VF_SMOKE_AGENT"),
		helper:        mustEnv("AGENCY_APPLE_VF_SMOKE_HELPER"),
		kernel:        mustEnv("AGENCY_APPLE_VF_SMOKE_KERNEL"),
		mke2fs:        mustEnv("AGENCY_APPLE_VF_SMOKE_MKE2FS"),
		enforcer:      mustEnv("AGENCY_APPLE_VF_SMOKE_ENFORCER"),
		bridge:        mustEnv("AGENCY_APPLE_VF_SMOKE_BRIDGE"),
		rootfsOCIRef:  mustEnv("AGENCY_APPLE_VF_SMOKE_ROOTFS_OCI_REF"),
		rootfsSizeMiB: getenvDefault("AGENCY_APPLE_VF_SMOKE_ROOTFS_SIZE_MIB", "1024"),
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := writeSmokeAgent(cfg.home, cfg.agent); err != nil {
		return err
	}
	overlayPath := filepath.Join(cfg.home, "overlays", "entrypoint.sh")
	if err := copyFile(filepath.Join(cfg.repo, "images", "body", "entrypoint.sh"), overlayPath, 0o755); err != nil {
		return err
	}

	ports, err := allocatePorts(5)
	if err != nil {
		return err
	}
	comms, err := startComms(ctx, cfg, ports.comms)
	if err != nil {
		return err
	}
	defer stopProcess(comms)
	servers, err := startDummyServices(ports)
	if err != nil {
		return err
	}
	defer func() {
		for _, srv := range servers {
			_ = srv.Shutdown(context.Background())
		}
	}()

	setSmokePorts(ports)
	rs := orchestrate.NewRuntimeSupervisor(cfg.home, "0.2.0-smoke", cfg.repo, "apple-vf-microvm-smoke", runtimebackend.BackendAppleVFMicroVM, nil, nil, nil, nil)
	rs.BackendConfig = map[string]string{
		"helper_binary":            cfg.helper,
		"kernel_path":              cfg.kernel,
		"enforcer_binary_path":     cfg.enforcer,
		"vsock_bridge_binary_path": cfg.bridge,
		"mke2fs_path":              cfg.mke2fs,
		"rootfs_oci_ref":           cfg.rootfsOCIRef,
		"rootfs_size_mib":          cfg.rootfsSizeMiB,
	}
	spec, err := rs.Compile(ctx, cfg.agent)
	if err != nil {
		return err
	}
	overlayEnv, err := runtimebackend.FirecrackerRootFSOverlaysEnvValue([]runtimebackend.FirecrackerRootFSOverlay{
		{HostPath: overlayPath, GuestPath: "/app/entrypoint.sh"},
	})
	if err != nil {
		return err
	}
	spec.Package.Env[runtimebackend.FirecrackerRootFSOverlaysEnv] = overlayEnv
	if err := rs.Reconcile(ctx, spec); err != nil {
		return err
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = rs.Stop(stopCtx, cfg.agent)
	}()
	if err := rs.EnsureWorkspace(ctx, cfg.agent); err != nil {
		return fmt.Errorf("ensure Apple VF workspace: %w", err)
	}
	status, err := waitHealthy(ctx, rs, cfg.agent)
	if err != nil {
		return err
	}
	if err := rs.Validate(ctx, cfg.agent); err != nil {
		return fmt.Errorf("validate Apple VF runtime: %w", err)
	}
	data, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(data))
	return nil
}

type smokeConfig struct {
	repo          string
	home          string
	agent         string
	helper        string
	kernel        string
	mke2fs        string
	enforcer      string
	bridge        string
	rootfsOCIRef  string
	rootfsSizeMiB string
}

type smokePorts struct {
	gateway  string
	comms    string
	knowledge string
	webFetch string
	egress   string
}

func (c smokeConfig) validate() error {
	for name, path := range map[string]string{
		"helper":   c.helper,
		"kernel":   c.kernel,
		"mke2fs":   c.mke2fs,
		"enforcer": c.enforcer,
		"bridge":   c.bridge,
	} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s path %s: %w", name, path, err)
		}
	}
	return nil
}

func allocatePorts(n int) (smokePorts, error) {
	ports := make([]string, 0, n)
	for len(ports) < n {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return smokePorts{}, err
		}
		port := fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)
		_ = ln.Close()
		ports = append(ports, port)
	}
	return smokePorts{gateway: ports[0], comms: ports[1], knowledge: ports[2], webFetch: ports[3], egress: ports[4]}, nil
}

func writeSmokeAgent(home, agent string) error {
	agentDir := filepath.Join(home, "agents", agent)
	if err := os.MkdirAll(filepath.Join(agentDir, "state"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_apple_vf_smoke\nmodel: smoke-model\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(agentDir, "identity.md"), []byte("# Apple VF Smoke\n"), 0o644)
}

func startComms(ctx context.Context, cfg smokeConfig, port string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, filepath.Join(cfg.repo, ".venv", "bin", "python"), "services/comms/server.py", "--port", port, "--data-dir", filepath.Join(cfg.home, "comms-data"), "--agents-dir", filepath.Join(cfg.home, "agents"))
	cmd.Dir = cfg.repo
	cmd.Env = append(os.Environ(), "PYTHONPATH=.")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if err := waitHTTP(ctx, "http://127.0.0.1:"+port+"/health"); err != nil {
		stopProcess(cmd)
		return nil, err
	}
	return cmd, nil
}

func startDummyServices(ports smokePorts) ([]*http.Server, error) {
	servicePorts := []string{ports.gateway, ports.knowledge, ports.webFetch, ports.egress}
	servers := make([]*http.Server, 0, len(servicePorts))
	for _, port := range servicePorts {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		})
		ln, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			return nil, err
		}
		srv := &http.Server{Handler: mux}
		servers = append(servers, srv)
		go func() { _ = srv.Serve(ln) }()
	}
	return servers, nil
}

func waitHealthy(ctx context.Context, rs *orchestrate.RuntimeSupervisor, agent string) (any, error) {
	deadline := time.Now().Add(3 * time.Minute)
	var last any
	for time.Now().Before(deadline) {
		status, err := rs.Get(ctx, agent)
		if err == nil {
			last = status
			if status.Healthy && status.Details["body_ws_connected"] == "true" {
				return status, nil
			}
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return last, fmt.Errorf("Apple VF runtime did not become healthy; last=%v", last)
}

func waitHTTP(ctx context.Context, url string) error {
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("wait for %s: %w", url, lastErr)
}

func setSmokePorts(ports smokePorts) {
	_ = os.Setenv("AGENCY_GATEWAY_PORT", ports.gateway)
	_ = os.Setenv("AGENCY_GATEWAY_PROXY_PORT", ports.comms)
	_ = os.Setenv("AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT", ports.knowledge)
	_ = os.Setenv("AGENCY_WEB_FETCH_PORT", ports.webFetch)
	_ = os.Setenv("AGENCY_EGRESS_PROXY_PORT", ports.egress)
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func mustEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		panic(key + " is required")
	}
	return value
}

func getenvDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
GOEOF

log "Running Apple VF microVM smoke"
env \
  AGENCY_APPLE_VF_SMOKE_REPO="$ROOT" \
  AGENCY_APPLE_VF_SMOKE_HOME="$SMOKE_HOME" \
  AGENCY_APPLE_VF_SMOKE_AGENT="$AGENT_NAME" \
  AGENCY_APPLE_VF_SMOKE_HELPER="$HELPER_BIN" \
  AGENCY_APPLE_VF_SMOKE_KERNEL="$KERNEL_PATH" \
  AGENCY_APPLE_VF_SMOKE_MKE2FS="$MKE2FS_PATH" \
  AGENCY_APPLE_VF_SMOKE_ENFORCER="$ENFORCER_BIN" \
  AGENCY_APPLE_VF_SMOKE_BRIDGE="$VSOCK_BRIDGE_BIN" \
  AGENCY_APPLE_VF_SMOKE_ROOTFS_OCI_REF="$ROOTFS_OCI_REF" \
  AGENCY_APPLE_VF_SMOKE_ROOTFS_SIZE_MIB="$ROOTFS_SIZE_MIB" \
  go run "$GO_SMOKE"

log "Apple VF microVM smoke passed"
