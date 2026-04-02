package main

import (
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type Blocklist struct {
	mu       sync.RWMutex
	exact    map[string]bool
	suffixes []string
	prefixes []string
}

func NewBlocklist() *Blocklist {
	return &Blocklist{exact: make(map[string]bool)}
}

func (bl *Blocklist) AddDeny(pattern string) {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if strings.HasPrefix(pattern, "*.") {
		bl.suffixes = append(bl.suffixes, strings.TrimPrefix(pattern, "*"))
	} else if strings.HasSuffix(pattern, ".*") {
		bl.prefixes = append(bl.prefixes, strings.TrimSuffix(pattern, "*"))
	} else {
		bl.exact[pattern] = true
	}
}

func (bl *Blocklist) IsBlocked(host string) bool {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	host = strings.ToLower(host)
	if bl.exact[host] {
		return true
	}
	for _, suffix := range bl.suffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	for _, prefix := range bl.prefixes {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	return false
}

type blocklistFile struct {
	Deny          []string `yaml:"deny"`
	AllowOverride []string `yaml:"allow_override"`
}

func LoadBlocklistYAML(data []byte) (*Blocklist, error) {
	var bf blocklistFile
	if err := yaml.Unmarshal(data, &bf); err != nil {
		return nil, err
	}
	bl := NewBlocklist()
	for _, d := range bf.Deny {
		bl.AddDeny(d)
	}
	return bl, nil
}

func LoadBlocklistFile(path string) (*Blocklist, error) {
	data, err := readFileIfExists(path)
	if err != nil || data == nil {
		return NewBlocklist(), err
	}
	return LoadBlocklistYAML(data)
}

func MergeBlocklists(lists ...*Blocklist) *Blocklist {
	merged := NewBlocklist()
	for _, bl := range lists {
		bl.mu.RLock()
		for k := range bl.exact {
			merged.exact[k] = true
		}
		merged.suffixes = append(merged.suffixes, bl.suffixes...)
		merged.prefixes = append(merged.prefixes, bl.prefixes...)
		bl.mu.RUnlock()
	}
	return merged
}
