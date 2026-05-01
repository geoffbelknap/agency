package orchestrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (inf *Infra) hostInfraSourceDir(required ...string) (string, error) {
	var starts []string
	if sourceDir := strings.TrimSpace(inf.SourceDir); sourceDir != "" {
		starts = append(starts, sourceDir)
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, start := range starts {
		if dir, ok := findHostInfraSourceDir(start, required...); ok {
			return dir, nil
		}
	}
	return "", fmt.Errorf("host infra source unavailable; missing %s", strings.Join(required, ", "))
}

func findHostInfraSourceDir(start string, required ...string) (string, bool) {
	dir := filepath.Clean(start)
	for i := 0; i < 8; i++ {
		if hostInfraSourceHas(dir, required...) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

func hostInfraSourceHas(dir string, required ...string) bool {
	for _, rel := range required {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			return false
		}
	}
	return true
}

func hostInfraVenvBin(sourceDir, name string) string {
	for _, dir := range hostInfraVenvDirs(sourceDir) {
		path := filepath.Join(dir, "bin", name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func hostInfraVenvDirs(sourceDir string) []string {
	sourceDir = filepath.Clean(sourceDir)
	dirs := []string{filepath.Join(sourceDir, ".venv")}

	// Homebrew formulae should keep application virtualenvs under libexec.
	// Packaged assets live under <prefix>/share/agency[-rc], so derive the
	// sibling <prefix>/libexec/venv path from the source directory.
	parent := filepath.Dir(sourceDir)
	if filepath.Base(parent) == "share" {
		prefix := filepath.Dir(parent)
		dirs = append(dirs, filepath.Join(prefix, "libexec", "venv"))
	}

	return dirs
}
