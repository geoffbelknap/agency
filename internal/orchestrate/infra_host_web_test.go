package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostWebPreviewEnvDisablesHTTPS(t *testing.T) {
	inf := &Infra{Home: t.TempDir(), BuildID: "build-1"}
	env := inf.hostWebPreviewEnv()
	if !envContains(env, "AGENCY_HOME="+inf.Home) {
		t.Fatalf("host web preview env missing AGENCY_HOME: %#v", env)
	}
	if !envContains(env, "BUILD_ID=build-1") {
		t.Fatalf("host web preview env missing BUILD_ID: %#v", env)
	}
	if !envContains(env, "VITE_DISABLE_HTTPS=1") {
		t.Fatalf("host web preview env missing VITE_DISABLE_HTTPS=1: %#v", env)
	}
}

func envContains(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func TestHostInfraLogsReadsTailFromMetadataLogFile(t *testing.T) {
	inf := &Infra{Home: t.TempDir(), RuntimeBackendName: "firecracker"}
	logPath := filepath.Join(inf.Home, "logs", "infra", "comms.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(inf.Home, "run"), 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	if err := inf.writeHostInfraMetadata("comms", 123, []string{"comms"}, logPath, "http://127.0.0.1:8202/health"); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	got, err := inf.InfraLogs(context.Background(), "comms", 2)
	if err != nil {
		t.Fatalf("InfraLogs: %v", err)
	}
	if got != "two\nthree\n" {
		t.Fatalf("logs = %q, want tail", got)
	}
}

func TestHostInfraLogsRejectsUnknownComponent(t *testing.T) {
	inf := &Infra{Home: t.TempDir()}
	_, err := inf.InfraLogs(context.Background(), "../comms", 10)
	if err == nil || !strings.Contains(err.Error(), "unknown infrastructure component") {
		t.Fatalf("InfraLogs error = %v, want unknown component", err)
	}
}
