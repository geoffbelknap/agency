package orchestrate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type hostInfraMetadata struct {
	Component string   `json:"component"`
	Service   string   `json:"service"`
	PID       int      `json:"pid"`
	PIDFile   string   `json:"pid_file"`
	BuildID   string   `json:"build_id,omitempty"`
	Command   []string `json:"command"`
	LogFile   string   `json:"log_file,omitempty"`
	HealthURL string   `json:"health_url,omitempty"`
	StartedAt string   `json:"started_at"`
}

func (inf *Infra) hostInfraPIDPath(component string) string {
	return filepath.Join(inf.Home, "run", "agency-infra-"+component+".pid")
}

func (inf *Infra) hostInfraMetadataPath(component string) string {
	return filepath.Join(inf.Home, "run", "agency-infra-"+component+".json")
}

func (inf *Infra) legacyHostInfraPIDPath(component string) string {
	return filepath.Join(inf.Home, "run", component+".pid")
}

func (inf *Infra) writeHostInfraPID(component string, pid int) error {
	return os.WriteFile(inf.hostInfraPIDPath(component), []byte(strconv.Itoa(pid)), 0o644)
}

func (inf *Infra) writeHostInfraMetadata(component string, pid int, command []string, logFile, healthURL string) error {
	meta := hostInfraMetadata{
		Component: component,
		Service:   "agency-infra-" + component,
		PID:       pid,
		PIDFile:   inf.hostInfraPIDPath(component),
		BuildID:   inf.BuildID,
		Command:   append([]string(nil), command...),
		LogFile:   logFile,
		HealthURL: healthURL,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(inf.hostInfraMetadataPath(component), data, 0o644)
}

func (inf *Infra) readHostInfraPID(component string) (int, error) {
	data, err := os.ReadFile(inf.hostInfraPIDPath(component))
	if errors.Is(err, os.ErrNotExist) {
		data, err = os.ReadFile(inf.legacyHostInfraPIDPath(component))
	}
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func (inf *Infra) hostInfraPID(component string) (int, bool) {
	pid, err := inf.readHostInfraPID(component)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func (inf *Infra) hostInfraCurrentBuild(component string) bool {
	if strings.TrimSpace(inf.BuildID) == "" {
		return true
	}
	return strings.TrimSpace(inf.hostInfraBuildID(component)) == strings.TrimSpace(inf.BuildID)
}

func (inf *Infra) hostInfraBuildID(component string) string {
	data, err := os.ReadFile(inf.hostInfraMetadataPath(component))
	if err != nil {
		return ""
	}
	var meta hostInfraMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.BuildID)
}

func (inf *Infra) removeHostInfraPID(component string) {
	_ = os.Remove(inf.hostInfraPIDPath(component))
	_ = os.Remove(inf.hostInfraMetadataPath(component))
	_ = os.Remove(inf.legacyHostInfraPIDPath(component))
}
