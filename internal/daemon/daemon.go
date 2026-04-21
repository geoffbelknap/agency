package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

const (
	startLockWaitTimeout = 15 * time.Second
	startLockStaleAfter  = 30 * time.Second
	defaultStartTimeout  = 10 * time.Second
)

func agencyHome() string {
	if home := os.Getenv("AGENCY_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agency")
}

// PIDFile returns the path to the Agency home gateway pid file.
func PIDFile() string {
	home := agencyHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "gateway.pid")
}

func startupLockFile() string {
	home := agencyHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "gateway.start.lock")
}

func daemonStartTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("AGENCY_DAEMON_START_TIMEOUT"))
	if raw == "" {
		return defaultStartTimeout
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return duration
	}
	return defaultStartTimeout
}

// IsRunning checks if a daemon is running by:
// 1. Reading PID from ~/.agency/gateway.pid and verifying the process
// 2. Falling back to health endpoint check if PID file is missing
func IsRunning(port int) bool {
	pidFile := PIDFile()
	if pidFile == "" {
		return false
	}

	data, err := os.ReadFile(pidFile)
	if err == nil {
		pid, err := strconv.Atoi(string(data))
		if err == nil {
			proc, err := os.FindProcess(pid)
			if err == nil {
				if err := proc.Signal(syscall.Signal(0)); err == nil {
					// Process exists — verify health endpoint
					client := &http.Client{Timeout: 2 * time.Second}
					resp, err := client.Get(healthURL(port))
					if err == nil {
						resp.Body.Close()
						if resp.StatusCode == 200 {
							return true
						}
					}
				}
			}
		}
		// PID file exists but process is dead — clean up
		os.Remove(pidFile)
	}

	// No PID file or stale PID — check if health endpoint responds anyway
	// (daemon may be running without a PID file)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// Start spawns "agency serve" as a detached background process.
// It writes the PID to ~/.agency/gateway.pid and waits for the health
// endpoint to become available with exponential backoff.
func Start(port int) error {
	pidFile := PIDFile()
	if pidFile == "" {
		return fmt.Errorf("cannot determine home directory for PID file")
	}

	// Ensure ~/.agency/ directory exists
	agencyDir := filepath.Dir(pidFile)
	if err := os.MkdirAll(agencyDir, 0755); err != nil {
		return fmt.Errorf("create agency dir: %w", err)
	}

	releaseLock, alreadyStarting, err := acquireStartLock(func() bool { return IsRunning(port) }, startLockWaitTimeout)
	if err != nil {
		return err
	}
	if alreadyStarting {
		return nil
	}
	defer releaseLock()

	if IsRunning(port) {
		return nil
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	runtimehost.EnsureUsableHostEnv()

	// Open log file for daemon stdout/stderr
	logPath := filepath.Join(agencyDir, "gateway.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	// Spawn "agency serve" as a detached background process.
	// Don't pass --http; the serve command reads gateway_addr from config.yaml.
	// The daemon only needs the port for its health check.
	cmd := exec.Command(exePath, "serve")
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	setDetached(cmd)

	if err := cmd.Start(); err != nil {
		if cerr := logFile.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "close log file: %v\n", cerr)
		}
		return fmt.Errorf("start daemon: %w", err)
	}

	// Write PID file
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		if cerr := logFile.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "close log file: %v\n", cerr)
		}
		return fmt.Errorf("write PID file: %w", err)
	}

	// Don't wait for the child — it's detached
	go func() {
		cmd.Wait()
		if err := logFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close log file: %v\n", err)
		}
	}()

	// Wait for health endpoint with exponential backoff
	delay := 100 * time.Millisecond
	timeout := daemonStartTimeout()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(delay)
		if isHealthy(port) {
			return nil
		}
		delay *= 2
		if delay > 2*time.Second {
			delay = 2 * time.Second
		}
	}

	return fmt.Errorf("daemon started (pid %d) but health check timed out after %s; check %s",
		cmd.Process.Pid, timeout.Round(time.Second), logPath)
}

