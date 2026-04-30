package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
)

func main() {
	var image string
	var out string
	var stateDir string
	var mke2fs string
	var sizeMiB int64
	var osName string
	var arch string
	flag.StringVar(&image, "image", "ghcr.io/geoffbelknap/agency-body:latest", "OCI image reference")
	flag.StringVar(&out, "out", "", "output ext4 rootfs path")
	flag.StringVar(&stateDir, "state-dir", "", "builder state directory")
	flag.StringVar(&mke2fs, "mke2fs", "mke2fs", "mke2fs binary path")
	flag.Int64Var(&sizeMiB, "size-mib", 1024, "rootfs image size in MiB")
	flag.StringVar(&osName, "os", "linux", "target OS")
	flag.StringVar(&arch, "arch", "arm64", "target architecture")
	flag.Parse()

	if strings.TrimSpace(out) == "" {
		fmt.Fprintln(os.Stderr, "--out is required")
		os.Exit(2)
	}
	builder := runtimebackend.MicroVMOCIRootFSBuilder{
		StateDir:   stateDir,
		Mke2fsPath: mke2fs,
		SizeMiB:    sizeMiB,
		Platform:   ocispec.Platform{OS: osName, Architecture: arch},
	}
	result, err := builder.Build(context.Background(), image, out, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build rootfs: %v\n", err)
		os.Exit(1)
	}
	sum, err := fileSHA256(result.RootFSPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sha256 rootfs: %v\n", err)
		os.Exit(1)
	}
	fileOut, _ := exec.Command("file", "-b", result.RootFSPath).Output()
	info, err := os.Stat(result.RootFSPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat rootfs: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("image_ref=%s\n", result.ImageRef)
	fmt.Printf("manifest_digest=%s\n", result.Manifest.Digest)
	fmt.Printf("platform=%s/%s\n", result.Platform.OS, result.Platform.Architecture)
	fmt.Printf("config_platform=%s/%s\n", result.Config.OS, result.Config.Architecture)
	fmt.Printf("rootfs_path=%s\n", result.RootFSPath)
	fmt.Printf("sha256=%s\n", sum)
	fmt.Printf("file=%s\n", strings.TrimSpace(string(fileOut)))
	fmt.Printf("size_bytes=%d\n", info.Size())
	fmt.Printf("init_path=%s\n", result.InitPath)
	fmt.Printf("layer_count=%d\n", len(result.LayerDigests))
	fmt.Printf("container_runtime_used=false\n")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
