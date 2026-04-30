#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FIRECRACKER_VERSION="${AGENCY_FIRECRACKER_VERSION:-v1.12.1}"
FIRECRACKER_ARCH="$(uname -m)"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
ARTIFACT_DIR="${AGENCY_FIRECRACKER_ARTIFACT_DIR:-$AGENCY_HOME_DIR/runtime/firecracker/artifacts}"
FIRECRACKER_BIN="${AGENCY_FIRECRACKER_BIN:-$ARTIFACT_DIR/$FIRECRACKER_VERSION/firecracker-$FIRECRACKER_VERSION-$FIRECRACKER_ARCH}"
KERNEL_PATH="${AGENCY_FIRECRACKER_KERNEL:-$ARTIFACT_DIR/vmlinux}"
MKE2FS_PATH="${AGENCY_MKE2FS:-}"
ENFORCER_BIN="${AGENCY_FIRECRACKER_ENFORCER_BIN:-/tmp/agency-firecracker-enforcer-host}"
VSOCK_BRIDGE_BIN="${AGENCY_FIRECRACKER_VSOCK_BRIDGE_BIN:-/tmp/agency-firecracker-vsock-http-bridge}"
AGENT_NAME="${AGENT_NAME:-firecracker-smoke-$(date +%s)}"
ROOTFS_SIZE_MIB="${AGENCY_FIRECRACKER_ROOTFS_SIZE_MIB:-1024}"
OCI_CMD="${CONTAINER_CMD:-}"
BUILD_BODY=1
KEEP_HOME=0
KEEP_AGENT=0
SMOKE_HOME=""
GO_SMOKE=""

