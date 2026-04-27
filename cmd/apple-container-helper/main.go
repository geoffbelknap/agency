package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const backendName = "apple-container"

type commandRunner func(context.Context, ...string) ([]byte, []byte, error)

type inspectContainer struct {
	Status        string            `json:"status"`
	Configuration inspectConfig     `json:"configuration"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type inspectConfig struct {
	ID     string            `json:"id"`
	Labels map[string]string `json:"labels"`
}

type healthResponse struct {
	OK           bool   `json:"ok"`
	Backend      string `json:"backend"`
	EventSupport string `json:"event_support"`
	Version      string `json:"version,omitempty"`
	Error        string `json:"error,omitempty"`
}

type listOwnedResponse struct {
	OK         bool               `json:"ok"`
	Backend    string             `json:"backend"`
	Containers []inspectContainer `json:"containers"`
}

type inspectResponse struct {
	OK         bool               `json:"ok"`
	Backend    string             `json:"backend"`
	Containers []inspectContainer `json:"containers"`
}

type operationResponse struct {
	OK          bool         `json:"ok"`
	Backend     string       `json:"backend"`
	ContainerID string       `json:"container_id,omitempty"`
	Output      string       `json:"output,omitempty"`
	Error       string       `json:"error,omitempty"`
	Event       *helperEvent `json:"event,omitempty"`
}

type helperEvent struct {
	ID         string         `json:"id"`
	SourceType string         `json:"source_type"`
	SourceName string         `json:"source_name"`
	EventType  string         `json:"event_type"`
	Timestamp  string         `json:"timestamp"`
	Data       map[string]any `json:"data"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, defaultContainerRunner); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, runContainer commandRunner) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: agency-apple-container-helper <health|inspect|list-owned|events|start|stop|kill|delete|exec>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	switch args[0] {
	case "health":
		return writeJSON(stdout, health(ctx, runContainer))
	case "inspect":
		return inspect(ctx, args[1:], stdout, runContainer)
	case "list-owned":
		return listOwned(ctx, args[1:], stdout, runContainer)
	case "events":
		return events(ctx, args[1:], stdout, runContainer)
	case "start":
		return startContainer(ctx, args[1:], stdout, runContainer)
	case "stop":
		return stopContainer(ctx, args[1:], stdout, runContainer)
	case "kill":
		return killContainer(ctx, args[1:], stdout, runContainer)
	case "delete":
		return deleteContainer(ctx, args[1:], stdout, runContainer)
	case "exec":
		return execContainer(ctx, args[1:], stdout, runContainer)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func health(ctx context.Context, runContainer commandRunner) healthResponse {
	resp := healthResponse{
		Backend:      backendName,
		EventSupport: "none",
	}
	if _, _, err := runContainer(ctx, "system", "status", "--format", "json"); err != nil {
		if _, _, fallbackErr := runContainer(ctx, "system", "status"); fallbackErr != nil {
			resp.OK = false
			resp.Error = fallbackErr.Error()
			return resp
		}
	}
	resp.OK = true
	if stdout, _, err := runContainer(ctx, "system", "version", "--format", "json"); err == nil {
		resp.Version = strings.TrimSpace(string(stdout))
	}
	return resp
}

func inspect(ctx context.Context, args []string, stdout io.Writer, runContainer commandRunner) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := fs.Args()
	if len(ids) == 0 {
		return fmt.Errorf("inspect requires at least one container id")
	}
	raw, _, err := runContainer(ctx, append([]string{"inspect"}, ids...)...)
	if err != nil {
		return err
	}
	containers, err := parseInspect(raw)
	if err != nil {
		return err
	}
	return writeJSON(stdout, inspectResponse{OK: true, Backend: backendName, Containers: containers})
}

func listOwned(ctx context.Context, args []string, stdout io.Writer, runContainer commandRunner) error {
	fs := flag.NewFlagSet("list-owned", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	homeHash := fs.String("home-hash", "", "Agency home ownership hash")
	all := fs.Bool("all", true, "include stopped containers")
	if err := fs.Parse(args); err != nil {
		return err
	}

	containerArgs := []string{"list", "--format", "json"}
	if *all {
		containerArgs = append(containerArgs, "--all")
	}
	raw, _, err := runContainer(ctx, containerArgs...)
	if err != nil {
		return err
	}
	containers, err := parseInspect(raw)
	if err != nil {
		return err
	}

	filtered := make([]inspectContainer, 0, len(containers))
	for _, ctr := range containers {
		labels := mergedLabels(ctr)
		if labels["agency.managed"] != "true" || labels["agency.backend"] != backendName {
			continue
		}
		if strings.TrimSpace(*homeHash) != "" && labels["agency.home"] != strings.TrimSpace(*homeHash) {
			continue
		}
		filtered = append(filtered, ctr)
	}
	return writeJSON(stdout, listOwnedResponse{OK: true, Backend: backendName, Containers: filtered})
}

func events(ctx context.Context, args []string, stdout io.Writer, runContainer commandRunner) error {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	homeHash := fs.String("home-hash", "", "Agency home ownership hash")
	once := fs.Bool("once", false, "emit one reconciliation batch and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*once {
		return fmt.Errorf("live Apple Container helper events require the wait-backed helper implementation; use --once for bounded reconciliation")
	}

	containerArgs := []string{"list", "--format", "json", "--all"}
	raw, _, err := runContainer(ctx, containerArgs...)
	if err != nil {
		return err
	}
	containers, err := parseInspect(raw)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, ctr := range containers {
		labels := mergedLabels(ctr)
		if labels["agency.managed"] != "true" || labels["agency.backend"] != backendName {
			continue
		}
		if strings.TrimSpace(*homeHash) != "" && labels["agency.home"] != strings.TrimSpace(*homeHash) {
			continue
		}
		if err := enc.Encode(eventFromContainer(ctr, now)); err != nil {
			return err
		}
	}
	return nil
}

func startContainer(ctx context.Context, args []string, stdout io.Writer, runContainer commandRunner) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := fs.Args()
	if len(ids) != 1 {
		return fmt.Errorf("start requires exactly one container id")
	}
	return runVerifiedOperation(ctx, stdout, runContainer, ids[0], "operator_start", []string{"running"}, "start", ids[0])
}

