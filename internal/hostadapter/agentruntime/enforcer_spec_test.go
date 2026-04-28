package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildEnforcerLaunchSpecCapturesPerAgentBoundary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENCY_LOG_FORMAT", "text")
	if err := os.MkdirAll(filepath.Join(home, "agents", "alice", "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "infrastructure", "egress", "certs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem"), []byte("ca"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "infrastructure", "routing.yaml"), []byte("models: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("token: gateway-token\ngateway_addr: 127.0.0.1:8200\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	enforcer := &Enforcer{
		AgentName:          "alice",
		ContainerName:      "agency-alice-enforcer",
		Home:               home,
		BuildID:            "build-1",
		ProxyHostPort:      "19128",
		ConstraintHostPort: "19081",
		LifecycleID:        "life-1",
		PrincipalUUID:      "principal-1",
	}
	spec, err := enforcer.BuildLaunchSpec(context.Background(), false)
	if err != nil {
		t.Fatalf("BuildLaunchSpec returned error: %v", err)
	}
	if spec.AgentName != "alice" || spec.ComponentName != "agency-alice-enforcer" || spec.Image != enforcerImage {
		t.Fatalf("unexpected identity fields: %#v", spec)
	}
	if spec.ProxyHostPort != "19128" || spec.ConstraintHostPort != "19081" || spec.ConstraintPort != EnforcerConstraintPort || spec.ProxyPort != EnforcerProxyPort {
		t.Fatalf("unexpected ports: %#v", spec)
	}
	if spec.InternalNetwork != "agency-alice-internal" {
		t.Fatalf("internal network = %q", spec.InternalNetwork)
	}
	for key, want := range map[string]string{
		"AGENT_NAME":          "alice",
		"AGENCY_COMPONENT":    "enforcer",
		"AGENCY_CALLER":       "enforcer",
		"AGENCY_LOG_FORMAT":   "text",
		"AGENCY_LIFECYCLE_ID": "life-1",
		"BUILD_ID":            "build-1",
		"GATEWAY_TOKEN":       "gateway-token",
		"GATEWAY_URL":         "http://gateway:8200",
		"COMMS_URL":           "http://agency-infra-comms:8080",
		"KNOWLEDGE_URL":       "http://agency-infra-knowledge:8080",
		"WEB_FETCH_URL":       "http://agency-infra-web-fetch:8080",
		"SSL_CERT_FILE":       "/etc/ssl/certs/agency-egress-ca.pem",
	} {
		if got := spec.Env[key]; got != want {
			t.Fatalf("env[%s] = %q, want %q", key, got, want)
		}
	}
	for _, want := range []EnforcerMount{
		{HostPath: filepath.Join(home, "agents", "alice"), GuestPath: "/agency/agent", Mode: "ro"},
		{HostPath: filepath.Join(home, "audit", "alice", "enforcer"), GuestPath: "/agency/enforcer/audit", Mode: "rw"},
		{HostPath: filepath.Join(home, "infrastructure", "enforcer", "data", "alice"), GuestPath: "/agency/enforcer/data", Mode: "rw"},
		{HostPath: filepath.Join(home, "infrastructure", "routing.yaml"), GuestPath: "/agency/enforcer/routing.yaml", Mode: "ro"},
		{HostPath: filepath.Join(home, "agents", "alice", "memory"), GuestPath: "/agency/memory", Mode: "ro"},
	} {
		if !hasEnforcerMount(spec.Mounts, want) {
			t.Fatalf("missing mount %#v in %#v", want, spec.Mounts)
		}
	}
	keysFile := filepath.Join(home, "agents", "alice", "state", "enforcer-auth", "api_keys.yaml")
	keys, err := os.ReadFile(keysFile)
	if err != nil {
		t.Fatalf("read keys file: %v", err)
	}
	if !strings.Contains(string(keys), "agency-scoped--") {
		t.Fatalf("keys file missing scoped key: %s", string(keys))
	}
}

func TestEnforcerLaunchSpecContainerBinds(t *testing.T) {
	spec := EnforcerLaunchSpec{Mounts: []EnforcerMount{
		{HostPath: "/host/a", GuestPath: "/guest/a", Mode: "ro"},
		{HostPath: "/host/b", GuestPath: "/guest/b", Mode: "rw"},
	}}
	got := spec.ContainerBinds()
	want := []string{"/host/a:/guest/a:ro", "/host/b:/guest/b:rw"}
	if len(got) != len(want) {
		t.Fatalf("bind len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bind[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEnforcerLaunchSpecHostProcessEnv(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "egress-domains.yaml"), []byte("domains: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := EnforcerLaunchSpec{
		AgentName:          "alice",
		ProxyHostPort:      "19128",
		ConstraintHostPort: "19081",
		Env: map[string]string{
			"HOME":               "/agency/enforcer/data",
			"COMMS_URL":          "http://agency-infra-comms:8080",
			"CONSTRAINT_WS_PORT": EnforcerConstraintPort,
			"SSL_CERT_FILE":      "/etc/ssl/certs/agency-egress-ca.pem",
		},
		Mounts: []EnforcerMount{
			{HostPath: filepath.Join(home, "auth"), GuestPath: "/agency/enforcer/auth", Mode: "ro"},
			{HostPath: filepath.Join(home, "audit"), GuestPath: "/agency/enforcer/audit", Mode: "rw"},
			{HostPath: filepath.Join(home, "data"), GuestPath: "/agency/enforcer/data", Mode: "rw"},
			{HostPath: filepath.Join(home, "routing.yaml"), GuestPath: "/agency/enforcer/routing.yaml", Mode: "ro"},
			{HostPath: filepath.Join(home, "services"), GuestPath: "/agency/enforcer/services", Mode: "ro"},
			{HostPath: agentDir, GuestPath: "/agency/agent", Mode: "ro"},
			{HostPath: filepath.Join(home, "ca.pem"), GuestPath: "/etc/ssl/certs/agency-egress-ca.pem", Mode: "ro"},
		},
	}
	env := spec.HostProcessEnv(map[string]string{
		"gateway":   "http://127.0.0.1:8200",
		"comms":     "http://127.0.0.1:8202",
		"knowledge": "http://127.0.0.1:8204",
		"web-fetch": "http://127.0.0.1:8206",
	})
	for key, want := range map[string]string{
		"ENFORCER_PORT":           "19128",
		"ENFORCER_BIND_ADDR":      "127.0.0.1",
		"CONSTRAINT_WS_PORT":      "19081",
		"CONSTRAINT_WS_BIND_ADDR": "127.0.0.1",
		"HOME":                    filepath.Join(home, "data"),
		"API_KEYS_FILE":           filepath.Join(home, "auth", "api_keys.yaml"),
		"ENFORCER_LOG_DIR":        filepath.Join(home, "audit"),
		"ROUTING_CONFIG":          filepath.Join(home, "routing.yaml"),
		"SERVICES_DIR":            filepath.Join(home, "services"),
		"AGENT_DIR":               agentDir,
		"EGRESS_DOMAINS_FILE":     filepath.Join(agentDir, "egress-domains.yaml"),
		"SSL_CERT_FILE":           filepath.Join(home, "ca.pem"),
		"GATEWAY_URL":             "http://127.0.0.1:8200",
		"COMMS_URL":               "http://127.0.0.1:8202",
		"KNOWLEDGE_URL":           "http://127.0.0.1:8204",
		"WEB_FETCH_URL":           "http://127.0.0.1:8206",
	} {
		if got := env[key]; got != want {
			t.Fatalf("env[%s] = %q, want %q", key, got, want)
		}
	}
}

func hasEnforcerMount(mounts []EnforcerMount, want EnforcerMount) bool {
	for _, mount := range mounts {
		if mount == want {
			return true
		}
	}
	return false
}
