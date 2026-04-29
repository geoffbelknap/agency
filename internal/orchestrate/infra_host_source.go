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
