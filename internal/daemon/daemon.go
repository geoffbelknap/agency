package daemon

import (
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

	agencydocker "github.com/geoffbelknap/agency/internal/docker"
)

const (
	startLockWaitTimeout = 15 * time.Second
	startLockStaleAfter  = 30 * time.Second
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
					resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", port))
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
	agencydocker.EnsureUsableHostEnv()

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
	deadline := time.Now().Add(10 * time.Second)
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

	return fmt.Errorf("daemon started (pid %d) but health check timed out after 10s; check %s",
		cmd.Process.Pid, logPath)
}

// Stop sends SIGTERM to the daemon process and removes the PID file.
// If the PID file is missing, it falls back to finding the process by
// scanning for "agency serve" in the process list.
func Stop() error {
	pidFile := PIDFile()
	if pidFile == "" {
		return fmt.Errorf("cannot determine PID file path")
	}

	pid, err := readPID(pidFile)
	if err != nil {
		// PID file missing or invalid — try to find the process by command line
		foundPID, findErr := findDaemonProcess()
		if findErr != nil || foundPID == 0 {
			return fmt.Errorf("no daemon PID file found and could not locate process: %w", err)
		}
		pid = foundPID
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidFile)
		return fmt.Errorf("process not found: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		os.Remove(pidFile)
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			os.Remove(pidFile)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		os.Remove(pidFile)
		return fmt.Errorf("daemon did not exit after SIGTERM and SIGKILL failed: %w", err)
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

// findDaemonProcess scans /proc for a running "agency serve" process.
// Returns the PID if found, 0 otherwise.
func findDaemonProcess() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return findDaemonProcessWithPS()
	}
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		// cmdline is null-separated; check for "agency" and "serve"
		s := string(cmdline)
		if strings.Contains(s, "agency") && strings.Contains(s, "serve") {
			return pid, nil
		}
	}
	return 0, nil
}

func findDaemonProcessWithPS() (int, error) {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return 0, err
	}
	self := os.Getpid()
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
		command := strings.Join(fields[1:], " ")
		if strings.Contains(command, "agency") && strings.Contains(command, "serve") {
			return pid, nil
		}
	}
	return 0, nil
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
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}
