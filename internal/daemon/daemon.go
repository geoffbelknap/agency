package daemon

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// PIDFile returns the path to ~/.agency/gateway.pid
func PIDFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agency", "gateway.pid")
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

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

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
func Stop() error {
	pidFile := PIDFile()
	if pidFile == "" {
		return fmt.Errorf("cannot determine PID file path")
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("no daemon PID file found: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		os.Remove(pidFile)
		return fmt.Errorf("invalid PID in file: %w", err)
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

	os.Remove(pidFile)
	return nil
}

// EnsureRunning starts the daemon if not already running.
func EnsureRunning(port int) error {
	if IsRunning(port) {
		return nil
	}
	return Start(port)
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
