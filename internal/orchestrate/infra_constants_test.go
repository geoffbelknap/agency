package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test file to find the repo root (contains go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}

// TestNetworkConstantsInDocs verifies that the actual Docker network names
// defined in Go constants appear in AGENTS.md (the canonical agent-instructions
// file; CLAUDE.md is a one-line @AGENTS.md import). Catches stale doc references
// when network names are renamed in code.
func TestNetworkConstantsInDocs(t *testing.T) {
	root := repoRoot(t)
	agentsMD := filepath.Join(root, "AGENTS.md")

	data, err := os.ReadFile(agentsMD)
	if err != nil {
		t.Fatal("read AGENTS.md:", err)
	}
	content := string(data)

	networks := map[string]string{
		"baseGatewayNet":   baseGatewayNet,
		"baseEgressIntNet": baseEgressIntNet,
		"baseEgressExtNet": baseEgressExtNet,
	}

	for constName, netName := range networks {
		if !strings.Contains(content, netName) {
			t.Errorf("AGENTS.md does not mention network %q (Go const %s = %q) — update the Container topology section",
				netName, constName, netName)
		}
	}
}

// TestGatewayProxyNotSelfLoop parses the gateway-proxy entrypoint script and
// verifies that socat never forwards to TCP:gateway (the container's own alias).
// A TCP self-loop was the root cause of all inter-container communication failures.
// The proxy uses UNIX-CONNECT on Linux and backend-provided host aliases on VM-backed runtimes.
func TestGatewayProxyNotSelfLoop(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "images", "gateway-proxy", "entrypoint.sh")

	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal("read gateway-proxy entrypoint.sh:", err)
	}
	content := string(data)

	// Must never forward to TCP:gateway — that's a self-loop
	if strings.Contains(content, "TCP:gateway:") {
		t.Error("gateway-proxy entrypoint forwards to TCP:gateway — self-loop")
	}
	// Must support Unix socket path (Linux)
	if !strings.Contains(content, "UNIX-CONNECT:/run/gateway.sock") {
		t.Error("gateway-proxy entrypoint missing UNIX-CONNECT:/run/gateway.sock (Linux path)")
	}
	// Must support backend-provided host aliases for VM-backed runtimes.
	if !strings.Contains(content, "AGENCY_HOST_GATEWAY_HOSTS") {
		t.Error("gateway-proxy entrypoint missing AGENCY_HOST_GATEWAY_HOSTS")
	}
	if !strings.Contains(content, "host.docker.internal") {
		t.Error("gateway-proxy entrypoint missing host.docker.internal fallback")
	}
	if !strings.Contains(content, "host.containers.internal") {
		t.Error("gateway-proxy entrypoint missing host.containers.internal fallback")
	}
	if !strings.Contains(content, "AGENCY_HOST_GATEWAY_PORT") {
		t.Error("gateway-proxy entrypoint must use AGENCY_HOST_GATEWAY_PORT for disposable gateways")
	}
	for _, envName := range []string{"AGENCY_COMMS_HOST", "AGENCY_KNOWLEDGE_HOST", "AGENCY_INTAKE_HOST"} {
		if !strings.Contains(content, envName) {
			t.Errorf("gateway-proxy entrypoint missing %s", envName)
		}
	}
}

// TestDefaultImagesHaveHealthChecks verifies that every infra image in
// defaultImages also has a health check defined. Missing health checks
// cause containers to never become "healthy" and block startup.
func TestDefaultImagesHaveHealthChecks(t *testing.T) {
	// These images intentionally have no Docker health check:
	// - gateway-proxy: socat, starts instantly; readiness checked via waitSocketReady()
	// - relay, web, embeddings: non-service or external images
	noHealthCheck := map[string]bool{
		"gateway-proxy": true,
		"relay":         true,
		"web":           true,
		"embeddings":    true,
	}

	for name := range defaultImages {
		if noHealthCheck[name] {
			continue
		}
		if _, ok := defaultHealthChecks[name]; !ok {
			t.Errorf("defaultImages has %q but defaultHealthChecks does not — infra up will never consider it healthy", name)
		}
	}
}
