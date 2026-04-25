package deployments

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	deploymentFileName = "deployment.yaml"
	configFileName     = "config.yaml"
	credRefsFileName   = "credrefs.yaml"
	bindingsFileName   = "bindings.yaml"
	schemaFileName     = "schema.yaml"
	auditDirName       = "audit"
	lockFileName       = ".lock"
)

type Store interface {
	Create(ctx context.Context, dep *Deployment, schema *Schema) error
	Get(ctx context.Context, id string) (*Deployment, *Schema, error)
	List(ctx context.Context) ([]*Deployment, error)
	Update(ctx context.Context, id string, mutator func(*Deployment, *Schema) error) error
	Delete(ctx context.Context, id string) error
	Claim(ctx context.Context, id string, owner OwnerRef, force bool) error
	Release(ctx context.Context, id string) error
	Export(ctx context.Context, id string) (io.ReadCloser, error)
	Import(ctx context.Context, bundle io.Reader) (*Deployment, *Schema, error)
	AppendAudit(ctx context.Context, id string, entry AuditEntry) error
}

type FilesystemStore struct {
	root string
	now  func() time.Time
}

func NewFilesystemStore(root string) *FilesystemStore {
	return &FilesystemStore{root: root, now: func() time.Time { return time.Now().UTC() }}
}

func (s *FilesystemStore) Create(_ context.Context, dep *Deployment, schema *Schema) error {
	if dep == nil || schema == nil {
		return fmt.Errorf("deployment and schema are required")
	}
	if dep.ID == "" {
		dep.ID = newID()
	}
	if dep.CreatedAt.IsZero() {
		dep.CreatedAt = s.now()
	}
	dep.UpdatedAt = dep.CreatedAt
	if dep.AuditLogPath == "" {
		dep.AuditLogPath = filepath.Join(auditDirName, dep.CreatedAt.Format("2006-01-02T15-04-05Z")+".jsonl")
	}
	if err := os.MkdirAll(s.dir(dep.ID), 0o755); err != nil {
		return err
	}
	unlock, err := s.lock(dep.ID)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.ensureNameAvailable(dep.Name, dep.ID); err != nil {
		return err
	}
	return s.writeAll(dep, schema)
}

func (s *FilesystemStore) Get(_ context.Context, id string) (*Deployment, *Schema, error) {
	return s.readAll(id)
}

func (s *FilesystemStore) List(_ context.Context) ([]*Deployment, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return []*Deployment{}, nil
	}
	if err != nil {
		return nil, err
	}
	var deployments []*Deployment
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dep, _, err := s.readAll(entry.Name())
		if err == nil {
			deployments = append(deployments, dep)
		}
	}
	sort.Slice(deployments, func(i, j int) bool { return deployments[i].Name < deployments[j].Name })
	return deployments, nil
}

func (s *FilesystemStore) Update(_ context.Context, id string, mutator func(*Deployment, *Schema) error) error {
	unlock, err := s.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	dep, schema, err := s.readAll(id)
	if err != nil {
		return err
	}
	if err := mutator(dep, schema); err != nil {
		return err
	}
	dep.UpdatedAt = s.now()
	if err := s.ensureNameAvailable(dep.Name, dep.ID); err != nil {
		return err
	}
	return s.writeAll(dep, schema)
}

func (s *FilesystemStore) Delete(_ context.Context, id string) error {
	unlock, err := s.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	return os.RemoveAll(s.dir(id))
}

func (s *FilesystemStore) Claim(ctx context.Context, id string, owner OwnerRef, force bool) error {
	return s.Update(ctx, id, func(dep *Deployment, _ *Schema) error {
		if dep.Owner.AgencyID != "" && dep.Owner.AgencyID != owner.AgencyID && !force {
			if dep.Owner.Heartbeat.After(s.now().Add(-5 * time.Minute)) {
				return fmt.Errorf("deployment currently owned by %s", dep.Owner.AgencyName)
			}
		}
		owner.ClaimedAt = s.now()
		owner.Heartbeat = owner.ClaimedAt
		dep.Owner = owner
		return nil
	})
}

func (s *FilesystemStore) Release(ctx context.Context, id string) error {
	return s.Update(ctx, id, func(dep *Deployment, _ *Schema) error {
		dep.Owner = OwnerRef{}
		return nil
	})
}

