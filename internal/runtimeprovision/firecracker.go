package runtimeprovision

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultFirecrackerVersion = "v1.12.1"
)

type FirecrackerOptions struct {
	AgencyVersion        string
	Home                 string
	BinaryPath           string
	KernelPath           string
	FirecrackerBaseURL   string
	KernelReleaseBaseURL string
	Arch                 string
	Force                bool
	Logf                 func(string, ...any)
}

func ProvisionFirecracker(ctx context.Context, opt FirecrackerOptions) error {
	arch := normalizeFirecrackerArch(firstNonEmpty(opt.Arch, runtime.GOARCH))
	if arch == "" {
		return fmt.Errorf("unsupported Firecracker architecture %q", firstNonEmpty(opt.Arch, runtime.GOARCH))
	}
	kernelArtifact, err := FirecrackerKernelArtifact(arch)
	if err != nil {
		return err
	}

	home := strings.TrimSpace(opt.Home)
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		home = filepath.Join(userHome, ".agency")
	}
	artifactDir := filepath.Join(home, "runtime", "firecracker", "artifacts")
	binaryPath := firstNonEmpty(opt.BinaryPath, filepath.Join(artifactDir, DefaultFirecrackerVersion, "firecracker-"+DefaultFirecrackerVersion+"-"+arch))
	kernelPath := firstNonEmpty(opt.KernelPath, filepath.Join(artifactDir, kernelArtifact.FileName))

	if !opt.Force {
		if executable(binaryPath) && readable(kernelPath) {
			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(kernelPath), 0755); err != nil {
		return err
	}

	logf := opt.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	client := &http.Client{Timeout: 0}
	if !opt.Force && executable(binaryPath) {
		logf("Firecracker binary already present at %s", binaryPath)
	} else {
		logf("Fetching pinned Firecracker %s release artifact", DefaultFirecrackerVersion)
		if err := provisionFirecrackerBinary(ctx, client, opt.FirecrackerBaseURL, arch, binaryPath); err != nil {
			return err
		}
	}

	if !opt.Force && readable(kernelPath) {
		logf("Firecracker kernel already present at %s", kernelPath)
		return nil
	}
	logf("Fetching Agency Firecracker kernel artifact")
	if err := provisionFirecrackerKernel(ctx, client, opt.KernelReleaseBaseURL, kernelArtifact, kernelPath); err != nil {
		return err
	}
	return nil
}

func provisionFirecrackerBinary(ctx context.Context, client *http.Client, baseURL, arch, dest string) error {
	if baseURL == "" {
		baseURL = "https://github.com/firecracker-microvm/firecracker/releases/download/" + DefaultFirecrackerVersion
	}
	tarballName := "firecracker-" + DefaultFirecrackerVersion + "-" + arch + ".tgz"
	tarballURL := strings.TrimRight(baseURL, "/") + "/" + tarballName
	shaURL := tarballURL + ".sha256.txt"

	tarball, err := download(ctx, client, tarballURL)
	if err != nil {
		return err
	}
	shaFile, err := download(ctx, client, shaURL)
	if err != nil {
		return err
	}
	expected, err := firstSHA256Field(string(shaFile))
	if err != nil {
		return fmt.Errorf("read Firecracker checksum: %w", err)
	}
	if err := verifySHA256(tarball, expected); err != nil {
		return fmt.Errorf("verify Firecracker tarball checksum: %w", err)
	}
	bin, err := extractFirecrackerBinary(tarball, "firecracker-"+DefaultFirecrackerVersion+"-"+arch)
	if err != nil {
		return err
	}
	return writeExecutable(dest, bin)
}

func provisionFirecrackerKernel(ctx context.Context, client *http.Client, baseURL string, artifact KernelArtifact, dest string) error {
	if baseURL == "" {
		baseURL = artifact.ReleaseBaseURL
	}
	kernel, err := download(ctx, client, strings.TrimRight(baseURL, "/")+"/"+artifact.AssetName)
	if err != nil {
		return err
	}
	shaFile, err := download(ctx, client, strings.TrimRight(baseURL, "/")+"/"+artifact.ChecksumName)
	if err != nil {
		return err
	}
	expected, err := firstSHA256Field(string(shaFile))
	if err != nil {
		return fmt.Errorf("read Firecracker kernel checksum: %w", err)
	}
	if err := verifySHA256(kernel, expected); err != nil {
		return fmt.Errorf("verify Firecracker kernel checksum: %w", err)
	}
	if err := validateKernelArtifact(kernel, artifact); err != nil {
		return err
	}
	return writeFile(dest, kernel, 0644)
}

func validateKernelArtifact(kernel []byte, artifact KernelArtifact) error {
	if len(kernel) == 0 {
		return errors.New("downloaded kernel artifact is empty")
	}
	switch artifact.Format {
	case "elf-vmlinux":
		if len(kernel) < 4 || string(kernel[:4]) != "\x7fELF" {
			return errors.New("downloaded kernel is not an uncompressed ELF vmlinux artifact")
		}
	case "arm64-Image":
		if len(kernel) < 64 {
			return errors.New("downloaded ARM64 Image kernel artifact is unexpectedly small")
		}
	default:
		return fmt.Errorf("unsupported kernel artifact format %q", artifact.Format)
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func extractFirecrackerBinary(tarball []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		clean := path.Clean(h.Name)
		if path.Base(clean) != name {
			continue
		}
		return io.ReadAll(tr)
	}
	return nil, fmt.Errorf("Firecracker release did not contain %s", name)
}

func firstSHA256Field(content string) (string, error) {
	fields := strings.Fields(content)
	for _, field := range fields {
		if len(field) == sha256.Size*2 {
			if _, err := hex.DecodeString(field); err == nil {
				return strings.ToLower(field), nil
			}
		}
	}
	return "", errors.New("no sha256 digest found")
}

func verifySHA256(data []byte, expected string) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != strings.ToLower(strings.TrimSpace(expected)) {
		return fmt.Errorf("got %s want %s", actual, expected)
	}
	return nil
}

func writeExecutable(dest string, data []byte) error {
	return writeFile(dest, data, 0755)
}

func writeFile(dest string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

func releaseTag(version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" || version == "unknown" {
		return "", errors.New("Agency release version is not known; install a released Agency build or set AGENCY_FIRECRACKER_KERNEL_RELEASE_BASE_URL")
	}
	if strings.HasPrefix(version, "v") {
		return version, nil
	}
	return "v" + version, nil
}

func normalizeFirecrackerArch(arch string) string {
	switch strings.TrimSpace(arch) {
	case "amd64", "x86_64":
		return "x86_64"
	case "arm64", "aarch64":
		return "aarch64"
	default:
		return ""
	}
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

func readable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
