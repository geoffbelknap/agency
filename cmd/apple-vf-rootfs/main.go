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
	var overlayBaseDir string
	var mke2fs string
	var vsockBridgeBinary string
	var sizeMiB int64
	var osName string
	var arch string
	var env envFlags
	flag.StringVar(&image, "image", "", "versioned OCI image reference to realize as an ext4 rootfs")
	flag.StringVar(&out, "out", "", "output ext4 rootfs path")
	flag.StringVar(&stateDir, "state-dir", "", "builder state directory")
	flag.StringVar(&overlayBaseDir, "overlay-base-dir", "", "base directory for safe rootfs overlays")
	flag.StringVar(&mke2fs, "mke2fs", "mke2fs", "mke2fs binary path")
	flag.StringVar(&vsockBridgeBinary, "vsock-bridge-binary", "", "Linux agency-vsock-http-bridge binary path to install in the rootfs")
	flag.Int64Var(&sizeMiB, "size-mib", 1024, "rootfs image size in MiB")
	flag.StringVar(&osName, "os", "linux", "target OS")
	flag.StringVar(&arch, "arch", "arm64", "target architecture")
	flag.Var(&env, "env", "guest environment KEY=VALUE; may be repeated")
	flag.Parse()

	if strings.TrimSpace(out) == "" {
		fmt.Fprintln(os.Stderr, "--out is required")
		os.Exit(2)
	}
	if strings.TrimSpace(image) == "" {
		fmt.Fprintln(os.Stderr, "--image is required")
		os.Exit(2)
	}
	if strings.HasSuffix(strings.TrimSpace(image), ":latest") {
		fmt.Fprintln(os.Stderr, "--image must be a versioned OCI artifact reference, not a mutable :latest tag")
		os.Exit(2)
	}
	builder := runtimebackend.MicroVMOCIRootFSBuilder{
		StateDir:          stateDir,
		Mke2fsPath:        mke2fs,
		SizeMiB:           sizeMiB,
		Platform:          ocispec.Platform{OS: osName, Architecture: arch},
		VsockBridgeBinary: vsockBridgeBinary,
		OverlayBaseDir:    overlayBaseDir,
	}
	result, err := builder.Build(context.Background(), image, out, env.Map())
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

type envFlags []string

func (e *envFlags) String() string {
	return strings.Join(*e, ",")
}

func (e *envFlags) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("env value is empty")
	}
	if !strings.Contains(value, "=") {
		return fmt.Errorf("env value %q must be KEY=VALUE", value)
	}
	*e = append(*e, value)
	return nil
}

func (e envFlags) Map() map[string]string {
	if len(e) == 0 {
		return nil
	}
	out := make(map[string]string, len(e))
	for _, item := range e {
		key, value, _ := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
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
