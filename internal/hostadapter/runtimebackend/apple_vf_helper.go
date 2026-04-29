package runtimebackend

import (
	"bytes"
	"context"
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
	RequestID               string `json:"requestID,omitempty"`
	RuntimeID               string `json:"runtimeID,omitempty"`
	Role                    string `json:"role,omitempty"`
	AgencyHomeHash          string `json:"agencyHomeHash,omitempty"`
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

func AppleVFHelperPrepare(ctx context.Context, helperBinary string, req AppleVFHelperRequest) (AppleVFHelperResponse, error) {
	return appleVFHelperRunJSON(ctx, helperBinary, AppleVFCommandPrepare, req)
}

func appleVFHelperRunJSON(ctx context.Context, helperBinary string, command AppleVFHelperCommand, req AppleVFHelperRequest) (AppleVFHelperResponse, error) {
	helperBinary = strings.TrimSpace(helperBinary)
	if helperBinary == "" {
		return AppleVFHelperResponse{}, fmt.Errorf("apple-vf helper binary path is not configured")
	}
	arg, err := req.JSONArg()
	if err != nil {
		return AppleVFHelperResponse{}, err
	}
	cmd := appleVFHelperCommandContext(ctx, helperBinary, string(command), "--request-json", arg)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	resp, parseErr := ParseAppleVFHelperResponse(out)
	if parseErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return AppleVFHelperResponse{}, fmt.Errorf("apple-vf helper %s output is not parseable: %w: %s", command, parseErr, detail)
		}
		return AppleVFHelperResponse{}, fmt.Errorf("apple-vf helper %s output is not parseable: %w", command, parseErr)
	}
	if err != nil {
		if strings.TrimSpace(resp.Error) != "" {
			return resp, errors.New(resp.Error)
		}
		return resp, fmt.Errorf("apple-vf helper %s failed: %w", command, err)
	}
	if !resp.OK {
		if strings.TrimSpace(resp.Error) != "" {
			return resp, errors.New(resp.Error)
		}
		return resp, fmt.Errorf("apple-vf helper %s returned ok=false", command)
	}
	if resp.Backend != BackendAppleVFMicroVM {
		return resp, fmt.Errorf("apple-vf helper reported backend %q, want %q", resp.Backend, BackendAppleVFMicroVM)
	}
	return resp, nil
}

func ParseAppleVFHelperHealth(data []byte) (AppleVFHelperHealth, error) {
	resp, err := ParseAppleVFHelperResponse(data)
	if err != nil {
		return AppleVFHelperHealth{}, err
	}
	return AppleVFHelperHealth{
		OK:                      resp.OK,
		Backend:                 resp.Backend,
		Command:                 string(resp.Command),
		Version:                 resp.Version,
		RequestID:               resp.RequestID,
		RuntimeID:               resp.RuntimeID,
		Role:                    string(resp.Role),
		AgencyHomeHash:          resp.AgencyHomeHash,
		Darwin:                  resp.Darwin,
		Arch:                    resp.Arch,
		VirtualizationAvailable: resp.VirtualizationAvailable,
		Error:                   resp.Error,
	}, nil
}
