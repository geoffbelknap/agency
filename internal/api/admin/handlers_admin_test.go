package admin

import "testing"

func TestSplitDoctorChecksSeparatesDockerBackendHygiene(t *testing.T) {
	t.Parallel()

	checks := []doctorCheckResult{
		{Name: "credentials_isolated", Agent: "henry", Status: "pass"},
		{Name: "host_capacity", Status: "pass"},
		{Name: "docker_dangling_images", Status: "warn"},
		{Name: "network_pool", Status: "warn"},
	}

	runtimeChecks, backendChecks := splitDoctorChecks(checks, "docker")

	if len(runtimeChecks) != 2 {
		t.Fatalf("runtimeChecks len = %d, want 2", len(runtimeChecks))
	}
	if len(backendChecks) != 2 {
		t.Fatalf("backendChecks len = %d, want 2", len(backendChecks))
	}
	if runtimeChecks[0].Name != "credentials_isolated" || runtimeChecks[1].Name != "host_capacity" {
		t.Fatalf("unexpected runtime checks: %#v", runtimeChecks)
	}
	if backendChecks[0].Name != "docker_dangling_images" || backendChecks[1].Name != "network_pool" {
		t.Fatalf("unexpected backend checks: %#v", backendChecks)
	}
}

func TestConfiguredRuntimeBackendDefaultsToDocker(t *testing.T) {
	t.Parallel()

	if got := configuredRuntimeBackend(nil); got != "docker" {
		t.Fatalf("configuredRuntimeBackend(nil) = %q, want docker", got)
	}
}
