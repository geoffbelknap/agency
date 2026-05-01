package runtimeprovision

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProvisionFirecrackerDownloadsPinnedArtifacts(t *testing.T) {
	firecracker := []byte("#!/bin/sh\n")
	kernel := append([]byte("\x7fELF"), []byte("agency kernel")...)
	tarball := firecrackerTarball(t, "release-v1.12.1-x86_64/firecracker-v1.12.1-x86_64", firecracker)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/firecracker/firecracker-v1.12.1-x86_64.tgz":
			_, _ = w.Write(tarball)
		case "/firecracker/firecracker-v1.12.1-x86_64.tgz.sha256.txt":
			fmt.Fprintf(w, "%x  firecracker-v1.12.1-x86_64.tgz\n", sha256.Sum256(tarball))
		case "/kernel/agency-firecracker-vmlinux_x86_64":
			_, _ = w.Write(kernel)
		case "/kernel/agency-firecracker-vmlinux_x86_64.sha256":
			fmt.Fprintf(w, "%x  agency-firecracker-vmlinux_x86_64\n", sha256.Sum256(kernel))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	err := ProvisionFirecracker(t.Context(), FirecrackerOptions{
		AgencyVersion:        "0.3.4-rc4",
		Home:                 home,
		FirecrackerBaseURL:   server.URL + "/firecracker",
		KernelReleaseBaseURL: server.URL + "/kernel",
		Arch:                 "x86_64",
	})
	if err != nil {
		t.Fatalf("ProvisionFirecracker() error = %v", err)
	}

	binaryPath := filepath.Join(home, "runtime", "firecracker", "artifacts", "v1.12.1", "firecracker-v1.12.1-x86_64")
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("stat binary: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatalf("binary mode = %v, want executable", info.Mode())
	}
	kernelPath := filepath.Join(home, "runtime", "firecracker", "artifacts", "vmlinux")
	got, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Fatalf("read kernel: %v", err)
	}
	if !bytes.Equal(got, kernel) {
		t.Fatalf("kernel bytes = %q, want %q", got, kernel)
	}
}

func TestProvisionFirecrackerRejectsKernelChecksumMismatch(t *testing.T) {
	firecracker := []byte("#!/bin/sh\n")
	kernel := append([]byte("\x7fELF"), []byte("agency kernel")...)
	tarball := firecrackerTarball(t, "firecracker-v1.12.1-x86_64", firecracker)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/firecracker/firecracker-v1.12.1-x86_64.tgz":
			_, _ = w.Write(tarball)
		case "/firecracker/firecracker-v1.12.1-x86_64.tgz.sha256.txt":
			fmt.Fprintf(w, "%x  firecracker-v1.12.1-x86_64.tgz\n", sha256.Sum256(tarball))
		case "/kernel/agency-firecracker-vmlinux_x86_64":
			_, _ = w.Write(kernel)
		case "/kernel/agency-firecracker-vmlinux_x86_64.sha256":
			_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  agency-firecracker-vmlinux_x86_64\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := ProvisionFirecracker(t.Context(), FirecrackerOptions{
		AgencyVersion:        "0.3.4-rc4",
		Home:                 t.TempDir(),
		FirecrackerBaseURL:   server.URL + "/firecracker",
		KernelReleaseBaseURL: server.URL + "/kernel",
		Arch:                 "x86_64",
	})
	if err == nil {
		t.Fatal("ProvisionFirecracker() error = nil, want checksum mismatch")
	}
}

func firecrackerTarball(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
