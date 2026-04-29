package runtimebackend

import (
	"bytes"
	"encoding/json"
)

type AppleVFHelperCommand string

const (
	AppleVFCommandHealth  AppleVFHelperCommand = "health"
	AppleVFCommandVersion AppleVFHelperCommand = "version"
	AppleVFCommandPrepare AppleVFHelperCommand = "prepare"
	AppleVFCommandStart   AppleVFHelperCommand = "start"
	AppleVFCommandStop    AppleVFHelperCommand = "stop"
	AppleVFCommandKill    AppleVFHelperCommand = "kill"
	AppleVFCommandInspect AppleVFHelperCommand = "inspect"
	AppleVFCommandDelete  AppleVFHelperCommand = "delete"
	AppleVFCommandEvents  AppleVFHelperCommand = "events"
)

type AppleVFComponentRole string

const (
	AppleVFRoleWorkload AppleVFComponentRole = "workload"
	AppleVFRoleEnforcer AppleVFComponentRole = "enforcer"
)

type AppleVFHelperVMConfig struct {
	KernelPath      string `json:"kernelPath,omitempty"`
	RootFSPath      string `json:"rootfsPath,omitempty"`
	StateDir        string `json:"stateDir,omitempty"`
	MemoryMiB       int64  `json:"memoryMiB,omitempty"`
	CPUCount        int64  `json:"cpuCount,omitempty"`
	EnforcementMode string `json:"enforcementMode,omitempty"`
}

type AppleVFHelperRequest struct {
	RequestID      string                 `json:"requestID,omitempty"`
	RuntimeID      string                 `json:"runtimeID,omitempty"`
	Role           AppleVFComponentRole   `json:"role,omitempty"`
	Backend        string                 `json:"backend,omitempty"`
	AgencyHomeHash string                 `json:"agencyHomeHash,omitempty"`
	Config         *AppleVFHelperVMConfig `json:"config,omitempty"`
}

type AppleVFHelperResponse struct {
	OK                      bool                 `json:"ok"`
	Backend                 string               `json:"backend"`
	Command                 AppleVFHelperCommand `json:"command"`
	Version                 string               `json:"version"`
	RequestID               string               `json:"requestID,omitempty"`
	RuntimeID               string               `json:"runtimeID,omitempty"`
	Role                    AppleVFComponentRole `json:"role,omitempty"`
	AgencyHomeHash          string               `json:"agencyHomeHash,omitempty"`
	Darwin                  string               `json:"darwin,omitempty"`
	Arch                    string               `json:"arch,omitempty"`
	VirtualizationAvailable bool                 `json:"virtualizationAvailable"`
	VMState                 string               `json:"vmState,omitempty"`
	Details                 map[string]string    `json:"details,omitempty"`
	Error                   string               `json:"error,omitempty"`
}

type AppleVFHelperEvent struct {
	Backend        string               `json:"backend"`
	Version        string               `json:"version"`
	RequestID      string               `json:"requestID,omitempty"`
	RuntimeID      string               `json:"runtimeID"`
	Role           AppleVFComponentRole `json:"role"`
	AgencyHomeHash string               `json:"agencyHomeHash,omitempty"`
	Type           string               `json:"type"`
	VMState        string               `json:"vmState,omitempty"`
	Detail         string               `json:"detail,omitempty"`
}

func (r AppleVFHelperRequest) JSONArg() (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ParseAppleVFHelperResponse(data []byte) (AppleVFHelperResponse, error) {
	var resp AppleVFHelperResponse
	if err := json.Unmarshal(bytes.TrimSpace(data), &resp); err != nil {
		return AppleVFHelperResponse{}, err
	}
	return resp, nil
}

func ParseAppleVFHelperEvent(data []byte) (AppleVFHelperEvent, error) {
	var event AppleVFHelperEvent
	if err := json.Unmarshal(bytes.TrimSpace(data), &event); err != nil {
		return AppleVFHelperEvent{}, err
	}
	return event, nil
}
