package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

// interactiveInstallEnabled reports whether the current invocation can
// safely prompt the user and run installers. Interactive mode is opt-out
// via AGENCY_NO_INTERACTIVE=1 (for CI, Make targets, MCP servers) and
// requires a real TTY on stdin so we don't hang waiting for input from a
// pipe.
func interactiveInstallEnabled() bool {
	if strings.TrimSpace(os.Getenv("AGENCY_NO_INTERACTIVE")) != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptYesNo reads a single y/n answer from stdin. Empty input returns
// the supplied default. Anything else is resolved to true/false by its
// leading character.
func promptYesNo(question string, defaultYes bool) bool {
	suffix := "[Y/n]"
	if !defaultYes {
		suffix = "[y/N]"
	}
	fmt.Fprintf(os.Stderr, "%s %s ", question, suffix)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return defaultYes
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans == "" {
		return defaultYes
	}
	return ans[0] == 'y'
}

// promptPickBackend asks the user which of multiple reachable backends
// to use. Empty input keeps the first entry (the configured preference
// order — podman when present). Returns the chosen detection, or nil
// on EOF/error (caller falls back to the default).
func promptPickBackend(reachable []runtimehost.BackendDetection) *runtimehost.BackendDetection {
	if len(reachable) <= 1 {
		if len(reachable) == 1 {
			return &reachable[0]
		}
		return nil
	}
	fmt.Fprintln(os.Stderr, "Multiple container backends are reachable on this host:")
	for i, d := range reachable {
		mark := "  "
		if i == 0 {
			mark = "* "
		}
		mode := d.Mode
		if mode == "" {
			mode = "available"
		}
		fmt.Fprintf(os.Stderr, "%s[%d] %s (%s)\n", mark, i+1, d.Name(), mode)
	}
	fmt.Fprintf(os.Stderr, "Choose [1-%d, default 1 = %s]: ", len(reachable), reachable[0].Name())
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return &reachable[0]
	}
	raw := strings.TrimSpace(line)
	if raw == "" {
		return &reachable[0]
	}
	var idx int
	if _, err := fmt.Sscanf(raw, "%d", &idx); err != nil || idx < 1 || idx > len(reachable) {
		fmt.Fprintf(os.Stderr, "Invalid choice %q, using default %s.\n", raw, reachable[0].Name())
		return &reachable[0]
	}
	return &reachable[idx-1]
}

// offerBackendInstall is invoked when ProbeAllBackends returned nothing
// reachable. It explains the options, prompts for consent, runs the
// platform-specific installer, and returns a fresh detection if the
// install produced a usable backend. On decline or install failure it
// returns nil — the caller then falls back to printing InstallHint()
// and exiting non-zero.
func offerBackendInstall() *runtimehost.BackendDetection {
	fmt.Fprintln(os.Stderr, "No container backend detected on this host.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Agency recommends rootless podman. We can install it for you via Homebrew:")
	switch runtime.GOOS {
	case "darwin":
		fmt.Fprintln(os.Stderr, "  - brew install podman")
		fmt.Fprintln(os.Stderr, "  - podman machine init && podman machine start")
		fmt.Fprintln(os.Stderr, "    (downloads a Fedora CoreOS VM image, ~600MB, takes 2-3 min)")
	default:
		fmt.Fprintln(os.Stderr, "  - brew install podman")
		fmt.Fprintln(os.Stderr, "  - verify /etc/subuid and /etc/subgid have an entry for your user")
		fmt.Fprintln(os.Stderr, "    (needed for rootless; emits a sudo command if missing)")
	}
	fmt.Fprintln(os.Stderr, "")
	if _, err := exec.LookPath("brew"); err != nil {
		fmt.Fprintln(os.Stderr, "Homebrew isn't on PATH, so we can't install automatically.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, runtimehost.InstallHint())
		return nil
	}
	if !promptYesNo("Install podman now?", true) {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, runtimehost.InstallHint())
		return nil
	}

	if err := brewInstallPodman(); err != nil {
		fmt.Fprintf(os.Stderr, "brew install podman failed: %v\n", err)
		return nil
	}

	if runtime.GOOS == "darwin" {
		if err := ensurePodmanMachineRunning(); err != nil {
			fmt.Fprintf(os.Stderr, "podman machine setup failed: %v\n", err)
			return nil
		}
	} else {
		if err := checkLinuxRootlessPrereqs(); err != nil {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return nil
		}
	}

	fmt.Fprintln(os.Stderr, "Re-probing the podman backend...")
	d := runtimehost.ProbeBackend(runtimehost.BackendProbe{Name: runtimehost.BackendPodman, CLICommand: "podman"})
	if !d.Reachable {
		fmt.Fprintln(os.Stderr, "Install finished but podman is still not reachable:")
		if d.Err != nil {
			fmt.Fprintf(os.Stderr, "  %v\n", d.Err)
		}
		return nil
	}
	fmt.Fprintf(os.Stderr, "podman is now reachable at %s.\n", d.Endpoint)
	return &d
}

