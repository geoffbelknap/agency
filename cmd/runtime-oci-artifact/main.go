package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	bodyBaseRef    = "docker.io/library/python:3.13-slim@sha256:d168b8d9eb761f4d3fe305ebd04aeb7e7f2de0297cec5fb2f8f6403244621664"
	ociImageConfig = "application/vnd.oci.image.config.v1+json"
	ociManifest    = "application/vnd.oci.image.manifest.v1+json"
	ociIndex       = "application/vnd.oci.image.index.v1+json"
	ociLayerGzip   = "application/vnd.oci.image.layer.v1.tar+gzip"
)

type options struct {
	artifact      string
	repoRef       string
	version       string
	sourceRoot    string
	buildID       string
	depsAMD64     string
	depsARM64     string
	enforcerAMD64 string
	enforcerARM64 string
	caBundle      string
	inspectRef    string
}

func main() {
	var opt options
	flag.StringVar(&opt.artifact, "artifact", "", "artifact to publish: body or enforcer")
	flag.StringVar(&opt.repoRef, "repo", "", "target OCI repository, for example ghcr.io/geoffbelknap/agency-runtime-body")
	flag.StringVar(&opt.version, "version", "", "version without leading v")
	flag.StringVar(&opt.sourceRoot, "source-root", ".", "Agency source root")
	flag.StringVar(&opt.buildID, "build-id", "", "build identifier label")
	flag.StringVar(&opt.depsAMD64, "body-deps-amd64", "", "body Python dependency root for linux/amd64")
	flag.StringVar(&opt.depsARM64, "body-deps-arm64", "", "body Python dependency root for linux/arm64")
	flag.StringVar(&opt.enforcerAMD64, "enforcer-amd64", "", "linux/amd64 enforcer binary")
	flag.StringVar(&opt.enforcerARM64, "enforcer-arm64", "", "linux/arm64 enforcer binary")
	flag.StringVar(&opt.caBundle, "ca-bundle", "/etc/ssl/certs/ca-certificates.crt", "CA bundle to include in enforcer artifact")
	flag.StringVar(&opt.inspectRef, "inspect-ref", "", "inspect a published runtime artifact ref and verify linux/amd64 plus linux/arm64")
	flag.Parse()

	ctx := context.Background()
	var err error
	if opt.inspectRef != "" {
		err = inspectRuntimeArtifact(ctx, opt.inspectRef)
	} else {
		err = run(ctx, opt)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func inspectRuntimeArtifact(ctx context.Context, ref string) error {
	repoRef, reference, err := splitRegistryReference(ref)
	if err != nil {
		return err
	}
	repo, err := newRepository(repoRef)
	if err != nil {
		return err
	}
	desc, err := repo.Resolve(ctx, reference)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", ref, err)
	}
	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", ref, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	platforms := map[string]bool{}
	if desc.MediaType == ociIndex || strings.Contains(desc.MediaType, "manifest.list") || strings.Contains(desc.MediaType, "image.index") {
		var index ocispec.Index
		if err := json.Unmarshal(data, &index); err != nil {
			return err
		}
		for _, manifest := range index.Manifests {
			if manifest.Platform != nil && manifest.Platform.OS == "linux" {
				platforms[manifest.Platform.Architecture] = true
			}
		}
	} else {
		var manifest ocispec.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
		configBytes, err := fetchBytes(ctx, repo, manifest.Config)
		if err != nil {
			return err
		}
		var config ocispec.Image
		if err := json.Unmarshal(configBytes, &config); err != nil {
			return err
		}
		if config.OS == "linux" {
			platforms[config.Architecture] = true
		}
	}
	var missing []string
	for _, arch := range []string{"amd64", "arm64"} {
		if !platforms[arch] {
			missing = append(missing, "linux/"+arch)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s missing platforms: %s", ref, strings.Join(missing, ", "))
	}
	fmt.Printf("%s platforms=linux/amd64,linux/arm64 digest=%s\n", ref, desc.Digest)
	return nil
}

func run(ctx context.Context, opt options) error {
	if strings.TrimSpace(opt.repoRef) == "" {
		return fmt.Errorf("--repo is required")
	}
	if strings.TrimSpace(opt.version) == "" || strings.Contains(opt.version, ":") || opt.version == "latest" {
		return fmt.Errorf("--version must be a concrete version without leading v, not %q", opt.version)
	}
	target, err := newRepository(opt.repoRef)
	if err != nil {
		return err
	}

	var manifests []ocispec.Descriptor
	for _, platform := range []ocispec.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	} {
		desc, err := publishPlatform(ctx, target, opt, platform)
		if err != nil {
			return err
		}
		manifests = append(manifests, desc)
		fmt.Printf("%s/%s=%s\n", platform.OS, platform.Architecture, desc.Digest)
	}

	index := ocispec.Index{
		Versioned:   specsVersioned(),
		MediaType:   ociIndex,
		Manifests:   manifests,
		Annotations: annotations(opt),
	}
	indexBytes, indexDesc, err := marshalDescriptor(index, ociIndex)
	if err != nil {
		return err
	}
	tag := "v" + opt.version
	if err := target.PushReference(ctx, indexDesc, bytes.NewReader(indexBytes), tag); err != nil {
		return fmt.Errorf("push %s:%s: %w", opt.repoRef, tag, err)
	}
	fmt.Printf("%s:%s@%s\n", opt.repoRef, tag, indexDesc.Digest)
	return nil
}

func publishPlatform(ctx context.Context, target *remote.Repository, opt options, platform ocispec.Platform) (ocispec.Descriptor, error) {
	var config ocispec.Image
	var layers []ocispec.Descriptor
	switch opt.artifact {
	case "body":
		baseConfig, baseLayers, err := copyBaseImage(ctx, bodyBaseRef, target, platform)
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		config = baseConfig
		layers = append(layers, baseLayers...)
		layer, err := bodyLayer(opt, platform)
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		if err := pushBytes(ctx, target, layer.desc, layer.bytes); err != nil {
			return ocispec.Descriptor{}, err
		}
		layers = append(layers, layer.desc)
		config.Config.WorkingDir = "/app"
		config.Config.User = "61000:61000"
		config.Config.Cmd = []string{"/app/entrypoint.sh"}
	case "enforcer":
		config = ocispec.Image{
			Platform: platform,
			Config: ocispec.ImageConfig{
				Cmd: []string{"/usr/local/bin/enforcer"},
				ExposedPorts: map[string]struct{}{
					"3128/tcp": {},
					"8081/tcp": {},
				},
			},
		}
		layer, err := enforcerLayer(opt, platform)
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		if err := pushBytes(ctx, target, layer.desc, layer.bytes); err != nil {
			return ocispec.Descriptor{}, err
		}
		layers = append(layers, layer.desc)
	default:
		return ocispec.Descriptor{}, fmt.Errorf("--artifact must be body or enforcer")
	}

	config.OS = platform.OS
	config.Architecture = platform.Architecture
	config.Created = ptrTime(time.Now().UTC())
	config.Config.Labels = mergeLabels(config.Config.Labels, annotations(opt))
	configBytes, configDesc, err := marshalDescriptor(config, ociImageConfig)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if err := pushBytes(ctx, target, configDesc, configBytes); err != nil {
		return ocispec.Descriptor{}, err
	}
	manifest := ocispec.Manifest{
		Versioned: specsVersioned(),
		MediaType: ociManifest,
		Config:    configDesc,
		Layers:    layers,
	}
	manifestBytes, manifestDesc, err := marshalDescriptor(manifest, ociManifest)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if err := pushBytes(ctx, target, manifestDesc, manifestBytes); err != nil {
		return ocispec.Descriptor{}, err
	}
	manifestDesc.Platform = &platform
	return manifestDesc, nil
}

func copyBaseImage(ctx context.Context, ref string, target *remote.Repository, platform ocispec.Platform) (ocispec.Image, []ocispec.Descriptor, error) {
	repoRef, reference, err := splitRegistryReference(ref)
	if err != nil {
		return ocispec.Image{}, nil, err
	}
	source, err := newRepository(repoRef)
	if err != nil {
		return ocispec.Image{}, nil, err
	}
	_, manifestBytes, err := oras.FetchBytes(ctx, source, reference, oras.FetchBytesOptions{
		FetchOptions: oras.FetchOptions{ResolveOptions: oras.ResolveOptions{TargetPlatform: &platform}},
	})
	if err != nil {
		return ocispec.Image{}, nil, fmt.Errorf("fetch base image %s for %s/%s: %w", ref, platform.OS, platform.Architecture, err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return ocispec.Image{}, nil, err
	}
	configBytes, err := copyBlob(ctx, source, target, manifest.Config)
	if err != nil {
		return ocispec.Image{}, nil, err
	}
	var config ocispec.Image
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return ocispec.Image{}, nil, err
	}
	for _, layer := range manifest.Layers {
		if _, err := copyBlob(ctx, source, target, layer); err != nil {
			return ocispec.Image{}, nil, err
		}
	}
	return config, manifest.Layers, nil
}

func copyBlob(ctx context.Context, source, target *remote.Repository, desc ocispec.Descriptor) ([]byte, error) {
	data, err := fetchBytes(ctx, source, desc)
	if err != nil {
		return nil, err
	}
	return data, pushBytes(ctx, target, desc, data)
}

func fetchBytes(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) ([]byte, error) {
	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

type layerBlob struct {
	desc  ocispec.Descriptor
	bytes []byte
}

func bodyLayer(opt options, platform ocispec.Platform) (layerBlob, error) {
	deps := opt.depsAMD64
	if platform.Architecture == "arm64" {
		deps = opt.depsARM64
	}
	entries := []tarEntry{
		dirEntry("app", 0o755, 0, 0),
		dirEntry("workspace", 0o755, 61000, 61000),
		fileEntry(filepath.Join(opt.sourceRoot, "images", "logging_config.py"), "app/logging_config.py", 0o644, 0, 0),
		fileEntry(filepath.Join(opt.sourceRoot, "images", "_sitecustomize.py"), "app/sitecustomize.py", 0o644, 0, 0),
	}
	bodyDir := filepath.Join(opt.sourceRoot, "images", "body")
	matches, err := filepath.Glob(filepath.Join(bodyDir, "*.py"))
	if err != nil {
		return layerBlob{}, err
	}
	for _, match := range matches {
		base := filepath.Base(match)
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
			continue
		}
		entries = append(entries, fileEntry(match, filepath.Join("app", base), 0o644, 0, 0))
	}
	entries = append(entries, fileEntry(filepath.Join(bodyDir, "entrypoint.sh"), "app/entrypoint.sh", 0o755, 0, 0))
	if strings.TrimSpace(deps) != "" {
		depEntries, err := dirEntries(deps, "", 0, 0)
		if err != nil {
			return layerBlob{}, err
		}
		entries = append(entries, depEntries...)
	}
	return makeLayer(entries)
}

func enforcerLayer(opt options, platform ocispec.Platform) (layerBlob, error) {
	bin := opt.enforcerAMD64
	if platform.Architecture == "arm64" {
		bin = opt.enforcerARM64
	}
	if strings.TrimSpace(bin) == "" {
		return layerBlob{}, fmt.Errorf("missing enforcer binary for %s/%s", platform.OS, platform.Architecture)
	}
	entries := []tarEntry{
		dirEntry("usr", 0o755, 0, 0),
		dirEntry("usr/local", 0o755, 0, 0),
		dirEntry("usr/local/bin", 0o755, 0, 0),
		fileEntry(bin, "usr/local/bin/enforcer", 0o755, 0, 0),
	}
	if strings.TrimSpace(opt.caBundle) != "" {
		entries = append(entries,
			dirEntry("etc", 0o755, 0, 0),
			dirEntry("etc/ssl", 0o755, 0, 0),
			dirEntry("etc/ssl/certs", 0o755, 0, 0),
			fileEntry(opt.caBundle, "etc/ssl/certs/ca-certificates.crt", 0o644, 0, 0),
		)
	}
	return makeLayer(entries)
}

type tarEntry struct {
	src  string
	name string
	mode int64
	uid  int
	gid  int
	dir  bool
}

func dirEntry(name string, mode int64, uid, gid int) tarEntry {
	return tarEntry{name: filepath.ToSlash(filepath.Clean(name)), mode: mode, uid: uid, gid: gid, dir: true}
}

func fileEntry(src, name string, mode int64, uid, gid int) tarEntry {
	return tarEntry{src: src, name: filepath.ToSlash(filepath.Clean(name)), mode: mode, uid: uid, gid: gid}
}

func dirEntries(root, prefix string, uid, gid int) ([]tarEntry, error) {
	var entries []tarEntry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		name := filepath.Join(prefix, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			entries = append(entries, dirEntry(name, int64(info.Mode().Perm()), uid, gid))
			return nil
		}
		if info.Mode().Type() != 0 {
			return nil
		}
		entries = append(entries, fileEntry(path, name, int64(info.Mode().Perm()), uid, gid))
		return nil
	})
	return entries, err
}

