package containers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostConfigDefaults_Workspace(t *testing.T) {
	hc := HostConfigDefaults(RoleWorkspace)

	if hc.Memory != 512*1024*1024 {
		t.Errorf("workspace Memory = %d, want %d", hc.Memory, 512*1024*1024)
	}
	if hc.NanoCPUs != 2_000_000_000 {
		t.Errorf("workspace NanoCPUs = %d, want 2e9", hc.NanoCPUs)
	}
	if hc.PidsLimit == nil || *hc.PidsLimit != 512 {
		t.Errorf("workspace PidsLimit = %v, want 512", hc.PidsLimit)
	}
	if hc.RestartPolicy.Name != "on-failure" || hc.RestartPolicy.MaximumRetryCount != 3 {
		t.Errorf("workspace RestartPolicy = %+v, want on-failure/3", hc.RestartPolicy)
	}
	if hc.LogConfig.Config["max-size"] != "10m" || hc.LogConfig.Config["max-file"] != "3" {
		t.Errorf("workspace LogConfig = %+v", hc.LogConfig)
	}
	if !hc.ReadonlyRootfs {
		t.Error("workspace ReadonlyRootfs should be true")
	}
	if len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Errorf("workspace CapDrop = %v, want [ALL]", hc.CapDrop)
	}
}

func TestHostConfigDefaults_Enforcer(t *testing.T) {
	hc := HostConfigDefaults(RoleEnforcer)

	if hc.Memory != 128*1024*1024 {
		t.Errorf("enforcer Memory = %d, want %d", hc.Memory, 128*1024*1024)
	}
	if hc.NanoCPUs != 500_000_000 {
		t.Errorf("enforcer NanoCPUs = %d, want 5e8", hc.NanoCPUs)
	}
	if hc.PidsLimit == nil || *hc.PidsLimit != 256 {
		t.Errorf("enforcer PidsLimit = %v, want 256", hc.PidsLimit)
	}
	if hc.RestartPolicy.Name != "on-failure" || hc.RestartPolicy.MaximumRetryCount != 3 {
		t.Errorf("enforcer RestartPolicy = %+v, want on-failure/3", hc.RestartPolicy)
	}
	if hc.LogConfig.Config["max-size"] != "10m" {
		t.Errorf("enforcer LogConfig max-size = %s, want 10m", hc.LogConfig.Config["max-size"])
	}
}

func TestHostConfigDefaults_Infra(t *testing.T) {
	hc := HostConfigDefaults(RoleInfra)

	if hc.Memory != 256*1024*1024 {
		t.Errorf("infra Memory = %d, want %d", hc.Memory, 256*1024*1024)
	}
	if hc.NanoCPUs != 1_000_000_000 {
		t.Errorf("infra NanoCPUs = %d, want 1e9", hc.NanoCPUs)
	}
	if hc.PidsLimit == nil || *hc.PidsLimit != 1024 {
		t.Errorf("infra PidsLimit = %v, want 1024", hc.PidsLimit)
	}
	if hc.RestartPolicy.Name != "unless-stopped" {
		t.Errorf("infra RestartPolicy = %+v, want unless-stopped", hc.RestartPolicy)
	}
	if hc.ReadonlyRootfs {
		t.Error("infra ReadonlyRootfs should be false")
	}
}

func TestHostConfigDefaults_Meeseeks(t *testing.T) {
	hc := HostConfigDefaults(RoleMeeseeks)

	if hc.Memory != 512*1024*1024 {
		t.Errorf("meeseeks Memory = %d, want %d", hc.Memory, 512*1024*1024)
	}
	if hc.NanoCPUs != 1_000_000_000 {
		t.Errorf("meeseeks NanoCPUs = %d, want 1e9", hc.NanoCPUs)
	}
	if hc.PidsLimit == nil || *hc.PidsLimit != 512 {
		t.Errorf("meeseeks PidsLimit = %v, want 512", hc.PidsLimit)
	}
	if hc.RestartPolicy.Name != "no" {
		t.Errorf("meeseeks RestartPolicy = %+v, want no", hc.RestartPolicy)
	}
	if hc.LogConfig.Config["max-size"] != "5m" || hc.LogConfig.Config["max-file"] != "2" {
		t.Errorf("meeseeks LogConfig = %+v", hc.LogConfig)
	}
	if !hc.ReadonlyRootfs {
		t.Error("meeseeks ReadonlyRootfs should be true")
	}
}

func TestHostConfigDefaults_AllRolesHaveSecurityBaseline(t *testing.T) {
	roles := []ContainerRole{RoleWorkspace, RoleEnforcer, RoleInfra, RoleMeeseeks}
	for _, role := range roles {
		hc := HostConfigDefaults(role)
		if len(hc.CapDrop) == 0 || hc.CapDrop[0] != "ALL" {
			t.Errorf("role %s: CapDrop should be [ALL], got %v", role, hc.CapDrop)
		}
		if len(hc.CapAdd) == 0 || hc.CapAdd[0] != "NET_BIND_SERVICE" {
			t.Errorf("role %s: CapAdd should contain NET_BIND_SERVICE, got %v", role, hc.CapAdd)
		}
		found := false
		for _, opt := range hc.SecurityOpt {
			if opt == "no-new-privileges:true" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("role %s: SecurityOpt missing no-new-privileges:true, got %v", role, hc.SecurityOpt)
		}
		if hc.LogConfig.Type != "json-file" {
			t.Errorf("role %s: LogConfig.Type = %s, want json-file", role, hc.LogConfig.Type)
		}
	}
}

func TestSeccompProfile_FallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()
	profile := SeccompProfile(dir)
	if profile == "" {
		t.Fatal("SeccompProfile returned empty string")
	}
	// Embedded profile should contain valid JSON markers.
	if len(profile) < 10 {
		t.Errorf("SeccompProfile too short: %q", profile)
	}
}

func TestSeccompProfile_ReadsOverrideFile(t *testing.T) {
	dir := t.TempDir()
	infraDir := filepath.Join(dir, "infrastructure")
	if err := os.MkdirAll(infraDir, 0755); err != nil {
		t.Fatal(err)
	}
	want := `{"custom":"profile"}`
	if err := os.WriteFile(filepath.Join(infraDir, "seccomp-workspace.json"), []byte(want), 0644); err != nil {
		t.Fatal(err)
	}

	got := SeccompProfile(dir)
	if got != want {
		t.Errorf("SeccompProfile = %q, want %q", got, want)
	}
}