// Stop sends SIGTERM to the daemon process and removes the PID file.
// If the PID file is missing, it falls back to finding the process by
// scanning for "agency serve" in the process list.
func Stop() error {
	pidFile := PIDFile()
	if pidFile == "" {
		return fmt.Errorf("cannot determine PID file path")
	}

	pids, err := daemonPIDs()
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		if _, readErr := readPID(pidFile); readErr != nil {
			return fmt.Errorf("no daemon PID file found and could not locate process: %w", readErr)
		}
		return fmt.Errorf("daemon PID file exists but no matching agency serve process was found")
	}

	procs := make(map[int]*os.Process, len(pids))
	for _, pid := range pids {
		proc, findErr := os.FindProcess(pid)
		if findErr != nil {
			continue
		}
		procs[pid] = proc
	}
	if len(procs) == 0 {
		os.Remove(pidFile)
		return fmt.Errorf("process not found")
	}

	for pid, proc := range procs {
		if err := proc.Signal(syscall.SIGTERM); err != nil && !isProcessGone(err) {
			os.Remove(pidFile)
			return fmt.Errorf("send SIGTERM to pid %d: %w", pid, err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		allExited := true
		for _, proc := range procs {
			if err := proc.Signal(syscall.Signal(0)); err == nil {
				allExited = false
				break
			}
		}
		if allExited {
			os.Remove(pidFile)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	for pid, proc := range procs {
		if err := proc.Signal(syscall.SIGKILL); err != nil && !isProcessGone(err) {
			os.Remove(pidFile)
			return fmt.Errorf("daemon did not exit after SIGTERM and SIGKILL failed for pid %d: %w", pid, err)
		}
	}

	os.Remove(pidFile)
	return nil
}

// readPID reads and parses the PID from a file.
func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		os.Remove(path)
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}
	return pid, nil
}

func daemonPIDs() ([]int, error) {
	pids, err := findDaemonProcesses()
	if err != nil {
		return nil, err
	}
	seen := make(map[int]struct{}, len(pids)+1)
	ordered := make([]int, 0, len(pids)+1)
	if pid, err := readPID(PIDFile()); err == nil && pid > 0 {
		seen[pid] = struct{}{}
		ordered = append(ordered, pid)
	}
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		ordered = append(ordered, pid)
	}
	return ordered, nil
}

// findDaemonProcesses scans for running "agency serve" processes launched
// from the current executable path. Returns every matching PID.
func findDaemonProcesses() ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return findDaemonProcessWithPS()
	}
	exePaths := expectedExecutablePaths()
	self := os.Getpid()
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if isMatchingServeArgv(splitCmdline(cmdline), exePaths) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func findDaemonProcessWithPS() ([]int, error) {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}
	return parsePSDaemonProcesses(out, expectedExecutablePaths(), os.Getpid()), nil
}

func expectedExecutablePaths() []string {
	exePath, err := os.Executable()
	if err != nil || exePath == "" {
		return nil
	}
	paths := []string{exePath}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" && resolved != exePath {
		paths = append(paths, resolved)
	}
	return paths
}

func splitCmdline(cmdline []byte) []string {
	cmdline = bytes.TrimRight(cmdline, "\x00")
	if len(cmdline) == 0 {
		return nil
	}
	raw := bytes.Split(cmdline, []byte{0})
	argv := make([]string, 0, len(raw))
	for _, part := range raw {
		if len(part) == 0 {
			continue
		}
		argv = append(argv, string(part))
	}
	return argv
}

func isMatchingServeArgv(argv []string, exePaths []string) bool {
	if len(argv) < 2 || argv[1] != "serve" {
		return false
	}
	if len(argv) > 2 && !strings.HasPrefix(argv[2], "-") {
		return false
	}
	for _, exePath := range exePaths {
		if argv[0] == exePath {
			return true
		}
	}
	return false
}

func parsePSDaemonProcesses(out []byte, exePaths []string, self int) []int {
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == self {
			continue
		}
		if isMatchingServeCommand(strings.Join(fields[1:], " "), exePaths) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func isMatchingServeCommand(command string, exePaths []string) bool {
	fields := strings.Fields(command)
	if !isMatchingServeArgv(fields, exePaths) {
		return false
	}
	for _, exePath := range exePaths {
		if fields[0] == exePath {
			return true
		}
	}
	return false
}

func isProcessGone(err error) bool {
	return err != nil && (errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH))
}

// EnsureRunning starts the daemon if not already running.
func EnsureRunning(port int) error {
	if IsRunning(port) {
		return nil
	}
	return Start(port)
}

func acquireStartLock(waitForHealthy func() bool, timeout time.Duration) (release func(), alreadyStarted bool, err error) {
	lockFile := startupLockFile()
	if lockFile == "" {
		return nil, false, fmt.Errorf("cannot determine daemon startup lock path")
	}

	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() {
				_ = os.Remove(lockFile)
			}, false, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, false, fmt.Errorf("create daemon startup lock: %w", err)
		}

		if waitForHealthy != nil && waitForHealthy() {
			return func() {}, true, nil
		}

		info, statErr := os.Stat(lockFile)
		if statErr == nil && time.Since(info.ModTime()) > startLockStaleAfter {
			_ = os.Remove(lockFile)
			continue
		}
		if statErr != nil && errors.Is(statErr, os.ErrNotExist) {
			continue
		}

		if time.Now().After(deadline) {
			if waitForHealthy != nil && waitForHealthy() {
				return func() {}, true, nil
			}
			return nil, false, fmt.Errorf("timed out waiting for daemon startup lock")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// isHealthy checks if the health endpoint responds with 200.
func isHealthy(port int) bool {
	addr := healthAddr(port)
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(healthURL(port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func healthURL(port int) string {
	return "http://" + healthAddr(port) + "/api/v1/health"
}

func healthAddr(port int) string {
	cfg := config.Load()
	if cfg != nil && strings.TrimSpace(cfg.GatewayAddr) != "" {
		host, cfgPort, err := net.SplitHostPort(cfg.GatewayAddr)
		if err == nil && cfgPort != "" {
			if host == "" || host == "0.0.0.0" || host == "::" {
				host = "127.0.0.1"
			}
			return net.JoinHostPort(host, cfgPort)
		}
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
