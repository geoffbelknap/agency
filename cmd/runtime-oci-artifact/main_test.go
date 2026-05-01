package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanArtifactPath(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"/usr/local/bin/enforcer", "usr/local/bin/enforcer"},
		{"usr/local/bin/enforcer", "usr/local/bin/enforcer"},
		{"./usr/local/bin/enforcer", "usr/local/bin/enforcer"},
	} {
		if got := cleanArtifactPath(tt.in); got != tt.want {
			t.Fatalf("cleanArtifactPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractFileFromLayer(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "usr/local/bin/enforcer",
		Mode:     0o755,
		Size:     int64(len("binary")),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("binary")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "enforcer")
	found, mode, err := extractFileFromLayer(ociLayerGzip, bytes.NewReader(buf.Bytes()), "/usr/local/bin/enforcer", out)
	if err != nil {
		t.Fatalf("extractFileFromLayer returned error: %v", err)
	}
	if !found {
		t.Fatal("extractFileFromLayer did not find file")
	}
	if mode != 0o755 {
		t.Fatalf("mode = %o, want 755", mode)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary" {
		t.Fatalf("extracted data = %q", string(data))
	}
}
