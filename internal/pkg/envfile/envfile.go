package envfile

import (
	"os"
	"strings"
)

// Load reads KEY=VALUE pairs from a file. Lines starting with '#' and
// blank lines are skipped. Returns an empty map if the file cannot be read.
func Load(path string) map[string]string {
	env := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return env
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx > 0 {
			env[line[:idx]] = line[idx+1:]
		}
	}
	return env
}

// Upsert reads path (if it exists), replaces any lines whose KEY matches
// a key in entries, and appends new lines for every key in entries. Writes 0600.
// Backs up the file before writing. This is the ONLY function that should write
// to env files — all callers must go through here.
func Upsert(path string, entries map[string]string) error {
	// Back up before writing — service keys are critical credentials
	if data, err := os.ReadFile(path); err == nil && len(data) > 1 {
		os.WriteFile(path+".bak", data, 0600)
	} else if _, berr := os.Stat(path + ".bak"); berr == nil {
		// File missing or empty but backup exists — restore
		if bak, berr := os.ReadFile(path + ".bak"); berr == nil && len(bak) > 1 {
			os.WriteFile(path, bak, 0600)
		}
	}

	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				lines = append(lines, trimmed)
				continue
			}
			// Parse KEY=value to check if this line is being replaced.
			eqIdx := strings.IndexByte(trimmed, '=')
			if eqIdx < 0 {
				lines = append(lines, trimmed)
				continue
			}
			lineKey := trimmed[:eqIdx]
			if _, replacing := entries[lineKey]; replacing {
				// Drop the old line; the new value will be appended below.
				continue
			}
			lines = append(lines, trimmed)
		}
	}

	for k, v := range entries {
		lines = append(lines, k+"="+v)
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0600)
}
