package runtimebackend

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractOCILayerAppliesWhiteouts(t *testing.T) {
	stageDir := t.TempDir()

	if err := extractOCILayer(stageDir, "application/vnd.oci.image.layer.v1.tar", tarLayer(t,
		tarEntry{path: "etc/", typ: tar.TypeDir, mode: 0o755},
		tarEntry{path: "etc/old.conf", body: "old", mode: 0o644},
		tarEntry{path: "etc/keep.conf", body: "keep", mode: 0o644},
	)); err != nil {
		t.Fatalf("extract base layer: %v", err)
	}
	if err := extractOCILayer(stageDir, "application/vnd.oci.image.layer.v1.tar", tarLayer(t,
		tarEntry{path: "etc/.wh.old.conf", body: "", mode: 0o000},
		tarEntry{path: "etc/new.conf", body: "new", mode: 0o600},
	)); err != nil {
		t.Fatalf("extract whiteout layer: %v", err)
	}

	assertNotExists(t, filepath.Join(stageDir, "etc", "old.conf"))
	assertFile(t, filepath.Join(stageDir, "etc", "keep.conf"), "keep", 0o644)
	assertFile(t, filepath.Join(stageDir, "etc", "new.conf"), "new", 0o600)
}

func TestExtractOCILayerAppliesOpaqueWhiteout(t *testing.T) {
	stageDir := t.TempDir()

	if err := extractOCILayer(stageDir, "application/vnd.oci.image.layer.v1.tar", tarLayer(t,
		tarEntry{path: "var/lib/app/", typ: tar.TypeDir, mode: 0o755},
		tarEntry{path: "var/lib/app/base", body: "base", mode: 0o644},
	)); err != nil {
		t.Fatalf("extract base layer: %v", err)
	}
	if err := extractOCILayer(stageDir, "application/vnd.oci.image.layer.v1.tar", tarLayer(t,
		tarEntry{path: "var/lib/app/.wh..wh..opq", body: "", mode: 0o000},
		tarEntry{path: "var/lib/app/final", body: "final", mode: 0o644},
	)); err != nil {
		t.Fatalf("extract opaque layer: %v", err)
	}

	assertNotExists(t, filepath.Join(stageDir, "var", "lib", "app", "base"))
	assertFile(t, filepath.Join(stageDir, "var", "lib", "app", "final"), "final", 0o644)
}

func TestExtractOCILayerCreatesSymlinkWithoutChmodTarget(t *testing.T) {
	stageDir := t.TempDir()

	if err := extractOCILayer(stageDir, "application/vnd.oci.image.layer.v1.tar", tarLayer(t,
		tarEntry{path: "usr/", typ: tar.TypeDir, mode: 0o755},
		tarEntry{path: "usr/bin/", typ: tar.TypeDir, mode: 0o755},
		tarEntry{path: "bin", typ: tar.TypeSymlink, link: "usr/bin", mode: 0o777},
	)); err != nil {
		t.Fatalf("extract symlink layer: %v", err)
	}

	link, err := os.Readlink(filepath.Join(stageDir, "bin"))
	if err != nil {
		t.Fatalf("read symlink: %v", err)
	}
	if link != "usr/bin" {
		t.Fatalf("symlink target = %q, want %q", link, "usr/bin")
	}
}

func TestExtractOCILayerRejectsPathTraversal(t *testing.T) {
	stageDir := t.TempDir()

	if err := extractOCILayer(stageDir, "application/vnd.oci.image.layer.v1.tar", tarLayer(t,
		tarEntry{path: "../escape", body: "nope", mode: 0o644},
	)); err == nil {
		t.Fatal("extract traversal layer succeeded")
	}
}

func TestExtractOCILayerDoesNotWriteThroughAbsoluteSymlink(t *testing.T) {
	stageDir := t.TempDir()
	outsideDir := t.TempDir()

	if err := extractOCILayer(stageDir, "application/vnd.oci.image.layer.v1.tar", tarLayer(t,
		tarEntry{path: "escape", typ: tar.TypeSymlink, link: outsideDir, mode: 0o777},
		tarEntry{path: "escape/pwned", body: "nope", mode: 0o644},
	)); err == nil {
		t.Fatal("extract layer wrote through absolute symlink")
	}
	assertNotExists(t, filepath.Join(outsideDir, "pwned"))
}

type tarEntry struct {
	path string
	typ  byte
	body string
	link string
	mode int64
}

func tarLayer(t *testing.T, entries ...tarEntry) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, entry := range entries {
		typ := entry.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 && typ != tar.TypeDir {
			mode = 0o644
		}
		header := &tar.Header{
			Name:     entry.path,
			Typeflag: typ,
			Mode:     mode,
			Linkname: entry.link,
			Size:     int64(len(entry.body)),
		}
		if typ == tar.TypeDir || typ == tar.TypeSymlink {
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", entry.path, err)
		}
		if header.Size > 0 {
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write tar body %s: %v", entry.path, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

func assertFile(t *testing.T, path, want string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Fatalf("%s mode = %o, want %o", path, got, mode)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s exists or stat returned unexpected error: %v", path, err)
	}
}
