package runtimebackend

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

type MicroVMOCIRootFSBuilder struct {
	StateDir          string
	Mke2fsPath        string
	SizeMiB           int64
	VsockBridgeBinary string
	Platform          ocispec.Platform
}

type MicroVMOCIRootFSResult struct {
	ImageRef     string
	Manifest     ocispec.Descriptor
	Config       ocispec.Image
	RootFSPath   string
	StageDir     string
	InitPath     string
	Platform     ocispec.Platform
	LayerDigests []string
}

func (b *MicroVMOCIRootFSBuilder) Build(ctx context.Context, imageRef, outPath string, env map[string]string) (MicroVMOCIRootFSResult, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("microvm OCI rootfs: image ref is required")
	}
	outPath = strings.TrimSpace(outPath)
	if outPath == "" {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("microvm OCI rootfs: output path is required")
	}
	repoRef, reference, err := splitRegistryReference(imageRef)
	if err != nil {
		return MicroVMOCIRootFSResult{}, err
	}
	repo, err := newMicroVMOCIRepository(repoRef)
	if err != nil {
		return MicroVMOCIRootFSResult{}, err
	}
	platform := b.platform()
	manifestDesc, manifestBytes, err := oras.FetchBytes(ctx, repo, reference, oras.FetchBytesOptions{
		FetchOptions: oras.FetchOptions{
			ResolveOptions: oras.ResolveOptions{TargetPlatform: &platform},
		},
	})
	if err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("fetch OCI image %s for %s/%s: %w", imageRef, platform.OS, platform.Architecture, err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("parse OCI image manifest: %w", err)
	}
	configBytes, err := fetchOCIBytes(ctx, repo, manifest.Config)
	if err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("fetch OCI image config: %w", err)
	}
	var imageConfig ocispec.Image
	if err := json.Unmarshal(configBytes, &imageConfig); err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("parse OCI image config: %w", err)
	}
	if imageConfig.OS != "" && imageConfig.OS != platform.OS {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("OCI image OS = %s, want %s", imageConfig.OS, platform.OS)
	}
	if imageConfig.Architecture != "" && imageConfig.Architecture != platform.Architecture {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("OCI image architecture = %s, want %s", imageConfig.Architecture, platform.Architecture)
	}

	tmpBase := filepath.Join(b.stateDir(), "tmp")
	if err := os.MkdirAll(tmpBase, 0o755); err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("create microvm OCI temp dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(tmpBase, "ocifs-*")
	if err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("create microvm OCI temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	stageDir := filepath.Join(tmpDir, "stage")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("create microvm OCI stage dir: %w", err)
	}
	var layerDigests []string
	for _, layer := range manifest.Layers {
		rc, err := repo.Fetch(ctx, layer)
		if err != nil {
			return MicroVMOCIRootFSResult{}, fmt.Errorf("fetch OCI layer %s: %w", layer.Digest, err)
		}
		if err := extractOCILayer(stageDir, layer.MediaType, rc); err != nil {
			_ = rc.Close()
			return MicroVMOCIRootFSResult{}, fmt.Errorf("extract OCI layer %s: %w", layer.Digest, err)
		}
		if err := rc.Close(); err != nil {
			return MicroVMOCIRootFSResult{}, fmt.Errorf("close OCI layer %s: %w", layer.Digest, err)
		}
		layerDigests = append(layerDigests, layer.Digest.String())
	}
	if err := applyFirecrackerRootFSOverlays(stageDir, env, b.stateDir()); err != nil {
		return MicroVMOCIRootFSResult{}, err
	}
	command := append([]string{}, imageConfig.Config.Entrypoint...)
	command = append(command, imageConfig.Config.Cmd...)
	if err := writeFirecrackerInit(stageDir, command, firecrackerGuestEnv(env)); err != nil {
		return MicroVMOCIRootFSResult{}, err
	}
	if err := installFirecrackerVsockBridge(stageDir, b.VsockBridgeBinary); err != nil {
		return MicroVMOCIRootFSResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("create microvm rootfs artifact dir: %w", err)
	}
	tmpImage := filepath.Join(tmpDir, "rootfs.ext4")
	if err := exec.CommandContext(ctx, "truncate", "-s", fmt.Sprintf("%dM", b.sizeMiB()), tmpImage).Run(); err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("allocate microvm rootfs image: %w", err)
	}
	if err := exec.CommandContext(ctx, b.mke2fsPath(), "-q", "-t", "ext4", "-d", stageDir, tmpImage).Run(); err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("build microvm ext4 rootfs: %w", err)
	}
	if err := os.Rename(tmpImage, outPath); err != nil {
		return MicroVMOCIRootFSResult{}, fmt.Errorf("commit microvm rootfs image: %w", err)
	}
	return MicroVMOCIRootFSResult{
		ImageRef:     imageRef,
		Manifest:     manifestDesc,
		Config:       imageConfig,
		RootFSPath:   outPath,
		InitPath:     firecrackerInitPath,
		Platform:     platform,
		LayerDigests: layerDigests,
	}, nil
}

