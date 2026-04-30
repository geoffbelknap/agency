package orchestrate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/geoffbelknap/agency/internal/infratier"
)

const maxInfraLogTail = 10000

// InfraLogs returns recent logs for shared infrastructure services without
// exposing the host/container implementation detail to callers.
func (inf *Infra) InfraLogs(ctx context.Context, component string, tail int) (string, error) {
	component = strings.TrimSpace(component)
	if !validInfraLogComponent(component) {
		return "", fmt.Errorf("unknown infrastructure component %q", component)
	}
	if tail <= 0 {
		tail = 200
	}
	if tail > maxInfraLogTail {
		tail = maxInfraLogTail
	}
	if inf.hostInfraLogsEnabled(component) {
		return inf.hostInfraLogs(component, tail)
	}
	if inf.Backend == nil {
		return "", fmt.Errorf("infrastructure runtime backend unavailable")
	}
	return inf.Backend.InfraLogs(ctx, component, tail)
}

func (inf *Infra) hostInfraLogs(component string, tail int) (string, error) {
	data, err := os.ReadFile(inf.hostInfraMetadataPath(component))
	if err != nil {
		return "", fmt.Errorf("host infrastructure metadata for %s is unavailable: %w", component, err)
	}
	var meta hostInfraMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("host infrastructure metadata for %s is invalid: %w", component, err)
	}
	if strings.TrimSpace(meta.LogFile) == "" {
		return "", fmt.Errorf("host infrastructure metadata for %s does not include a log file", component)
	}
	return tailFile(meta.LogFile, tail)
}

func (inf *Infra) hostInfraLogsEnabled(component string) bool {
	switch component {
	case "comms":
		return inf.hostCommsEnabled()
	case "knowledge":
		return inf.hostKnowledgeEnabled()
	case "egress":
		return inf.hostEgressEnabled()
	case "web":
		return inf.hostWebEnabled()
	default:
		return false
	}
}

func validInfraLogComponent(component string) bool {
	for _, candidate := range infratier.StatusComponents() {
		if component == candidate {
			return true
		}
	}
	return false
}

func tailFile(path string, tail int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	lines := make([]string, 0, tail)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(lines) < tail {
			lines = append(lines, line)
			continue
		}
		copy(lines, lines[1:])
		lines[len(lines)-1] = line
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n") + "\n", nil
}
