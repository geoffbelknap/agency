package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"
)

// fileEntry holds the hash and size of a single memory file.
type fileEntry struct {
	Hash string
	Size int64
}

// MemoryAuditor watches the agent's memory directory for mutations and emits
// structured audit entries for each change (ASK Tenet 25).
type MemoryAuditor struct {
	dir      string
	agent    string
	audit    *AuditLogger
	baseline map[string]fileEntry
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
}

// NewMemoryAuditor creates a MemoryAuditor. Call Start() in a goroutine to begin.
func NewMemoryAuditor(dir, agent string, audit *AuditLogger) *MemoryAuditor {
	return &MemoryAuditor{
		dir:      dir,
		agent:    agent,
		audit:    audit,
		baseline: make(map[string]fileEntry),
		interval: 60 * time.Second,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// scan walks the memory directory and returns a map of relative path → fileEntry.
// Files that cannot be read (e.g. locked mid-write) are skipped with a warning.
func (m *MemoryAuditor) scan() map[string]fileEntry {
	result := make(map[string]fileEntry)
	err := filepath.WalkDir(m.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("memory_audit: walk error at %s: %v", path, err)
			return nil // keep walking
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(m.dir, path)
		if relErr != nil {
			return nil
		}
		hash, size, hashErr := hashFile(path)
		if hashErr != nil {
			log.Printf("memory_audit: skipping %s (read error): %v", rel, hashErr)
			return nil
		}
		result[rel] = fileEntry{Hash: "sha256:" + hash, Size: size}
		return nil
	})
	if err != nil {
		log.Printf("memory_audit: scan error: %v", err)
	}
	return result
}

// diff compares old and new snapshots and emits audit entries for each change.
func (m *MemoryAuditor) diff(old, current map[string]fileEntry) {
	// Detect created and modified files.
	for path, entry := range current {
		prev, existed := old[path]
		if !existed {
			m.audit.Log(AuditEntry{
				Type:      "memory_mutation",
				Agent:     m.agent,
				File:      path,
				Action:    "created",
				NewHash:   entry.Hash,
				SizeDelta: entry.Size,
			})
		} else if prev.Hash != entry.Hash {
			m.audit.Log(AuditEntry{
				Type:      "memory_mutation",
				Agent:     m.agent,
				File:      path,
				Action:    "modified",
				OldHash:   prev.Hash,
				NewHash:   entry.Hash,
				SizeDelta: entry.Size - prev.Size,
			})
		}
	}

	// Detect deleted files.
	for path, prev := range old {
		if _, ok := current[path]; !ok {
			m.audit.Log(AuditEntry{
				Type:      "memory_mutation",
				Agent:     m.agent,
				File:      path,
				Action:    "deleted",
				OldHash:   prev.Hash,
				SizeDelta: -prev.Size,
			})
		}
	}
}

// Start establishes a baseline and begins periodic scanning. Blocks until Stop
// is called; run as a goroutine.
func (m *MemoryAuditor) Start() {
	defer close(m.done)

	// Baseline snapshot.
	m.baseline = m.scan()
	log.Printf("memory_audit: baseline established for %s (%d files)", m.dir, len(m.baseline))

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			current := m.scan()
			m.diff(m.baseline, current)
			m.baseline = current
		case <-m.stop:
			// Final scan on shutdown.
			current := m.scan()
			m.diff(m.baseline, current)
			log.Printf("memory_audit: final scan complete for %s", m.dir)
			return
		}
	}
}

// Stop signals the auditor to perform a final scan and exit.
func (m *MemoryAuditor) Stop() {
	close(m.stop)
	<-m.done
}

// hashFile computes the SHA-256 hash of a file using streaming I/O.
func hashFile(path string) (hexHash string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