func makeLayer(entries []tarEntry) (layerBlob, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := writeTarEntry(tw, entry); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			return layerBlob{}, err
		}
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return layerBlob{}, err
	}
	if err := gz.Close(); err != nil {
		return layerBlob{}, err
	}
	data := buf.Bytes()
	return layerBlob{bytes: data, desc: ocispec.Descriptor{
		MediaType: ociLayerGzip,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}}, nil
}

func writeTarEntry(tw *tar.Writer, entry tarEntry) error {
	header := &tar.Header{
		Name:     entry.name,
		Mode:     entry.mode,
		Uid:      entry.uid,
		Gid:      entry.gid,
		ModTime:  time.Unix(0, 0).UTC(),
		Typeflag: tar.TypeReg,
	}
	if entry.dir {
		header.Typeflag = tar.TypeDir
		return tw.WriteHeader(header)
	}
	data, err := os.ReadFile(entry.src)
	if err != nil {
		return fmt.Errorf("read %s: %w", entry.src, err)
	}
	header.Size = int64(len(data))
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

func splitRegistryReference(raw string) (repoRef, reference string, err error) {
	ref, err := registry.ParseReference(raw)
	if err != nil {
		return "", "", err
	}
	reference = ref.Reference
	if reference == "" {
		reference = "latest"
	}
	return ref.Registry + "/" + ref.Repository, reference, nil
}

func newRepository(repoRef string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(repoRef)
	if err != nil {
		return nil, err
	}
	host := strings.SplitN(repoRef, "/", 2)[0]
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.DefaultCache,
		Credential: func(_ context.Context, registry string) (auth.Credential, error) {
			if registry == "ghcr.io" {
				user := firstNonEmpty(os.Getenv("GHCR_USERNAME"), os.Getenv("GITHUB_ACTOR"))
				token := firstNonEmpty(os.Getenv("GHCR_TOKEN"), os.Getenv("GITHUB_TOKEN"))
				if user != "" && token != "" {
					return auth.Credential{Username: user, Password: token}, nil
				}
			}
			if registry == host && registry != "ghcr.io" {
				return auth.Credential{}, nil
			}
			return auth.Credential{}, nil
		},
	}
	return repo, nil
}

func pushBytes(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor, data []byte) error {
	if err := repo.Push(ctx, desc, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("push %s: %w", desc.Digest, err)
	}
	return nil
}

func marshalDescriptor(v any, mediaType string) ([]byte, ocispec.Descriptor, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	return data, ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}, nil
}

func annotations(opt options) map[string]string {
	buildID := firstNonEmpty(opt.buildID, "unknown")
	source := "https://github.com/geoffbelknap/agency"
	return map[string]string{
		"org.opencontainers.image.source":      source,
		"org.opencontainers.image.revision":    buildID,
		"org.opencontainers.image.version":     opt.version,
		"org.opencontainers.image.title":       "Agency runtime " + opt.artifact,
		"org.opencontainers.image.description": "Agency microVM runtime OCI filesystem artifact for " + opt.artifact,
		"agency.build.id":                      buildID,
		"agency.source.hash":                   sourceHash(opt.sourceRoot, opt.artifact),
	}
}

func sourceHash(root, artifact string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, root)
	_, _ = io.WriteString(h, artifact)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func mergeLabels(base, overlay map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func specsVersioned() specs.Versioned {
	return specs.Versioned{SchemaVersion: 2}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
