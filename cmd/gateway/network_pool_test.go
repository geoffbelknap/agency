package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerPoolConfiguredMissingFile(t *testing.T) {
	ok, err := dockerPoolConfigured(filepath.Join(t.TempDir(), "daemon.json"))
	if err != nil {
		t.Fatalf("dockerPoolConfigured returned error: %v", err)
	}
	if ok {
		t.Fatal("expected missing file to report unconfigured pool")
	}
}

func TestDockerPoolConfiguredRecognizesConfiguredPool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	data := `{"default-address-pools":[{"base":"172.16.0.0/12","size":24}]}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write daemon.json: %v", err)
	}

	ok, err := dockerPoolConfigured(path)
	if err != nil {
		t.Fatalf("dockerPoolConfigured returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected configured pool to be detected")
	}
}

func TestDockerPoolConfiguredRejectsDefaultSizedPool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	data := `{"default-address-pools":[{"base":"172.16.0.0/12","size":16}]}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write daemon.json: %v", err)
	}

	ok, err := dockerPoolConfigured(path)
	if err != nil {
		t.Fatalf("dockerPoolConfigured returned error: %v", err)
	}
	if ok {
		t.Fatal("expected /16 pool to be treated as unconfigured")
	}
}

func TestConfigureDockerPoolMergesExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	data := `{"debug":true}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write daemon.json: %v", err)
	}

	if err := configureDockerPool(path); err != nil {
		t.Fatalf("configureDockerPool returned error: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read daemon.json: %v", err)
	}
	content := string(updated)
	if !strings.Contains(content, `"debug": true`) {
		t.Fatalf("expected existing config to be preserved, got %s", content)
	}
	if !strings.Contains(content, `"default-address-pools"`) {
		t.Fatalf("expected default-address-pools to be added, got %s", content)
	}
	if !strings.Contains(content, `"size": 24`) {
		t.Fatalf("expected /24 pool size to be added, got %s", content)
	}
}
