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
// defined in Go constants appear in CLAUDE.md. Catches stale doc references
// when network names are renamed in code.
func TestNetworkConstantsInDocs(t *testing.T) {
	root := repoRoot(t)
	claudeMD := filepath.Join(root, "CLAUDE.md")

	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal("read CLAUDE.md:", err)
	}
	content := string(data)

	// These are the network constants from infra.go that should appear in docs.
	networks := map[string]string{
		"gatewayNet":   gatewayNet,
		"egressIntNet": egressIntNet,
		"egressExtNet": egressExtNet,
	}

	for constName, netName := range networks {
		if !strings.Contains(content, netName) {
			t.Errorf("CLAUDE.md does not mention network %q (Go const %s = %q) — update the Container topology section",
				netName, constName, netName)
		}
	}
}

// TestGatewayProxyNotSelfLoop parses the gateway-proxy entrypoint script and
// verifies that socat's upstream target is a Unix socket, not a TCP connection
// to the container's own hostname. A TCP self-loop (TCP:gateway:8200 when the
// container IS "gateway") was the root cause of all inter-container communication
// failures.
func TestGatewayProxyNotSelfLoop(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "images", "gateway-proxy", "entrypoint.sh")

	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal("read gateway-proxy entrypoint.sh:", err)
	}
	content := string(data)

	// The container→gateway bridge must use a Unix socket, not TCP to its own alias
	if strings.Contains(content, "TCP:gateway") {
		t.Errorf("gateway-proxy entrypoint forwards to TCP:gateway — this is a self-loop "+
			"because the container IS 'gateway' on the Docker network. "+
			"Use UNIX-CONNECT:/run/gateway.sock instead.")
	}
	if !strings.Contains(content, "UNIX-CONNECT:/run/gateway.sock") {
		t.Errorf("gateway-proxy entrypoint does not use UNIX-CONNECT:/run/gateway.sock — "+
			"the container→gateway bridge must use the Unix socket.")
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