func (s *FilesystemStore) AppendAudit(_ context.Context, id string, entry AuditEntry) error {
	dep, _, err := s.readAll(id)
	if err != nil {
		return err
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = s.now()
	}
	path := filepath.Join(s.dir(id), dep.AuditLogPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

func (s *FilesystemStore) Export(_ context.Context, id string) (io.ReadCloser, error) {
	dep, schema, err := s.readAll(id)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		gw := gzip.NewWriter(pw)
		defer gw.Close()
		tw := tar.NewWriter(gw)
		defer tw.Close()
		files := map[string]interface{}{
			deploymentFileName: dep,
			configFileName:     dep.Config,
			credRefsFileName:   dep.CredRefs,
			bindingsFileName:   dep.Instances,
			schemaFileName:     schema,
		}
		for name, value := range files {
			data, err := yaml.Marshal(value)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			if err := writeTarFile(tw, name, data); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
		for _, auditPath := range s.auditFiles(dep.ID) {
			data, err := os.ReadFile(auditPath)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			rel := filepath.Join(auditDirName, filepath.Base(auditPath))
			if err := writeTarFile(tw, rel, data); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
	}()
	return pr, nil
}

func (s *FilesystemStore) Import(_ context.Context, bundle io.Reader) (*Deployment, *Schema, error) {
	gr, err := gzip.NewReader(bundle)
	if err != nil {
		return nil, nil, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	files := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, err
		}
		files[hdr.Name] = data
	}
	var dep Deployment
	if err := yaml.Unmarshal(files[deploymentFileName], &dep); err != nil {
		return nil, nil, err
	}
	var schema Schema
	if err := yaml.Unmarshal(files[schemaFileName], &schema); err != nil {
		return nil, nil, err
	}
	dep.ID = newID()
	dep.Instances = nil
	dep.CreatedAt = s.now()
	dep.UpdatedAt = dep.CreatedAt
	dep.Owner = OwnerRef{}
	if err := s.Create(context.Background(), &dep, &schema); err != nil {
		return nil, nil, err
	}
	for name, data := range files {
		if !strings.HasPrefix(name, auditDirName+"/") {
			continue
		}
		cleanName := filepath.Clean(filepath.FromSlash(name))
		if filepath.IsAbs(cleanName) || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
			return nil, nil, fmt.Errorf("invalid audit path %q", name)
		}
		path := filepath.Join(s.dir(dep.ID), cleanName)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, nil, err
		}
	}
	return &dep, &schema, nil
}

func (s *FilesystemStore) readAll(id string) (*Deployment, *Schema, error) {
	dir := s.dir(id)
	var dep Deployment
	if err := readYAML(filepath.Join(dir, deploymentFileName), &dep); err != nil {
		return nil, nil, err
	}
	var schema Schema
	if err := readYAML(filepath.Join(dir, schemaFileName), &schema); err != nil {
		return nil, nil, err
	}
	if dep.Config == nil {
		if err := readYAML(filepath.Join(dir, configFileName), &dep.Config); err != nil {
			return nil, nil, err
		}
	}
	if dep.CredRefs == nil {
		if err := readYAML(filepath.Join(dir, credRefsFileName), &dep.CredRefs); err != nil {
			return nil, nil, err
		}
	}
	if dep.Instances == nil {
		if err := readYAML(filepath.Join(dir, bindingsFileName), &dep.Instances); err != nil {
			return nil, nil, err
		}
	}
	return &dep, &schema, nil
}

func (s *FilesystemStore) writeAll(dep *Deployment, schema *Schema) error {
	if err := os.MkdirAll(filepath.Join(s.dir(dep.ID), auditDirName), 0o755); err != nil {
		return err
	}
	if err := writeYAML(filepath.Join(s.dir(dep.ID), deploymentFileName), dep); err != nil {
		return err
	}
	if err := writeYAML(filepath.Join(s.dir(dep.ID), configFileName), dep.Config); err != nil {
		return err
	}
	if err := writeYAML(filepath.Join(s.dir(dep.ID), credRefsFileName), dep.CredRefs); err != nil {
		return err
	}
	if err := writeYAML(filepath.Join(s.dir(dep.ID), bindingsFileName), dep.Instances); err != nil {
		return err
	}
	return writeYAML(filepath.Join(s.dir(dep.ID), schemaFileName), schema)
}

func (s *FilesystemStore) ensureNameAvailable(name, currentID string) error {
	deployments, err := s.List(context.Background())
	if err != nil {
		return err
	}
	for _, dep := range deployments {
		if dep.Name == name && dep.ID != currentID {
			return fmt.Errorf("deployment name %q already exists", name)
		}
	}
	return nil
}

func (s *FilesystemStore) dir(id string) string {
	return filepath.Join(s.root, id)
}

func (s *FilesystemStore) auditFiles(id string) []string {
	pattern := filepath.Join(s.dir(id), auditDirName, "*.jsonl")
	matches, _ := filepath.Glob(pattern)
	sort.Strings(matches)
	return matches
}

func (s *FilesystemStore) lock(id string) (func(), error) {
	if err := os.MkdirAll(s.dir(id), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.dir(id), lockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func writeYAML(path string, value interface{}) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readYAML(path string, value interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, value)
}

func writeTarFile(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return hex.EncodeToString(buf)
}
