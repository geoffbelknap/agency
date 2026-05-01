package runtimeprovision

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type AppleVFOptions struct {
	Home                 string
	KernelPath           string
	KernelReleaseBaseURL string
	Arch                 string
	Force                bool
	Logf                 func(string, ...any)
}

func ProvisionAppleVFKernel(ctx context.Context, opt AppleVFOptions) error {
	arch := strings.TrimSpace(opt.Arch)
	if arch == "" {
		arch = runtime.GOARCH
	}
	if arch != "arm64" && arch != "aarch64" {
		return fmt.Errorf("automatic Apple VF kernel provisioning supports arm64 only; set AGENCY_APPLE_VF_KERNEL to a verified Agency Image for %s", arch)
	}

	home := strings.TrimSpace(opt.Home)
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		home = filepath.Join(userHome, ".agency")
	}

	artifact := AppleVFKernelArtifact()
	kernelPath := firstNonEmpty(opt.KernelPath, DefaultAppleVFKernelPath(home))
	if !opt.Force && readable(kernelPath) {
		return nil
	}

	logf := opt.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	logf("Fetching Agency Apple VF kernel artifact")

	baseURL := strings.TrimSpace(opt.KernelReleaseBaseURL)
	if baseURL == "" {
		baseURL = artifact.ReleaseBaseURL
	}
	client := &http.Client{Timeout: 0}
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
		return fmt.Errorf("read Apple VF kernel checksum: %w", err)
	}
	if err := verifySHA256(kernel, expected); err != nil {
		return fmt.Errorf("verify Apple VF kernel checksum: %w", err)
	}
	if err := validateKernelArtifact(kernel, artifact); err != nil {
		return err
	}
	return writeFile(kernelPath, kernel, 0644)
}
