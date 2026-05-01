package runtimeprovision

import (
	"fmt"
	"path/filepath"
)

const (
	KernelReleaseTag     = "agency-kernels-6.12.22-r1"
	KernelReleaseBaseURL = "https://github.com/geoffbelknap/agency/releases/download/" + KernelReleaseTag
	KernelVersion        = "6.12.22"
)

type KernelArtifact struct {
	Runtime        string
	Arch           string
	AssetName      string
	ChecksumName   string
	FileName       string
	Format         string
	ReleaseTag     string
	ReleaseBaseURL string
}

func FirecrackerKernelArtifact(arch string) (KernelArtifact, error) {
	arch = normalizeFirecrackerArch(arch)
	switch arch {
	case "x86_64":
		return kernelArtifact("firecracker", arch, "vmlinux", "elf-vmlinux"), nil
	case "aarch64":
		return kernelArtifact("firecracker", arch, "Image", "arm64-Image"), nil
	default:
		return KernelArtifact{}, fmt.Errorf("unsupported Firecracker kernel architecture %q", arch)
	}
}

func AppleVFKernelArtifact() KernelArtifact {
	return kernelArtifact("apple-vf", "arm64", "Image", "arm64-Image")
}

func DefaultFirecrackerKernelPath(home, arch string) (string, error) {
	artifact, err := FirecrackerKernelArtifact(arch)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "runtime", "firecracker", "artifacts", artifact.FileName), nil
}

func DefaultAppleVFKernelPath(home string) string {
	return filepath.Join(home, "runtime", "apple-vf-microvm", "artifacts", AppleVFKernelArtifact().FileName)
}

func kernelArtifact(runtimeName, arch, fileName, format string) KernelArtifact {
	asset := fmt.Sprintf("agency-kernel-%s-%s-%s", KernelVersion, runtimeName, arch)
	return KernelArtifact{
		Runtime:        runtimeName,
		Arch:           arch,
		AssetName:      asset,
		ChecksumName:   asset + ".sha256",
		FileName:       fileName,
		Format:         format,
		ReleaseTag:     KernelReleaseTag,
		ReleaseBaseURL: KernelReleaseBaseURL,
	}
}
