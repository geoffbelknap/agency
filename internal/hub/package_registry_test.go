package hub

import (
	"testing"
)

func TestPackageRegistry_StoresInstalledPackage(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	pkg := InstalledPackage{
		Kind:      "connector",
		Name:      "slack-interactivity",
		Version:   "1.0.0",
		Trust:     "verified",
		Assurance: []string{"publisher_verified", "ask_partial"},
		Spec: map[string]any{
			"runtime": map[string]any{
				"executor": map[string]any{"kind": "http_json"},
			},
		},
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
	if len(got.Assurance) != 2 || got.Assurance[0] != "publisher_verified" {
		t.Fatalf("Assurance = %#v", got.Assurance)
	}
	if runtimeSpec, ok := got.Spec["runtime"].(map[string]any); !ok || runtimeSpec["executor"] == nil {
		t.Fatalf("Spec = %#v", got.Spec)
	}
}

func TestPackageRegistry_ListPackagesAcrossKinds(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	packages := []InstalledPackage{
		{Kind: "connector", Name: "slack-interactivity", Version: "1.0.0", Trust: "verified"},
		{Kind: "pack", Name: "security-ops", Version: "2.0.0", Trust: "community"},
	}

	for _, pkg := range packages {
		if err := reg.PutPackage(pkg); err != nil {
			t.Fatalf("PutPackage(%s/%s): %v", pkg.Kind, pkg.Name, err)
		}
	}

	got, err := reg.ListPackages("")
	if err != nil {
		t.Fatalf("ListPackages(\"\"): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListPackages(\"\") len = %d, want 2", len(got))
	}

	seen := map[string]bool{}
	for _, pkg := range got {
		seen[pkg.Kind+"/"+pkg.Name] = true
	}
	for _, want := range []string{"connector/slack-interactivity", "pack/security-ops"} {
		if !seen[want] {
			t.Fatalf("missing package %q from unfiltered list: %+v", want, got)
		}
	}
}

func TestPackageRegistry_RejectsInvalidPathSegments(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	for _, pkg := range []InstalledPackage{
		{Kind: "connector/evil", Name: "slack-interactivity", Version: "1.0.0", Trust: "verified"},
		{Kind: "connector", Name: "../slack-interactivity", Version: "1.0.0", Trust: "verified"},
	} {
		if err := reg.PutPackage(pkg); err == nil {
			t.Fatalf("PutPackage(%q, %q) expected error, got nil", pkg.Kind, pkg.Name)
		}
	}
}