usage() {
  cat <<EOF
Usage: ./scripts/readiness/firecracker-microvm-smoke.sh [options]

Runs a disposable Linux/WSL Firecracker microVM smoke:
  1. verifies KVM/vhost-vsock host access
  2. verifies a pinned upstream Firecracker release binary
  3. verifies an Agency-built Firecracker-compatible ELF vmlinux kernel
  4. builds host enforcer and guest vsock bridge helper binaries
  5. builds the agency-body OCI artifact and realizes it as an ext4 rootfs
  6. starts, validates, restarts, stops, and deletes a disposable runtime
     without requiring real LLM provider credentials. With --keep-agent, the
     runtime stays up for an external contract smoke.

Options:
  --home PATH             Use a specific disposable Agency home.
  --agent NAME            Agent name for the disposable smoke runtime.
  --firecracker-bin PATH  Pinned upstream Firecracker binary path.
                           Default: $FIRECRACKER_BIN
  --firecracker-version V Expected Firecracker version. Default: $FIRECRACKER_VERSION
  --kernel PATH           Agency Linux build artifact vmlinux path.
                           Default: $KERNEL_PATH
  --mke2fs PATH           mke2fs path. Defaults to PATH lookup.
  --enforcer-bin PATH     Host-process enforcer output path.
  --vsock-bridge-bin PATH Linux agency-vsock-http-bridge output path.
  --container-cmd PATH    Podman- or Docker-compatible OCI image command.
  --rootfs-size-mib N     Rootfs image size. Defaults to 1024.
  --skip-body-build       Reuse existing agency-body:latest OCI artifact.
  --keep-home             Keep the disposable Agency home after the run.
  --keep-agent            Keep the disposable Agency home and leave the
                          Firecracker runtime running so contract smoke can
                          attach from another shell.

Environment:
  AGENCY_FIRECRACKER_VERSION      default: $FIRECRACKER_VERSION
  AGENCY_FIRECRACKER_ARTIFACT_DIR default: $ARTIFACT_DIR
  AGENCY_FIRECRACKER_BIN
  AGENCY_FIRECRACKER_KERNEL
  AGENCY_MKE2FS
  AGENCY_FIRECRACKER_ENFORCER_BIN
  AGENCY_FIRECRACKER_VSOCK_BRIDGE_BIN
  AGENCY_FIRECRACKER_ROOTFS_SIZE_MIB
  CONTAINER_CMD

Firecracker artifacts are intentionally explicit:
  - firecracker binary: upstream release artifact pinned by version
  - kernel: Agency Linux build artifact vmlinux, not a host distro kernel
  - rootfs: agency-body OCI artifact realized through the shared OCI-to-ext4 path
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
  if [[ "$KEEP_HOME" != "1" && -n "$SMOKE_HOME" && "$SMOKE_HOME" == /tmp/agency-firecracker-smoke.* ]]; then
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
    --firecracker-bin)
      [[ $# -ge 2 ]] || fail "--firecracker-bin requires a path"
      FIRECRACKER_BIN="$2"
      shift 2
      ;;
    --firecracker-version)
      [[ $# -ge 2 ]] || fail "--firecracker-version requires a value"
      FIRECRACKER_VERSION="$2"
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
    --vsock-bridge-bin)
      [[ $# -ge 2 ]] || fail "--vsock-bridge-bin requires a path"
      VSOCK_BRIDGE_BIN="$2"
      shift 2
      ;;
    --container-cmd)
      [[ $# -ge 2 ]] || fail "--container-cmd requires a path"
      OCI_CMD="$2"
      shift 2
      ;;
    --rootfs-size-mib)
      [[ $# -ge 2 ]] || fail "--rootfs-size-mib requires a value"
      ROOTFS_SIZE_MIB="$2"
      shift 2
      ;;
    --skip-body-build)
      BUILD_BODY=0
      shift
      ;;
    --keep-home)
      KEEP_HOME=1
      shift
      ;;
    --keep-agent)
      KEEP_AGENT=1
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

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

require_device_rw() {
  local path="$1"
  [[ -e "$path" ]] || fail "$path is missing"
  [[ -r "$path" && -w "$path" ]] || fail "$path is not readable and writable by $(id -un)"
}

verify_firecracker_binary() {
  [[ -x "$FIRECRACKER_BIN" ]] || fail "Firecracker binary is not executable at $FIRECRACKER_BIN"
  local version_out
  version_out="$("$FIRECRACKER_BIN" --version 2>&1 || true)"
  if [[ "$version_out" != *"$FIRECRACKER_VERSION"* ]]; then
    fail "Firecracker binary version mismatch: got '$version_out', want $FIRECRACKER_VERSION from pinned upstream release artifact"
  fi
}

verify_kernel() {
  [[ -r "$KERNEL_PATH" ]] || fail "Firecracker vmlinux is not readable at $KERNEL_PATH"
  python3 - "$KERNEL_PATH" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
magic = path.read_bytes()[:4]
if magic != b"\x7fELF":
    raise SystemExit(f"{path} is not an uncompressed ELF vmlinux image")
PY
}

if [[ "$(uname -s)" != "Linux" ]]; then
  fail "firecracker microVM smoke requires Linux or WSL2"
fi
case "$FIRECRACKER_ARCH" in
  x86_64|aarch64) ;;
  *) fail "unsupported Firecracker host architecture: $FIRECRACKER_ARCH" ;;
esac

require_cmd go
require_cmd make
require_cmd python3
require_cmd curl
require_device_rw /dev/kvm
require_device_rw /dev/vhost-vsock

if [[ -z "$MKE2FS_PATH" ]]; then
  if command -v mke2fs >/dev/null 2>&1; then
    MKE2FS_PATH="$(command -v mke2fs)"
  else
    fail "mke2fs not found; install e2fsprogs or pass --mke2fs"
  fi
fi
[[ -x "$MKE2FS_PATH" ]] || fail "mke2fs is not executable at $MKE2FS_PATH"

if [[ -z "$OCI_CMD" ]]; then
  if command -v podman >/dev/null 2>&1; then
    OCI_CMD="$(command -v podman)"
  elif command -v docker >/dev/null 2>&1; then
    OCI_CMD="$(command -v docker)"
  else
    fail "podman or docker is required to build/export the OCI body artifact"
  fi
fi
[[ -x "$OCI_CMD" ]] || fail "container command is not executable at $OCI_CMD"

verify_firecracker_binary
verify_kernel

cd "$ROOT"

if [[ "$BUILD_BODY" == "1" ]]; then
  log "Building agency-body OCI artifact with $OCI_CMD"
  make body CONTAINER_CMD="$OCI_CMD"
fi

log "Building host-process enforcer"
(cd "$ROOT/images/enforcer" && go build -o "$ENFORCER_BIN" .)
[[ -x "$ENFORCER_BIN" ]] || fail "host enforcer build did not produce $ENFORCER_BIN"

log "Building Linux guest vsock bridge"
go build -o "$VSOCK_BRIDGE_BIN" ./cmd/agency-vsock-http-bridge
[[ -x "$VSOCK_BRIDGE_BIN" ]] || fail "vsock bridge build did not produce $VSOCK_BRIDGE_BIN"

if [[ -z "$SMOKE_HOME" ]]; then
  SMOKE_HOME="$(mktemp -d /tmp/agency-firecracker-smoke.XXXXXX)"
else
  mkdir -p "$SMOKE_HOME"
fi
log "Using disposable Agency home: $SMOKE_HOME"

mkdir -p "$ROOT/build"
GO_SMOKE="$(mktemp "$ROOT/build/firecracker-microvm-smoke-XXXXXX.go")"
cat >"$GO_SMOKE" <<'GOEOF'
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

type smokeConfig struct {
	repo          string
	home          string
	agent         string
	firecracker   string
	kernel        string
	mke2fs        string
	enforcer      string
	bridge        string
	oci           string
	rootfsSizeMiB string
	keepAgent     bool
}

type smokePorts struct {
	gateway  string
	comms    string
	knowledge string
	webFetch string
	egress   string
}

func main() {
	cfg := smokeConfig{}
	flag.StringVar(&cfg.repo, "repo", "", "repository root")
	flag.StringVar(&cfg.home, "home", "", "Agency home")
	flag.StringVar(&cfg.agent, "agent", "", "agent name")
	flag.StringVar(&cfg.firecracker, "firecracker-bin", "", "Firecracker binary")
	flag.StringVar(&cfg.kernel, "kernel", "", "Firecracker vmlinux")
	flag.StringVar(&cfg.mke2fs, "mke2fs", "", "mke2fs binary")
	flag.StringVar(&cfg.enforcer, "enforcer-bin", "", "host enforcer binary")
	flag.StringVar(&cfg.bridge, "vsock-bridge-bin", "", "guest vsock bridge binary")
	flag.StringVar(&cfg.oci, "container-cmd", "", "podman/docker-compatible OCI command")
	flag.StringVar(&cfg.rootfsSizeMiB, "rootfs-size-mib", "1024", "rootfs size")
	flag.BoolVar(&cfg.keepAgent, "keep-agent", false, "leave runtime running for external contract smoke")
	flag.Parse()
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cfg smokeConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
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
	rs := orchestrate.NewRuntimeSupervisor(cfg.home, "0.2.0-smoke", cfg.repo, "firecracker-microvm-smoke", runtimebackend.BackendFirecracker, nil, nil, nil, nil)
	rs.BackendConfig = map[string]string{
		"binary_path":              cfg.firecracker,
		"kernel_path":              cfg.kernel,
		"enforcer_binary_path":     cfg.enforcer,
		"vsock_bridge_binary_path": cfg.bridge,
		"mke2fs_path":              cfg.mke2fs,
		"podman_path":              cfg.oci,
		"rootfs_size_mib":          cfg.rootfsSizeMiB,
	}
	spec, err := rs.Compile(ctx, cfg.agent)
	if err != nil {
		return err
	}
	spec.Lifecycle.RestartPolicy = "never"
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
	if !cfg.keepAgent {
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = rs.Stop(stopCtx, cfg.agent)
		}()
	}

	fmt.Println("==> Starting Firecracker workspace")
	if err := rs.EnsureWorkspace(ctx, cfg.agent); err != nil {
		return fmt.Errorf("ensure Firecracker workspace: %w", err)
	}
	status, err := waitHealthy(ctx, rs, cfg.agent)
	if err != nil {
		return err
	}
	if err := rs.Validate(ctx, cfg.agent); err != nil {
		return fmt.Errorf("validate Firecracker runtime: %w", err)
	}
	printStatus(status)
	if err := verifyRootFS(cfg.home, cfg.agent); err != nil {
		return err
	}

	fmt.Println("==> Restarting Firecracker workspace")
	if err := rs.Restart(ctx, cfg.agent); err != nil {
		return fmt.Errorf("restart Firecracker runtime: %w", err)
	}
	status, err = waitHealthy(ctx, rs, cfg.agent)
	if err != nil {
		return err
	}
	if err := rs.Validate(ctx, cfg.agent); err != nil {
		return fmt.Errorf("validate restarted Firecracker runtime: %w", err)
	}
	printStatus(status)
	if cfg.keepAgent {
		printKeepAgentInstructions(cfg)
		waitForContractSmoke(ctx)
	}

	fmt.Println("==> Stopping and deleting Firecracker workspace")
	if err := rs.Stop(ctx, cfg.agent); err != nil {
		return fmt.Errorf("stop Firecracker runtime: %w", err)
	}
	if err := os.RemoveAll(filepath.Join(cfg.home, "agents", cfg.agent)); err != nil {
		return err
	}
	return assertRuntimeStateRemoved(cfg.home, cfg.agent)
}

