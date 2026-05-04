package orchestrate

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
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

func TestHostWebHealthyRequiresSPAEntrypoint(t *testing.T) {
	tests := []struct {
		name       string
		rootStatus int
		want       bool
	}{
		{name: "root not found", rootStatus: http.StatusNotFound, want: false},
		{name: "root ok", rootStatus: http.StatusOK, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					w.WriteHeader(http.StatusOK)
				case "/":
					w.WriteHeader(tt.rootStatus)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatalf("split server address: %v", err)
			}
			t.Setenv("AGENCY_WEB_PORT", port)

			inf := &Infra{Home: t.TempDir()}
			got := inf.hostWebHealthy(context.Background())
			if got != tt.want {
				t.Fatalf("hostWebHealthy() = %v, want %v", got, tt.want)
			}
		})
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

func TestHostInfraMetadataRecordsBuildID(t *testing.T) {
	inf := &Infra{Home: t.TempDir(), BuildID: "build-1"}
	if err := os.MkdirAll(filepath.Join(inf.Home, "run"), 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	if err := inf.writeHostInfraMetadata("egress", 123, []string{"egress"}, "/tmp/egress.log", "http://127.0.0.1:8312/health"); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	if got := inf.hostInfraBuildID("egress"); got != "build-1" {
		t.Fatalf("hostInfraBuildID() = %q, want build-1", got)
	}
	if !inf.hostInfraCurrentBuild("egress") {
		t.Fatalf("hostInfraCurrentBuild() = false, want true")
	}
}

func TestHostInfraCurrentBuildRejectsMissingOrStaleMetadata(t *testing.T) {
	inf := &Infra{Home: t.TempDir(), BuildID: "build-2"}
	if err := os.MkdirAll(filepath.Join(inf.Home, "run"), 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}

	if inf.hostInfraCurrentBuild("egress") {
		t.Fatalf("hostInfraCurrentBuild() = true with missing metadata, want false")
	}
	inf.BuildID = "build-1"
	if err := inf.writeHostInfraMetadata("egress", 123, []string{"egress"}, "/tmp/egress.log", "http://127.0.0.1:8312/health"); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	inf.BuildID = "build-2"

	if inf.hostInfraCurrentBuild("egress") {
		t.Fatalf("hostInfraCurrentBuild() = true with stale metadata, want false")
	}
}

func TestHostInfraLogsRejectsUnknownComponent(t *testing.T) {
	inf := &Infra{Home: t.TempDir()}
	_, err := inf.InfraLogs(context.Background(), "../comms", 10)
	if err == nil || !strings.Contains(err.Error(), "unknown infrastructure component") {
		t.Fatalf("InfraLogs error = %v, want unknown component", err)
	}
}