// brewInstallPodman shells out to `brew install podman` and streams its
// output so the user sees progress. Returns the brew exit status.
func brewInstallPodman() error {
	fmt.Fprintln(os.Stderr, "Running: brew install podman")
	cmd := exec.Command("brew", "install", "podman")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// ensurePodmanMachineRunning initializes and starts the Podman VM on
// macOS when one isn't already running. Idempotent — skips init if a
// machine already exists, skips start if one is already running.
func ensurePodmanMachineRunning() error {
	if out, err := exec.Command("podman", "machine", "list", "--format", "{{.Name}} {{.Running}}").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		hasRunning := false
		hasMachine := false
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			hasMachine = true
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.EqualFold(fields[1], "true") {
				hasRunning = true
			}
		}
		if hasRunning {
			fmt.Fprintln(os.Stderr, "podman machine is already running.")
			return nil
		}
		if !hasMachine {
			fmt.Fprintln(os.Stderr, "Running: podman machine init  (downloads VM image)")
			initCmd := exec.Command("podman", "machine", "init")
			initCmd.Stdout = os.Stderr
			initCmd.Stderr = os.Stderr
			if err := initCmd.Run(); err != nil {
				return fmt.Errorf("podman machine init: %w", err)
			}
		}
	}
	fmt.Fprintln(os.Stderr, "Running: podman machine start")
	startCmd := exec.Command("podman", "machine", "start")
	startCmd.Stdout = os.Stderr
	startCmd.Stderr = os.Stderr
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("podman machine start: %w", err)
	}
	// Give rootlessport a moment to expose the socket on the host side.
	time.Sleep(2 * time.Second)
	return nil
}

// checkLinuxRootlessPrereqs verifies /etc/subuid and /etc/subgid contain
// an entry for the current user — required by rootless podman so it can
// allocate a user namespace. Returns nil when both are present, or an
// actionable error message with the exact sudo command to fix the gap.
// Not fatal in itself; the caller decides whether to continue.
func checkLinuxRootlessPrereqs() error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	name := u.Username
	missing := []string{}
	for _, path := range []string{"/etc/subuid", "/etc/subgid"} {
		if !userLinePresent(path, name) {
			missing = append(missing, path)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"rootless podman needs user-namespace mappings, but %s %s missing an entry for %s.\n\n  Run once, then re-run 'agency setup':\n\n    sudo usermod --add-subuids 100000-165535 --add-subgids 100000-165535 %s\n\n  (On distributions without --add-subuids, append '%s:100000:65536' to both /etc/subuid and /etc/subgid.)",
		strings.Join(missing, " and "),
		pluralize(len(missing), "is", "are"),
		name,
		name,
		name,
	)
}

// userLinePresent returns true if the given file contains a line
// starting with "$user:". Used for /etc/subuid and /etc/subgid probes
// which have the format "username:start:count" per line.
func userLinePresent(path, username string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	prefix := username + ":"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
