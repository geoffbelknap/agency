package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigServer serves agent config files over HTTP from the enforcer's
// mounted agent directory. The body runtime fetches these instead of
// reading bind-mounted files directly.
//
// Allowed files are whitelisted to prevent path traversal. Files are
// read fresh on each request — when the gateway SIGHUPs the enforcer
// after updating a file, the next request returns new content.

var configWhitelist = map[string]bool{
	"PLATFORM.md":            true,
	"mission.yaml":           true,
	"services-manifest.json": true,
	"FRAMEWORK.md":           true,
	"AGENTS.md":              true,
	"identity.md":            true,
	"constraints.yaml":       true,
	"skills-manifest.json":   true,
	"session-context.json":   true,
	"tiers.json":             true,
}

type ConfigServer struct {
	agentDir string
}

func NewConfigServer(agentDir string) *ConfigServer {
	return &ConfigServer{agentDir: agentDir}
}

func (cs *ConfigServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filename := strings.TrimPrefix(r.URL.Path, "/config/")
	if filename == "" || strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if !configWhitelist[filename] {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	path := filepath.Join(cs.agentDir, filename)
	info, err := os.Stat(path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// If-Modified-Since support
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if t, err := http.ParseTime(ims); err == nil {
			if !info.ModTime().Truncate(time.Second).After(t.Truncate(time.Second)) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Content-Type by extension
	ext := filepath.Ext(filename)
	switch ext {
	case ".md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	case ".yaml":
		w.Header().Set("Content-Type", "application/x-yaml")
	case ".json":
		w.Header().Set("Content-Type", "application/json")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	w.Write(data)
}
