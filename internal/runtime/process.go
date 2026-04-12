package runtime

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

var authorityProcessStarter = startAuthorityProcess
var authorityProcessStopper = stopAuthorityProcess

func startAuthorityProcess(instanceDir string, manifest *Manifest, nodeID string) (int, int, string, error) {
	port, err := reservePort()
	if err != nil {
		return 0, 0, "", err
	}
	exePath, err := os.Executable()
	if err != nil {
		return 0, 0, "", fmt.Errorf("resolve executable path: %w", err)
	}
	logDir := filepath.Join(instanceDir, "runtime", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return 0, 0, "", fmt.Errorf("create runtime log dir: %w", err)
	}
	logPath := filepath.Join(logDir, nodeID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, 0, "", fmt.Errorf("open runtime log: %w", err)
	}

	cmd := exec.Command(exePath, "runtime-authority-serve",
		"--instance-dir", instanceDir,
		"--node-id", nodeID,
		"--port", strconv.Itoa(port),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return 0, 0, "", fmt.Errorf("start authority runtime: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForAuthorityHealth(url, 5*time.Second); err != nil {
		return 0, 0, "", err
	}
	return cmd.Process.Pid, port, url, nil
}

func stopAuthorityProcess(pid int) error {
	if pid == 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	return nil
}

func reservePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve port: %w", err)
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("reserve port: unexpected addr %T", ln.Addr())
	}
	return addr.Port, nil
}

func waitForAuthorityHealth(baseURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("authority runtime at %s did not become healthy", baseURL)
}