func splitRegistryReference(raw string) (repoRef, reference string, err error) {
	ref, err := registry.ParseReference(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse OCI image ref %q: %w", raw, err)
	}
	reference = ref.Reference
	if reference == "" {
		reference = "latest"
	}
	return ref.Registry + "/" + ref.Repository, reference, nil
}

func newMicroVMOCIRepository(repoRef string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(repoRef)
	if err != nil {
		return nil, err
	}
	host := strings.SplitN(repoRef, "/", 2)[0]
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.DefaultCache,
		Credential: auth.StaticCredential(host, auth.Credential{}),
	}
	return repo, nil
}

func fetchOCIBytes(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) ([]byte, error) {
	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func extractOCILayer(stageDir, mediaType string, rc io.Reader) error {
	var reader io.Reader = rc
	if strings.Contains(mediaType, "gzip") || strings.HasSuffix(mediaType, ".gzip") || strings.HasSuffix(mediaType, "+gzip") {
		gz, err := gzip.NewReader(rc)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	}
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := applyOCITarEntry(stageDir, header, tr); err != nil {
			return err
		}
	}
}

func applyOCITarEntry(stageDir string, header *tar.Header, reader io.Reader) error {
	name := filepath.Clean(header.Name)
	if name == "." {
		return nil
	}
	if name == string(os.PathSeparator) || strings.HasPrefix(name, ".."+string(os.PathSeparator)) || filepath.IsAbs(name) {
		return fmt.Errorf("unsafe OCI layer path %q", header.Name)
	}
	base := filepath.Base(name)
	dir := filepath.Dir(name)
	if base == ".wh..wh..opq" {
		targetDir, err := safeOCIGuestPath(stageDir, dir)
		if err != nil {
			return err
		}
		return removeDirectoryChildren(targetDir)
	}
	if strings.HasPrefix(base, ".wh.") {
		target, err := safeOCIGuestPath(stageDir, filepath.Join(dir, strings.TrimPrefix(base, ".wh.")))
		if err != nil {
			return err
		}
		return os.RemoveAll(target)
	}
	target, err := safeOCIGuestPath(stageDir, name)
	if err != nil {
		return err
	}
	mode := os.FileMode(header.Mode).Perm()
	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(target, mode); err != nil {
			return err
		}
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, reader); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	case tar.TypeSymlink:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		_ = os.RemoveAll(target)
		return os.Symlink(header.Linkname, target)
	case tar.TypeLink:
		linkTarget, err := safeOCIGuestPath(stageDir, filepath.Clean(header.Linkname))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		_ = os.RemoveAll(target)
		if err := os.Link(linkTarget, target); err != nil {
			return err
		}
	default:
		return nil
	}
	return os.Chmod(target, mode)
}

func safeOCIGuestPath(stageDir, guestPath string) (string, error) {
	rel := filepath.Clean(strings.TrimPrefix(guestPath, string(os.PathSeparator)))
	if rel == "." || rel == "" {
		return "", fmt.Errorf("OCI layer path must be below root")
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe OCI layer path %q", guestPath)
	}
	return filepath.Join(stageDir, rel), nil
}

func removeDirectoryChildren(path string) error {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (b *MicroVMOCIRootFSBuilder) stateDir() string {
	if strings.TrimSpace(b.StateDir) != "" {
		return b.StateDir
	}
	return filepath.Join(os.TempDir(), "agency-microvm-ocifs")
}

func (b *MicroVMOCIRootFSBuilder) mke2fsPath() string {
	if strings.TrimSpace(b.Mke2fsPath) != "" {
		return b.Mke2fsPath
	}
	return "mke2fs"
}

func (b *MicroVMOCIRootFSBuilder) sizeMiB() int64 {
	if b.SizeMiB > 0 {
		return b.SizeMiB
	}
	return defaultFirecrackerRootFSMiB
}

func (b *MicroVMOCIRootFSBuilder) platform() ocispec.Platform {
	if b.Platform.OS != "" && b.Platform.Architecture != "" {
		return b.Platform
	}
	return ocispec.Platform{OS: "linux", Architecture: "arm64"}
}
