package hub

import "testing"

func TestPackageRegistry_StoresInstalledPackage(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	pkg := InstalledPackage{
		Kind:    "connector",
		Name:    "slack-interactivity",
		Version: "1.0.0",
		Trust:   "verified",
	}
	if err := reg.PutPackage(pkg); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}
	got, ok := reg.GetPackage("connector", "slack-interactivity")
	if !ok {
		t.Fatal("expected package to exist")
	}
	if got.Version != "1.0.0" {
		t.Fatalf("Version = %q, want 1.0.0", got.Version)
	}
}
