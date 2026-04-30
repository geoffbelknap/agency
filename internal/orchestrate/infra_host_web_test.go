package orchestrate

import (
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