func stopContainer(ctx context.Context, args []string, stdout io.Writer, runContainer commandRunner) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	timeout := fs.Int("time", 0, "seconds to wait before forcing stop")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := fs.Args()
	if len(ids) != 1 {
		return fmt.Errorf("stop requires exactly one container id")
	}
	containerArgs := []string{"stop"}
	if *timeout > 0 {
		containerArgs = append(containerArgs, "--time", fmt.Sprintf("%d", *timeout))
	}
	containerArgs = append(containerArgs, ids[0])
	return runVerifiedOperation(ctx, stdout, runContainer, ids[0], "operator_stop", []string{"stopped", "exited"}, containerArgs...)
}

func killContainer(ctx context.Context, args []string, stdout io.Writer, runContainer commandRunner) error {
	fs := flag.NewFlagSet("kill", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	signal := fs.String("signal", "", "signal to send")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := fs.Args()
	if len(ids) != 1 {
		return fmt.Errorf("kill requires exactly one container id")
	}
	containerArgs := []string{"kill"}
	if strings.TrimSpace(*signal) != "" {
		containerArgs = append(containerArgs, "--signal", strings.TrimSpace(*signal))
	}
	containerArgs = append(containerArgs, ids[0])
	return runVerifiedOperation(ctx, stdout, runContainer, ids[0], "operator_kill", []string{"stopped", "exited"}, containerArgs...)
}

func deleteContainer(ctx context.Context, args []string, stdout io.Writer, runContainer commandRunner) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("force", false, "delete even if running")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := fs.Args()
	if len(ids) != 1 {
		return fmt.Errorf("delete requires exactly one container id")
	}
	containerArgs := []string{"delete"}
	if *force {
		containerArgs = append(containerArgs, "--force")
	}
	containerArgs = append(containerArgs, ids[0])
	before, _ := inspectOne(ctx, runContainer, ids[0])
	out, errOut, err := runContainer(ctx, containerArgs...)
	resp := operationResponse{
		OK:          err == nil,
		Backend:     backendName,
		ContainerID: ids[0],
		Output:      string(append(out, errOut...)),
	}
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Event = deletedEvent(ids[0], before, time.Now().UTC().Format(time.RFC3339Nano))
	}
	return writeJSON(stdout, resp)
}

func execContainer(ctx context.Context, args []string, stdout io.Writer, runContainer commandRunner) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	user := fs.String("user", "", "user to execute as")
	workdir := fs.String("workdir", "", "working directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("exec requires a container id and command arguments")
	}
	containerID := remaining[0]
	containerArgs := []string{"exec"}
	if strings.TrimSpace(*user) != "" {
		containerArgs = append(containerArgs, "--user", strings.TrimSpace(*user))
	}
	if strings.TrimSpace(*workdir) != "" {
		containerArgs = append(containerArgs, "--workdir", strings.TrimSpace(*workdir))
	}
	containerArgs = append(containerArgs, containerID)
	containerArgs = append(containerArgs, remaining[1:]...)
	return runOperation(ctx, stdout, runContainer, containerID, containerArgs...)
}

func runOperation(ctx context.Context, stdout io.Writer, runContainer commandRunner, containerID string, args ...string) error {
	out, errOut, err := runContainer(ctx, args...)
	resp := operationResponse{
		OK:          err == nil,
		Backend:     backendName,
		ContainerID: containerID,
		Output:      string(append(out, errOut...)),
	}
	if err != nil {
		resp.Error = err.Error()
	}
	return writeJSON(stdout, resp)
}

