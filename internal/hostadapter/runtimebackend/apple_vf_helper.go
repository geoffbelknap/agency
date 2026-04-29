package runtimebackend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type AppleVFHelperHealth struct {
	OK                      bool   `json:"ok"`
	Backend                 string `json:"backend"`
	Command                 string `json:"command"`
	Version                 string `json:"version"`
	Darwin                  string `json:"darwin"`
	Arch                    string `json:"arch"`
	VirtualizationAvailable bool   `json:"virtualizationAvailable"`
	Error                   string `json:"error,omitempty"`
}

var appleVFHelperCommandContext = exec.CommandContext

func AppleVFHelperHealthStatus(ctx context.Context, helperBinary string) (AppleVFHelperHealth, error) {
	helperBinary = strings.TrimSpace(helperBinary)
	if helperBinary == "" {
		return AppleVFHelperHealth{}, fmt.Errorf("apple-vf helper binary path is not configured")
	}
	cmd := appleVFHelperCommandContext(ctx, helperBinary, "health")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	health, parseErr := ParseAppleVFHelperHealth(out)
	if parseErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return AppleVFHelperHealth{}, fmt.Errorf("apple-vf helper health output is not parseable: %w: %s", parseErr, detail)
		}
		return AppleVFHelperHealth{}, fmt.Errorf("apple-vf helper health output is not parseable: %w", parseErr)
	}
	if err != nil {
		if strings.TrimSpace(health.Error) != "" {
			return health, errors.New(health.Error)
		}
		return health, fmt.Errorf("apple-vf helper health failed: %w", err)
	}
	if !health.OK {
		if strings.TrimSpace(health.Error) != "" {
			return health, errors.New(health.Error)
		}
		return health, fmt.Errorf("apple-vf helper health returned ok=false")
	}
	if health.Backend != BackendAppleVFMicroVM {
		return health, fmt.Errorf("apple-vf helper reported backend %q, want %q", health.Backend, BackendAppleVFMicroVM)
	}
	return health, nil
}

func ParseAppleVFHelperHealth(data []byte) (AppleVFHelperHealth, error) {
	var health AppleVFHelperHealth
	if err := json.Unmarshal(bytes.TrimSpace(data), &health); err != nil {
		return AppleVFHelperHealth{}, err
	}
	return health, nil
}
