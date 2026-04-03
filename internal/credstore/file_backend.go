package credstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// fileEntry is the on-disk representation of a single credential.
type fileEntry struct {
	Name     string            `json:"name"`
	Value    string            `json:"value"`    // base64(nonce + ciphertext)
	Metadata map[string]string `json:"metadata"` // plaintext
}

// FileBackend implements SecretBackend with AES-256-GCM encryption,
// atomic writes, and flock-based file locking.
type FileBackend struct {
	storePath string // path to the encrypted store file
	keyPath   string // path to the 32-byte encryption key
	lockPath  string // path to the advisory lock file
}

// NewFileBackend creates a FileBackend. If keyPath does not exist, a new
// 32-byte random key is generated and written with mode 0400.
func NewFileBackend(storePath, keyPath string) (*FileBackend, error) {
	// Create parent directories.
	if err := os.MkdirAll(filepath.Dir(storePath), 0700); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, fmt.Errorf("create key dir: %w", err)
	}

	// Generate key if it doesn't exist.
	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		if err := os.WriteFile(keyPath, key, 0400); err != nil {
			return nil, fmt.Errorf("write key: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("stat key: %w", err)
	}

	return &FileBackend{
		storePath: storePath,
		keyPath:   keyPath,
		lockPath:  storePath + ".lock",
	}, nil
}

// Put stores or updates a credential. If name exists, the value and metadata
// are updated (metadata is merged). If not, a new entry is appended.
func (fb *FileBackend) Put(name, value string, metadata map[string]string) error {
	gcm, err := fb.newGCM()
	if err != nil {
		return err
	}

	return fb.withWriteLock(func(entries []fileEntry) ([]fileEntry, error) {
		encrypted, err := encrypt(gcm, []byte(value))
		if err != nil {
			return nil, err
		}

		for i, e := range entries {
			if e.Name == name {
				entries[i].Value = encrypted
				for k, v := range metadata {
					entries[i].Metadata[k] = v
				}
				return entries, nil
			}
		}

		return append(entries, fileEntry{
			Name:     name,
			Value:    encrypted,
			Metadata: metadata,
		}), nil
	})
}

// Get retrieves a credential by name, decrypting the value.
func (fb *FileBackend) Get(name string) (string, map[string]string, error) {
	gcm, err := fb.newGCM()
	if err != nil {
		return "", nil, err
	}

	entries, err := fb.readWithSharedLock()
	if err != nil {
		return "", nil, err
	}

	for _, e := range entries {
		if e.Name == name {
			plaintext, err := decrypt(gcm, e.Value)
			if err != nil {
				return "", nil, fmt.Errorf("decrypt %q: %w", name, err)
			}
			// Return a copy of metadata.
			meta := make(map[string]string, len(e.Metadata))
			for k, v := range e.Metadata {
				meta[k] = v
			}
			return string(plaintext), meta, nil
		}
	}

	return "", nil, fmt.Errorf("credential %q not found", name)
}

// Delete removes a credential by name.
func (fb *FileBackend) Delete(name string) error {
	return fb.withWriteLock(func(entries []fileEntry) ([]fileEntry, error) {
		for i, e := range entries {
			if e.Name == name {
				return append(entries[:i], entries[i+1:]...), nil
			}
		}
		return nil, fmt.Errorf("credential %q not found", name)
	})
}

// List returns all credential references without values.
func (fb *FileBackend) List() ([]SecretRef, error) {
	entries, err := fb.readWithSharedLock()
	if err != nil {
		return nil, err
	}

	refs := make([]SecretRef, len(entries))
	for i, e := range entries {
		meta := make(map[string]string, len(e.Metadata))
		for k, v := range e.Metadata {
			meta[k] = v
		}
		refs[i] = SecretRef{Name: e.Name, Metadata: meta}
	}
	return refs, nil
}

// newGCM creates an AES-256-GCM cipher from the key file.
func (fb *FileBackend) newGCM() (cipher.AEAD, error) {
	key, err := os.ReadFile(fb.keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key size: expected 32, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return gcm, nil
}

// encrypt produces base64(nonce + ciphertext) using a random 12-byte nonce.
func encrypt(gcm cipher.AEAD, plaintext []byte) (string, error) {
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decrypt reverses base64 decoding and AES-256-GCM decryption.
func decrypt(gcm cipher.AEAD, encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
}

// readWithSharedLock reads the store with a shared file lock.
func (fb *FileBackend) readWithSharedLock() ([]fileEntry, error) {
	lf, err := os.OpenFile(fb.lockPath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	defer lf.Close()

	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_SH); err != nil {
		return nil, fmt.Errorf("shared lock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	f, err := os.OpenFile(fb.storePath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	defer f.Close()

	return fb.readEntries(f)
}

// withWriteLock reads, mutates, and atomically writes the store under an
// exclusive file lock. The lock is held on a separate .lock file so that
// atomic renames of the store file do not invalidate the lock.
func (fb *FileBackend) withWriteLock(fn func([]fileEntry) ([]fileEntry, error)) (err error) {
	lf, err := os.OpenFile(fb.lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer func() {
		if cerr := lf.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("exclusive lock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	f, err := os.OpenFile(fb.storePath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	entries, err := fb.readEntries(f)
	f.Close()
	if err != nil {
		return err
	}

	entries, err = fn(entries)
	if err != nil {
		return err
	}

	return fb.atomicWrite(entries)
}

// readEntries decodes the JSON store file. An empty file yields an empty slice.
func (fb *FileBackend) readEntries(f *os.File) ([]fileEntry, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat store: %w", err)
	}
	if info.Size() == 0 {
		return nil, nil
	}

	var entries []fileEntry
	if err := json.NewDecoder(f).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode store: %w", err)
	}
	return entries, nil
}

// atomicWrite writes entries to a temp file, syncs, then renames over the store.
func (fb *FileBackend) atomicWrite(entries []fileEntry) error {
	tmpPath := fb.storePath + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		if cerr := tmp.Close(); cerr != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("encode store: %w (close: %v)", err, cerr)
		}
		os.Remove(tmpPath)
		return fmt.Errorf("encode store: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync tmp: %w", err)
	}

	// Close before rename to ensure all data is flushed to the filesystem.
	// Sync() has already been called, so data loss on close is not a concern.
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close tmp: %w", err)
	}

	if err := os.Rename(tmpPath, fb.storePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename store: %w", err)
	}
	return nil
}