func runVerifiedOperation(ctx context.Context, stdout io.Writer, runContainer commandRunner, containerID, reason string, expectedStatuses []string, args ...string) error {
	out, errOut, err := runContainer(ctx, args...)
	resp := operationResponse{
		OK:          err == nil,
		Backend:     backendName,
		ContainerID: containerID,
		Output:      string(append(out, errOut...)),
	}
	if err != nil {
		resp.Error = err.Error()
		return writeJSON(stdout, resp)
	}
	ctr, err := inspectOne(ctx, runContainer, containerID)
	if err != nil {
		resp.OK = false
		resp.Error = "verification failed: " + err.Error()
		resp.Event = stateUnknownEvent(containerID, reason, time.Now().UTC().Format(time.RFC3339Nano))
		return writeJSON(stdout, resp)
	}
	event := eventFromContainerReason(ctr, reason, time.Now().UTC().Format(time.RFC3339Nano))
	resp.Event = &event
	if !statusIn(ctr.Status, expectedStatuses) {
		resp.OK = false
		resp.Error = fmt.Sprintf("verification failed: container status %q, expected %s", ctr.Status, strings.Join(expectedStatuses, " or "))
	}
	return writeJSON(stdout, resp)
}

func inspectOne(ctx context.Context, runContainer commandRunner, containerID string) (inspectContainer, error) {
	raw, _, err := runContainer(ctx, "inspect", containerID)
	if err != nil {
		return inspectContainer{}, err
	}
	containers, err := parseInspect(raw)
	if err != nil {
		return inspectContainer{}, err
	}
	if len(containers) == 0 {
		return inspectContainer{}, fmt.Errorf("container %q not found", containerID)
	}
	return containers[0], nil
}

func statusIn(status string, expected []string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	for _, candidate := range expected {
		if normalized == strings.ToLower(strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func eventFromContainer(ctr inspectContainer, timestamp string) helperEvent {
	return eventFromContainerReason(ctr, "reconcile_once", timestamp)
}

func eventFromContainerReason(ctr inspectContainer, reason, timestamp string) helperEvent {
	labels := mergedLabels(ctr)
	containerID := ctr.Configuration.ID
	eventType := "runtime.container.state_observed"
	switch reason {
	case "operator_start":
		eventType = "runtime.container.started"
	case "operator_stop":
		eventType = "runtime.container.stopped"
	case "operator_kill":
		eventType = "runtime.container.killed"
	default:
		switch strings.ToLower(strings.TrimSpace(ctr.Status)) {
		case "running":
			eventType = "runtime.container.started"
		case "stopped", "exited":
			eventType = "runtime.container.stopped"
		}
	}
	role := firstNonEmpty(labels["agency.role"], labels["agency.type"])
	eventKey := strings.Join([]string{
		backendName,
		containerID,
		ctr.Status,
		labels["agency.home"],
	}, "|")
	return helperEvent{
		ID:         "evt-runtime-" + shortHash(eventKey),
		SourceType: "platform",
		SourceName: "host-adapter/apple-container",
		EventType:  eventType,
		Timestamp:  timestamp,
		Data: map[string]any{
			"backend":      backendName,
			"container_id": containerID,
			"agent":        labels["agency.agent"],
			"role":         role,
			"instance":     labels["agency.instance"],
			"status":       ctr.Status,
			"reason":       reason,
		},
		Metadata: map[string]any{
			"agency_home_hash": labels["agency.home"],
			"owned":            labels["agency.managed"] == "true",
		},
	}
}

func deletedEvent(containerID string, before inspectContainer, timestamp string) *helperEvent {
	if before.Configuration.ID == "" {
		before.Configuration.ID = containerID
	}
	event := eventFromContainerReason(before, "operator_delete", timestamp)
	event.EventType = "runtime.container.deleted"
	event.Data["status"] = "deleted"
	return &event
}

func stateUnknownEvent(containerID, reason, timestamp string) *helperEvent {
	event := helperEvent{
		ID:         "evt-runtime-" + shortHash(strings.Join([]string{backendName, containerID, "unknown", reason}, "|")),
		SourceType: "platform",
		SourceName: "host-adapter/apple-container",
		EventType:  "runtime.container.state_unknown",
		Timestamp:  timestamp,
		Data: map[string]any{
			"backend":      backendName,
			"container_id": containerID,
			"status":       "unknown",
			"reason":       reason,
		},
		Metadata: map[string]any{"owned": true},
	}
	return &event
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseInspect(raw []byte) ([]inspectContainer, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var list []inspectContainer
	if err := json.Unmarshal(trimmed, &list); err == nil {
		return list, nil
	}
	var single inspectContainer
	if err := json.Unmarshal(trimmed, &single); err != nil {
		return nil, err
	}
	return []inspectContainer{single}, nil
}

func mergedLabels(ctr inspectContainer) map[string]string {
	if len(ctr.Configuration.Labels) > 0 {
		return ctr.Configuration.Labels
	}
	return ctr.Labels
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

func defaultContainerRunner(ctx context.Context, args ...string) ([]byte, []byte, error) {
	binary := strings.TrimSpace(os.Getenv("AGENCY_APPLE_CONTAINER_BIN"))
	if binary == "" {
		binary = "container"
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = appleContainerCommandEnv(os.Environ())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("container %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func appleContainerCommandEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "AGENCY_HOME" {
			continue
		}
		out = append(out, entry)
	}
	return out
}