func (c smokeConfig) validate() error {
	for name, value := range map[string]string{
		"repo": c.repo, "home": c.home, "agent": c.agent, "firecracker": c.firecracker,
		"kernel": c.kernel, "mke2fs": c.mke2fs, "enforcer": c.enforcer,
		"bridge": c.bridge, "container-cmd": c.oci,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, path := range map[string]string{
		"firecracker": c.firecracker, "kernel": c.kernel, "mke2fs": c.mke2fs,
		"enforcer": c.enforcer, "bridge": c.bridge, "container-cmd": c.oci,
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
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_firecracker_smoke\nmodel: smoke-model\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(agentDir, "identity.md"), []byte("# Firecracker Smoke\n"), 0o644)
}

func startComms(ctx context.Context, cfg smokeConfig, port string) (*exec.Cmd, error) {
	python := filepath.Join(cfg.repo, ".venv", "bin", "python")
	if _, err := os.Stat(python); err != nil {
		found, lookErr := exec.LookPath("python3")
		if lookErr != nil {
			return nil, lookErr
		}
		python = found
	}
	cmd := exec.CommandContext(ctx, python, "services/comms/server.py", "--port", port, "--data-dir", filepath.Join(cfg.home, "comms-data"), "--agents-dir", filepath.Join(cfg.home, "agents"))
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
			if status.Backend == runtimebackend.BackendFirecracker &&
				status.Phase == "running" &&
				status.Healthy &&
				status.Details["body_ws_connected"] == "true" &&
				status.Details["enforcer_state"] == "running" &&
				status.Details["vsock_bridge_state"] == "running" {
				return status, nil
			}
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return last, fmt.Errorf("Firecracker runtime did not become healthy; last=%v", last)
}

func verifyRootFS(home, agent string) error {
	path := filepath.Join(home, "firecracker", "tasks", agent, "rootfs.ext4")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("rootfs was not realized at %s: %w", path, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("rootfs at %s is empty", path)
	}
	fmt.Printf("rootfs_path=%s\n", path)
	return nil
}

func assertRuntimeStateRemoved(home, agent string) error {
	paths := []string{
		filepath.Join(home, "agents", agent),
		filepath.Join(home, "firecracker", agent),
		filepath.Join(home, "firecracker", "tasks", agent),
		filepath.Join(home, "firecracker", "pids", agent+".pid"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("stale runtime state remains at %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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

func printStatus(status any) {
	data, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(data))
}

func printKeepAgentInstructions(cfg smokeConfig) {
	fmt.Printf("agent_name=%s\n", cfg.agent)
	fmt.Printf("agency_home=%s\n", cfg.home)
	fmt.Println("contract_smoke_command=bash ./scripts/readiness/runtime-contract-smoke.sh --agent " + cfg.agent + " --home " + cfg.home + " --start-gateway --skip-tests --skip-doctor")
	fmt.Println("==> Keeping Firecracker runtime and dummy services running; press Ctrl-C when external contract smoke is complete")
}

func waitForContractSmoke(ctx context.Context) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case <-signals:
	case <-ctx.Done():
		fmt.Println("==> Keep-agent hold timed out")
	}
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
GOEOF

log "Running Firecracker microVM smoke"
go_args=(
  "$GO_SMOKE"
  --repo "$ROOT" \
  --home "$SMOKE_HOME" \
  --agent "$AGENT_NAME" \
  --firecracker-bin "$FIRECRACKER_BIN" \
  --kernel "$KERNEL_PATH" \
  --mke2fs "$MKE2FS_PATH" \
  --enforcer-bin "$ENFORCER_BIN" \
  --vsock-bridge-bin "$VSOCK_BRIDGE_BIN" \
  --container-cmd "$OCI_CMD" \
  --rootfs-size-mib "$ROOTFS_SIZE_MIB"
)
if [[ "$KEEP_AGENT" == "1" ]]; then
  go_args+=(--keep-agent)
fi
go run "${go_args[@]}"

log "Firecracker microVM smoke passed"
