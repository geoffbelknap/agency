package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"log/slog"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/geoffbelknap/agency/internal/api"
	"github.com/geoffbelknap/agency/internal/apiclient"
	auditpkg "github.com/geoffbelknap/agency/internal/audit"
	agencyCLI "github.com/geoffbelknap/agency/internal/cli"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/daemon"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/hostadapter/containerops"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	agencylog "github.com/geoffbelknap/agency/internal/logging"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
	"github.com/geoffbelknap/agency/internal/registry"
	"github.com/geoffbelknap/agency/internal/routing"
	"github.com/geoffbelknap/agency/internal/update"
	"github.com/geoffbelknap/agency/internal/ws"
)

var (
	version   = "dev"
	commit    = "none"
	date      = "unknown"
	buildID   = "unknown"
	sourceDir = "" // stamped by Makefile ldflags for dev builds

	agencyHomeFlag string
)

// isLocalhostOrigin checks whether the given Origin URL refers to a localhost
// address (127.0.0.1, ::1, or "localhost") on any port. Returns false for
// anything else, including malformed URLs.
func isLocalhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// corsMiddleware allows browser-based clients (web UI, dev servers) on
// localhost to call the gateway API. Non-localhost origins are rejected —
// the Access-Control-Allow-Origin header is simply not set, so the browser
// blocks the response.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isLocalhostOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func activeAgencyHome() string {
	if home := os.Getenv("AGENCY_HOME"); home != "" {
		return home
	}
	return filepath.Join(os.Getenv("HOME"), ".agency")
}

func normalizeAgencyHomeFlag(home string) (string, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return "", nil
	}
	if home == "~" || strings.HasPrefix(home, "~/") {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if home == "~" {
			home = userHome
		} else {
			home = filepath.Join(userHome, strings.TrimPrefix(home, "~/"))
		}
	}
	if !filepath.IsAbs(home) {
		abs, err := filepath.Abs(home)
		if err != nil {
			return "", err
		}
		home = abs
	}
	return filepath.Clean(home), nil
}

func applyAgencyHomeFlag(home string) error {
	normalized, err := normalizeAgencyHomeFlag(home)
	if err != nil {
		return err
	}
	if normalized == "" {
		return nil
	}
	return os.Setenv("AGENCY_HOME", normalized)
}

func agencyHomeFlagFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return ""
		}
		if arg == "--agency-home" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(arg, "--agency-home=") {
			return strings.TrimPrefix(arg, "--agency-home=")
		}
		if arg == "-H" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(arg, "-H") && len(arg) > len("-H") {
			return strings.TrimPrefix(arg, "-H")
		}
	}
	return ""
}

func gatewayPortFromConfig() int {
	cfg := config.Load()
	if _, port, err := net.SplitHostPort(cfg.GatewayAddr); err == nil {
		if p, err := strconv.Atoi(port); err == nil {
			return p
		}
	}
	return 8200
}

func customHelp(cmd *cobra.Command, _ []string) {
	// If this is a subcommand (not root), render contextual help
	if cmd.HasParent() {
		subcommandHelp(cmd)
		return
	}

	// Root help — grouped sections
	groups := []struct{ id, title string }{
		{"daily", "Daily Operations"},
		{"agent", "Agent Lifecycle"},
		{"manage", "Management"},
		{"platform", "Platform"},
	}
	grouped := map[string][]*cobra.Command{}
	for _, c := range cmd.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		grouped[c.GroupID] = append(grouped[c.GroupID], c)
	}

	fmt.Println("Agency — Governed AI agents with isolation and auditability")
	fmt.Println()
	fmt.Println("Usage: agency <command> [options]")
	if agencyCLIExperimentalEnabled() {
		fmt.Println()
		fmt.Println("Experimental surfaces are enabled via AGENCY_EXPERIMENTAL_SURFACES=1.")
	}

	for _, g := range groups {
		cmds := grouped[g.id]
		if len(cmds) == 0 {
			continue
		}
		fmt.Printf("\n  %s\n", g.title)
		for _, c := range cmds {
			if c.HasSubCommands() {
				fmt.Printf("    %s  %s\n", pad(c.Name()+" ...", 28), helpShort(c))
			} else {
				use := c.Use
				if i := len(c.Name()); i < len(use) {
					use = c.Name() + use[i:]
				}
				fmt.Printf("    %s  %s\n", pad(use, 28), helpShort(c))
			}
		}
	}

	fmt.Println()
	fmt.Println("Run 'agency <command> --help' for details.")
}

func subcommandHelp(cmd *cobra.Command) {
	// Build the full command path: "agency hub", "agency channel", etc.
	path := cmd.CommandPath()

	fmt.Printf("%s — %s\n", path, cmd.Short)
	fmt.Println()

	if cmd.HasSubCommands() {
		fmt.Printf("Usage: %s <command> [options]\n", path)
		fmt.Println()
		for _, c := range cmd.Commands() {
			if c.Hidden || c.Name() == "help" {
				continue
			}
			use := c.Use
			if i := len(c.Name()); i < len(use) {
				use = c.Name() + use[i:]
			}
			fmt.Printf("  %s  %s\n", pad(use, 28), helpShort(c))
		}
	} else {
		fmt.Printf("Usage: %s\n", cmd.UseLine())
	}

	if agencyCLI.IsExperimentalCommand(cmd.Name()) {
		fmt.Println()
		fmt.Println("This surface is experimental and not part of the default core product path.")
	}

	if cmd.HasLocalFlags() {
		fmt.Println()
		fmt.Println("Options:")
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden {
				return
			}
			name := "--" + f.Name
			if f.Shorthand != "" {
				name = "-" + f.Shorthand + ", " + name
			}
			def := ""
			if f.DefValue != "" && f.DefValue != "false" {
				def = fmt.Sprintf(" (default: %s)", f.DefValue)
			}
			fmt.Printf("  %s  %s%s\n", pad(name, 28), f.Usage, def)
		})
	}

	if cmd.Example != "" {
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println(cmd.Example)
	}

	fmt.Println()
}

func agencyCLIExperimentalEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENCY_EXPERIMENTAL_SURFACES"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func helpShort(cmd *cobra.Command) string {
	short := cmd.Short
	if agencyCLI.IsExperimentalCommand(cmd.Name()) {
		short += " [experimental]"
	}
	return short
}

func main() {
	root := &cobra.Command{
		Use:               "agency",
		Short:             "Agency — An operating system for AI agents",
		Version:           fmt.Sprintf("%s (%s, %s)", version, buildID, date),
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	}
	root.SetHelpFunc(customHelp)
	root.SetUsageFunc(func(cmd *cobra.Command) error { customHelp(cmd, nil); return nil })
	root.PersistentFlags().StringVarP(&agencyHomeFlag, "agency-home", "H", "", "use an alternate Agency home directory")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return applyAgencyHomeFlag(agencyHomeFlag)
	}

	if home := agencyHomeFlagFromArgs(os.Args[1:]); home != "" {
		if err := applyAgencyHomeFlag(home); err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --agency-home: %v\n", err)
			os.Exit(1)
		}
	}

	// RegisterCommands sets up groups — must be called first
	agencyCLI.RegisterCommands(root)

	// Platform commands go in their own group
	root.AddGroup(&cobra.Group{ID: "platform", Title: "Platform:"})

	serve := serveCmd()
	serve.GroupID = "platform"
	serve.AddCommand(daemonStopCmd())
	serve.AddCommand(daemonRestartCmd())
	serve.AddCommand(daemonStatusCmd())
	root.AddCommand(serve)

	setupC := setupCmd()
	setupC.GroupID = "platform"
	root.AddCommand(setupC)

	quickstartC := quickstartCmd()
	quickstartC.GroupID = "platform"
	root.AddCommand(quickstartC)

	// Hidden alias: "init" → "setup" for backwards compatibility
	initAlias := *setupC
	initAlias.Use = "init"
	initAlias.Hidden = true
	root.AddCommand(&initAlias)

	// Start background update check (non-blocking, cached 24h, fail-silent)
	agencyHome := activeAgencyHome()
	waitForUpdate := update.Check(version, agencyHome)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}

	// Print update hint if a newer version was found (stderr so it doesn't
	// pollute piped output)
	if r := waitForUpdate(); r.Newer() {
		fmt.Fprintf(os.Stderr, "\nA new version of agency is available: %s → %s\n", r.Current, r.Latest)
		fmt.Fprintf(os.Stderr, "Update with: brew upgrade agency\n")
	}
}

func serveCmd() *cobra.Command {
	var (
		httpAddr string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the gateway daemon (REST API)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// CLI flag overrides config; if neither set, use default.
			if !cmd.Flags().Changed("http") {
				cfg := config.Load()
				if cfg.GatewayAddr != "" {
					httpAddr = cfg.GatewayAddr
				}
			}
			return runServe(httpAddr)
		},
	}

	// Default to 127.0.0.1 for security (ASK Tenet 4: least privilege).
	//
	// Cross-platform container access (Linux Docker CE, macOS/Windows Docker Desktop)
	// does not require binding the gateway to 0.0.0.0. Containers reach the gateway
	// through the gateway-proxy service on the Docker mediation network (gateway:8200),
	// while host-side clients continue to use localhost.
	cmd.Flags().StringVar(&httpAddr, "http", "127.0.0.1:8200", "HTTP API listen address")

	return cmd
}

func setupCmd() *cobra.Command {
	var (
		name                string
		preset              string
		provider            string
		apiKey              string
		notifyURL           string
		backend             string
		configurePool       bool
		experimentalBackend bool
		noInfra             bool
		noBrowser           bool
		noDockerStart       bool //nolint:unused // retained for backward-compat flag --no-docker-start
		cliMode             bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up the Agency platform (config, daemon, infrastructure)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Select the runtime backend before anything writes config or starts
			// a daemon. Strategic backends are selected automatically by host OS;
			// transitional container backends require explicit opt-in.
			var (
				backendName string
				backendCfg  map[string]string
			)
			if !noInfra {
				b, cfg, err := selectRuntimeBackend(backend, experimentalBackend)
				if err != nil {
					return err
				}
				backendName, backendCfg = b, cfg
			}

			if cliMode {
				// Quick setup: if --name or --preset flags are set, skip prompts
				if name != "" || preset != "" {
					return runSetup(provider, apiKey, notifyURL, backendName, backendCfg, noInfra, true, noBrowser, configurePool)
				}

				// Interactive: prompt for provider/key if not set via flags
				if provider == "" && !cmd.Flags().Changed("provider") {
					scanner := bufio.NewScanner(os.Stdin)

					fmt.Println("Agency Setup")
					fmt.Println()
					fmt.Println("LLM Provider:")
					providerChoices := quickstartProviderDescriptors()
					defaultChoice := 1
					for i, descriptor := range providerChoices {
						label := descriptor.DisplayName
						if descriptor.PromptBlurb != "" {
							label = fmt.Sprintf("%s (%s)", label, descriptor.PromptBlurb)
						} else if descriptor.Recommended {
							label = fmt.Sprintf("%s (recommended)", label)
						}
						fmt.Printf("  %d. %s\n", i+1, label)
						if descriptor.Recommended {
							defaultChoice = i + 1
						}
					}
					skipChoice := len(providerChoices) + 1
					fmt.Printf("  %d. Skip (configure later)\n", skipChoice)
					fmt.Println()
					fmt.Printf("Select [1-%d, default %d]: ", skipChoice, defaultChoice)

					choice := strconv.Itoa(defaultChoice)
					if scanner.Scan() {
						if t := scanner.Text(); t != "" {
							choice = t
						}
					}

					choiceIndex, err := strconv.Atoi(strings.TrimSpace(choice))
					if err != nil || choiceIndex < 1 || choiceIndex > skipChoice {
						choiceIndex = defaultChoice
					}
					if choiceIndex == skipChoice {
						provider = ""
					} else {
						provider = providerChoices[choiceIndex-1].Name
					}

					if provider != "" && apiKey == "" {
						fmt.Printf("\n%s API key: ", quickstartProviderDisplayName(provider))
						if keyBytes, err := readPassword(); err == nil {
							apiKey = strings.TrimSpace(string(keyBytes))
							fmt.Println() // newline after masked input
						}
					}
				}

				return runSetup(provider, apiKey, notifyURL, backendName, backendCfg, noInfra, true, noBrowser, configurePool)
			}

			// Default: web-assisted setup — no prompts.
			// Pass through --provider/--api-key if given (supports non-interactive use).
			return runSetup(provider, apiKey, notifyURL, backendName, backendCfg, noInfra, false, noBrowser, configurePool)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Agent name (quick setup)")
	cmd.Flags().StringVar(&preset, "preset", "", "Agent preset (quick setup)")
	cmd.Flags().StringVar(&provider, "provider", "", "LLM provider adapter name")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "LLM provider API key")
	cmd.Flags().StringVar(&notifyURL, "notify-url", "", "Notification URL (ntfy or webhook) for operator alerts")
	cmd.Flags().StringVar(&backend, "backend", "", "Runtime backend to use; defaults to firecracker on Linux/WSL and apple-vf-microvm on macOS. Also respected via AGENCY_RUNTIME_BACKEND. Docker, Podman, containerd, and apple-container require --experimental-backend.")
	cmd.Flags().BoolVar(&experimentalBackend, "experimental-backend", false, "Allow transitional or non-default runtime backends")
	cmd.Flags().BoolVar(&configurePool, "configure-network-pool", false, "Configure Docker default-address-pools before infrastructure startup (docker backend only)")
	cmd.Flags().BoolVar(&noInfra, "no-infra", false, "Skip the container-backend check and infrastructure startup")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't open the web UI in a browser (also respected via AGENCY_NO_BROWSER=1)")
	cmd.Flags().BoolVar(&noDockerStart, "no-docker-start", false, "Don't try to start Docker Desktop automatically (docker backend only; also respected via AGENCY_NO_DOCKER_START=1)")
	cmd.Flags().BoolVar(&cliMode, "cli", false, "Run full interactive setup in the terminal")

	return cmd
}

func daemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the gateway daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			port := gatewayPortFromConfig()
			if !daemon.IsRunning(port) {
				fmt.Println("Daemon is not running.")
				return nil
			}
			if err := daemon.Stop(); err != nil {
				return err
			}
			for i := 0; i < 20; i++ {
				time.Sleep(250 * time.Millisecond)
				if !daemon.IsRunning(port) {
					fmt.Println("Daemon stopped.")
					return nil
				}
			}
			return fmt.Errorf("daemon stop requested, but gateway is still responding on port %d", port)
		},
	}
}

func daemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the gateway daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			port := gatewayPortFromConfig()
			if daemon.IsRunning(port) {
				fmt.Println("Stopping daemon...")
				if err := daemon.Stop(); err != nil {
					// Stop may fail if the PID file is missing (e.g., after
					// a prior "serve stop" already removed it). As long as
					// the daemon actually shuts down, this is non-fatal.
					fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				}
				// Wait for process to exit
				for i := 0; i < 20; i++ {
					time.Sleep(250 * time.Millisecond)
					if !daemon.IsRunning(port) {
						break
					}
				}
			}
			fmt.Println("Starting daemon...")
			if err := daemon.Start(port); err != nil {
				return fmt.Errorf("start: %w", err)
			}
			fmt.Println("Daemon restarted.")
			return nil
		},
	}
}

func daemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if the gateway daemon is running",
		Run: func(cmd *cobra.Command, args []string) {
			if daemon.IsRunning(gatewayPortFromConfig()) {
				fmt.Println("Daemon is running.")
			} else {
				fmt.Println("Daemon is not running.")
			}
		},
	}
}

// openBrowser attempts to open url in the system default browser.
// Best-effort — callers should ignore errors.
// Suppressed when AGENCY_NO_BROWSER=1 env var is set.
func openBrowser(url string) error {
	if os.Getenv("AGENCY_NO_BROWSER") != "" {
		return nil
	}
	var cmd string
	var args []string

	switch {
	case runtime.GOOS == "darwin":
		cmd = "open"
		args = []string{url}
	case isWSL():
		if _, err := exec.LookPath("wslview"); err == nil {
			cmd = "wslview"
			args = []string{url}
		} else {
			cmd = "cmd.exe"
			args = []string{"/c", "start", url}
		}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "microsoft") || strings.Contains(s, "WSL")
}

// webHost derives the local web UI hostname. The web container publishes its
// host port on loopback, even when the gateway daemon listens on a backend
// bridge address for VM-backed runtimes.
func webHost() string {
	cfg := config.Load()
	if host, _, err := net.SplitHostPort(cfg.GatewayAddr); err == nil && host != "" {
		if host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1" {
			return host
		}
	}
	return "localhost"
}

func localWebURLForHost(host string) string {
	if strings.TrimSpace(host) == "" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:8280", host)
}

func dockerAutoStartDisabled() bool {
	return os.Getenv("AGENCY_NO_DOCKER_START") != ""
}

func tryStartDockerDesktop(wsl bool) bool {
	switch {
	case runtime.GOOS == "darwin":
		return exec.Command("open", "-a", "Docker").Start() == nil
	case runtime.GOOS == "windows":
		return exec.Command("cmd", "/c", "start", "", "Docker Desktop").Start() == nil
	case wsl:
		return exec.Command("cmd.exe", "/c", "start", "", "Docker Desktop").Start() == nil
	default:
		return false
	}
}

func defaultRuntimeBackendForHost() string {
	switch runtime.GOOS {
	case "darwin":
		return hostruntimebackend.BackendAppleVFMicroVM
	case "linux":
		return hostruntimebackend.BackendFirecracker
	default:
		return hostruntimebackend.BackendFirecracker
	}
}

func isStrategicRuntimeBackendForHost(backend string) bool {
	return backend == defaultRuntimeBackendForHost()
}

func normalizeRuntimeBackendName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return defaultRuntimeBackendForHost()
	}
	if name == hostruntimebackend.BackendFirecracker || name == hostruntimebackend.BackendAppleVFMicroVM {
		return name
	}
	return runtimehost.NormalizeContainerBackend(name)
}

func selectRuntimeBackend(override string, allowExperimental bool) (string, map[string]string, error) {
	override = strings.TrimSpace(strings.ToLower(override))
	if override == "" {
		override = strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_RUNTIME_BACKEND")))
	}
	if override == "" {
		override = strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_CONTAINER_BACKEND")))
	}
	// Honor an existing choice persisted in config.yaml so re-running setup
	// without a flag doesn't silently flip the backend when multiple are
	// installed. Flag and env still win over the persisted value.
	var configuredCfg map[string]string
	if override == "" {
		cfg := config.Load()
		if existing := strings.TrimSpace(cfg.Hub.DeploymentBackend); existing != "" {
			override = existing
			configuredCfg = cfg.Hub.DeploymentBackendConfig
		}
	}

	if override == "" {
		backend := defaultRuntimeBackendForHost()
		fmt.Fprintf(os.Stderr, "Using %s runtime backend.\n", backend)
		return backend, nil, nil
	}
	if override == hostruntimebackend.BackendFirecracker || override == hostruntimebackend.BackendAppleVFMicroVM {
		if !isStrategicRuntimeBackendForHost(override) && !allowExperimental {
			return "", nil, fmt.Errorf("runtime backend %q is not the default for this host; re-run with --experimental-backend to use it", override)
		}
		fmt.Fprintf(os.Stderr, "Using %s runtime backend.\n", override)
		return override, mergeBackendSocketConfig(configuredCfg, nil), nil
	}
	if !allowExperimental {
		return "", nil, fmt.Errorf("runtime backend %q is transitional; re-run with --experimental-backend to use it", override)
	}
	return selectContainerBackend(override, configuredCfg)
}

func selectContainerBackend(override string, configuredCfg map[string]string) (string, map[string]string, error) {
	if override != "" {
		var match *runtimehost.BackendProbe
		for _, p := range runtimehost.KnownBackends() {
			if p.Name == override {
				pp := p
				match = &pp
				break
			}
		}
		if match == nil {
			return "", nil, fmt.Errorf("unknown runtime backend %q", override)
		}
		d := runtimehost.ProbeBackend(*match)
		if !d.Reachable {
			fmt.Fprintf(os.Stderr, "Requested backend %q is not reachable: %v\n", override, d.Err)
			fmt.Fprintln(os.Stderr, "")
			if !d.CLIFound {
				fmt.Fprintln(os.Stderr, runtimehost.InstallHint())
				fmt.Fprintln(os.Stderr, "")
			}
			fmt.Fprintln(os.Stderr, "Then re-run: agency setup")
			return "", nil, fmt.Errorf("container backend %q not available", override)
		}
		fmt.Fprintf(os.Stderr, "Using %s backend (%s).\n", d.Name(), backendModeDescription(d))
		return d.Name(), withAppleContainerHelperConfig(d.Name(), mergeBackendSocketConfig(configuredCfg, d.Config)), nil
	}

	detections := runtimehost.ProbeAllBackends()
	reachable := runtimehost.SelectReachable(detections)

	switch len(reachable) {
	case 0:
		// No backend present. Offer an install when we have a TTY and the
		// user hasn't opted out of interactive prompts. If that produces a
		// working backend, use it directly. Otherwise print the hint and
		// exit so the caller can re-run after a manual install.
		if interactiveInstallEnabled() {
			if d := offerBackendInstall(); d != nil {
				fmt.Fprintf(os.Stderr, "Using %s backend (%s).\n", d.Name(), backendModeDescription(*d))
				return d.Name(), withAppleContainerHelperConfig(d.Name(), d.Config), nil
			}
		} else {
			fmt.Fprintln(os.Stderr, "No container backend detected on this host.")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, runtimehost.InstallHint())
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Then re-run: agency setup")
		}
		return "", nil, fmt.Errorf("no container backend available")
	case 1:
		d := reachable[0]
		fmt.Fprintf(os.Stderr, "Using %s backend (%s).\n", d.Name(), backendModeDescription(d))
		return d.Name(), withAppleContainerHelperConfig(d.Name(), d.Config), nil
	default:
		// With a TTY available, ask the user to pick. Default is podman
		// (first in reachable — preference order comes from KnownBackends).
		// Non-interactive callers get the default silently so MCP servers
		// and scripted installs remain deterministic.
		if interactiveInstallEnabled() {
			if pick := promptPickBackend(reachable); pick != nil {
				fmt.Fprintf(os.Stderr, "Using %s backend (%s).\n", pick.Name(), backendModeDescription(*pick))
				return pick.Name(), pick.Config, nil
			}
		}
		chosen := reachable[0]
		others := make([]string, 0, len(reachable)-1)
		for _, d := range reachable[1:] {
			others = append(others, d.Name())
		}
		fmt.Fprintf(os.Stderr,
			"Multiple container backends detected: %s, %s.\n",
			chosen.Name(), strings.Join(others, ", "),
		)
		fmt.Fprintf(os.Stderr,
			"Using %s (%s). To pick a different one, re-run with --backend %s --experimental-backend.\n",
			chosen.Name(), backendModeDescription(chosen), others[0],
		)
		return chosen.Name(), withAppleContainerHelperConfig(chosen.Name(), chosen.Config), nil
	}
}

func withAppleContainerHelperConfig(backend string, cfg map[string]string) map[string]string {
	if runtimehost.NormalizeContainerBackend(backend) != runtimehost.BackendAppleContainer {
		return cfg
	}
	helper := strings.TrimSpace(os.Getenv("AGENCY_APPLE_CONTAINER_HELPER_BIN"))
	waitHelper := strings.TrimSpace(os.Getenv("AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN"))
	if helper == "" && waitHelper == "" {
		return cfg
	}
	out := make(map[string]string, len(cfg)+2)
	for k, v := range cfg {
		out[k] = v
	}
	if helper != "" {
		out["helper_binary"] = helper
	}
	if waitHelper != "" {
		out["wait_helper_binary"] = waitHelper
	}
	return out
}

// backendModeDescription returns a short human-readable descriptor for the
// detection — "rootless"/"rootful" for podman/containerd, or "available"
// as a fallback when no mode is exposed by the backend.
func backendModeDescription(d runtimehost.BackendDetection) string {
	if d.Mode != "" {
		return d.Mode
	}
	return "available"
}

// mergeBackendSocketConfig preserves any socket keys from an existing config
// (e.g. a custom socket URI set by the operator) while filling in values
// the probe freshly resolved on this host.
func mergeBackendSocketConfig(existing, fresh map[string]string) map[string]string {
	if len(existing) == 0 {
		if len(fresh) == 0 {
			return nil
		}
		out := make(map[string]string, len(fresh))
		for k, v := range fresh {
			out[k] = v
		}
		return out
	}
	out := make(map[string]string, len(existing)+len(fresh))
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range fresh {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

// persistPendingKeysToEnv writes provider API keys to ~/.agency/.env so
// they survive a daemon startup failure. The envfile.Upsert primitive is
// idempotent — callers may run it repeatedly with the same entries safely.
//
// Keys land in the credential store on the next successful setup/serve
// via the ReadExistingKeys migration path.
func persistPendingKeysToEnv(agencyHome string, keys []config.KeyEntry) error {
	if len(keys) == 0 {
		return nil
	}
	entries := make(map[string]string, len(keys))
	for _, k := range keys {
		if k.EnvVar == "" || k.Key == "" {
			continue
		}
		entries[k.EnvVar] = k.Key
	}
	if len(entries) == 0 {
		return nil
	}
	return envfile.Upsert(filepath.Join(agencyHome, ".env"), entries)
}

func runSetup(provider, apiKey, notifyURL, backend string, backendCfg map[string]string, noInfra, cliMode, noBrowser, configurePool bool) error {
	provider = normalizeProvider(provider)
	gatewayAddr := ""
	if runtimehost.NormalizeContainerBackend(backend) == runtimehost.BackendAppleContainer {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		addr, err := appleContainerGatewayListenAddr(ctx, backendCfg)
		if err != nil {
			return fmt.Errorf("prepare apple-container gateway address: %w", err)
		}
		gatewayAddr = addr
	}
	pendingKeys, err := config.RunInit(config.InitOptions{
		Provider:                provider,
		APIKey:                  apiKey,
		NotifyURL:               notifyURL,
		GatewayAddr:             gatewayAddr,
		DeploymentBackend:       backend,
		DeploymentBackendConfig: backendCfg,
	})
	if err != nil {
		return err
	}

	cfg := config.Load()
	agencyHome := cfg.Home
	fmt.Println("Agency platform initialized at", agencyHome)
	fmt.Println()

	// Persist API keys to ~/.agency/.env BEFORE starting the daemon. If the
	// daemon fails to start (e.g. container backend unreachable mid-setup),
	// the keys survive in .env and the next setup/serve will migrate them
	// into the credential store via the ReadExistingKeys path further down.
	// Without this, a daemon startup failure silently drops the API key the
	// user just typed.
	if len(pendingKeys) > 0 {
		if err := persistPendingKeysToEnv(agencyHome, pendingKeys); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not persist API key to %s/.env: %v\n", agencyHome, err)
		}
	}

	// Start the daemon
	fmt.Println("Starting daemon...")
	if err := daemon.Start(gatewayPortFromConfig()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: daemon did not start: %v\n", err)
		fmt.Println()
		if len(pendingKeys) > 0 {
			fmt.Fprintf(os.Stderr, "Your API key(s) were saved to %s/.env and will be migrated to the secure credential store on the next successful setup.\n", agencyHome)
			fmt.Println()
		}
		fmt.Println("Next steps:")
		fmt.Println("  agency serve     # Start the daemon manually")
		return nil
	}

	fmt.Println("Daemon started successfully.")

	// Store LLM credentials in the encrypted credential store
	if len(pendingKeys) > 0 {
		c := apiclient.NewClient("http://" + cfg.GatewayAddr)
		for _, key := range pendingKeys {
			fmt.Printf("  Storing %s credential...\n", key.Provider)
			body := map[string]interface{}{
				"name":     key.EnvVar,
				"value":    key.Key,
				"kind":     "provider",
				"scope":    "platform",
				"protocol": "api-key",
			}
			if domains := config.ProviderDomains(key.Provider); len(domains) > 0 {
				body["protocol_config"] = map[string]interface{}{
					"domains": domains,
				}
			}
			_, err := c.CredentialSet(body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to store %s credential: %v\n", key.Provider, err)
				fmt.Fprintf(os.Stderr, "  Run manually: agency creds set %s <key> --kind provider --scope platform --protocol api-key\n", key.EnvVar)
			}
		}
	}

	// Migrate existing .env API keys to credential store (one-time)
	if len(pendingKeys) == 0 {
		existingProviders := config.ReadExistingKeys(agencyHome)
		if len(existingProviders) > 0 {
			envVars := envfile.Load(filepath.Join(agencyHome, ".env"))
			cfg := config.Load()
			c := apiclient.NewClient("http://" + cfg.GatewayAddr)
			for _, provider := range existingProviders {
				envVar := config.ProviderEnvVar(provider)
				if val, ok := envVars[envVar]; ok && val != "" {
					// Check if already in credential store
					existing, _ := c.CredentialShow(envVar, false)
					if existing == nil || existing["name"] == nil {
						fmt.Printf("  Migrating %s credential to secure store...\n", provider)
						body := map[string]interface{}{
							"name":     envVar,
							"value":    val,
							"kind":     "provider",
							"scope":    "platform",
							"protocol": "api-key",
						}
						if domains := config.ProviderDomains(provider); len(domains) > 0 {
							body["protocol_config"] = map[string]interface{}{
								"domains": domains,
							}
						}
						_, err := c.CredentialSet(body)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to migrate %s: %v\n", provider, err)
						}
					}
				}
			}
		}
	}

	hasEmbeddings := false
	embedProvider := os.Getenv("KNOWLEDGE_EMBED_PROVIDER")
	if embedProvider == "ollama" {
		hasEmbeddings = true
	}

	capacityBackend, capacityBackendCfg := backend, backendCfg
	if capacityBackend == "" {
		cfg := config.Load()
		capacityBackend = cfg.Hub.DeploymentBackend
		capacityBackendCfg = cfg.Hub.DeploymentBackendConfig
	}
	capCfg, capErr := orchestrate.ProfileHostForRuntime(hasEmbeddings, capacityBackend, capacityBackendCfg)
	if capErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not profile host: %v\n", capErr)
	}

	if capErr == nil {
		// The network pool tuning reads /etc/docker/daemon.json and is
		// meaningful only for the docker engine. Podman and containerd
		// manage their own network pools through different mechanisms,
		// so skip the reconcile and its accompanying stdout lines on
		// non-docker backends.
		if runtimehost.NormalizeContainerBackend(backend) == runtimehost.BackendDocker {
			fmt.Println()
			poolConfigured, poolPath, err := reconcileDockerNetworkPool(configurePool)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not inspect Docker network pool: %v\n", err)
			} else {
				capCfg.NetworkPoolConfigured = poolConfigured
				if poolConfigured {
					fmt.Printf("Docker network pool configured (%s)\n", poolPath)
				} else if poolPath != "" {
					fmt.Printf("Docker network pool uses defaults (%s)\n", poolPath)
				}
			}
		} else if configurePool {
			fmt.Fprintf(os.Stderr, "Warning: --configure-network-pool is docker-specific; ignored on %s backend.\n", backend)
		}
	}

	// Start infrastructure unless --no-infra was passed
	if !noInfra {
		fmt.Println()
		fmt.Println("Starting infrastructure...")
		cfg := config.Load()
		c := apiclient.NewClient("http://" + cfg.GatewayAddr)
		if err := c.InfraUpStream(func(component, status string) {
			fmt.Printf("  ✓ %s\n", component)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: infrastructure did not start: %v\n", err)
			fmt.Println("  Run 'agency infra up' to start manually.")
		} else {
			fmt.Println("Infrastructure running.")
		}
	}

	// Profile host capacity
	fmt.Println()
	fmt.Println("Profiling host capacity...")

	if capErr == nil {
		capPath := filepath.Join(agencyHome, "capacity.yaml")
		if err := orchestrate.SaveCapacity(capPath, capCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save capacity config: %v\n", err)
		} else {
			fmt.Println()
			fmt.Println("Host capacity profile:")
			fmt.Printf("  Memory: %d GB total, %.1f GB system reserve, %.1f GB infrastructure\n",
				capCfg.HostMemoryMB/1024,
				float64(capCfg.SystemReserveMB)/1024.0,
				float64(capCfg.InfraOverheadMB)/1024.0)
			fmt.Printf("  CPU: %d cores (2 reserved for system)\n", capCfg.HostCPUCores)
			if capCfg.RuntimeBackend != "" {
				if capCfg.EnforcementMode != "" {
					fmt.Printf("  Runtime: %s (%s enforcer)\n", capCfg.RuntimeBackend, capCfg.EnforcementMode)
				} else {
					fmt.Printf("  Runtime: %s\n", capCfg.RuntimeBackend)
				}
			}
			fmt.Printf("  Agent capacity: %d concurrent (%d MB each)\n",
				capCfg.MaxAgents, capCfg.AgentSlotMB)
			fmt.Printf("  Meeseeks: share the same pool (%d MB each)\n",
				capCfg.MeeseeksSlotMB)
			fmt.Println()
			fmt.Printf("Written to %s — edit to adjust limits.\n", capPath)
		}
	}

	fmt.Println()
	if cliMode {
		fmt.Println("You're ready to go:")
		fmt.Println()
		fmt.Println("  agency create my-agent  # Create an agent")
		fmt.Println("  agency start my-agent   # Start an agent")
		fmt.Println("  agency status           # Check platform status")
		fmt.Println()
		fmt.Printf("  Open %s for the web UI\n", localWebURLForHost(webHost()))
	} else {
		setupURL := localWebURLForHost(webHost()) + "/setup"
		if !noBrowser {
			_ = openBrowser(setupURL)
		}
		fmt.Printf("Finish setup at: %s\n", setupURL)
	}

	return nil
}

func appleContainerGatewayListenAddr(ctx context.Context, backendCfg map[string]string) (string, error) {
	cli, err := runtimehost.NewRawClientForBackend(runtimehost.BackendAppleContainer, backendCfg)
	if err != nil {
		return "", err
	}
	netName := runtimehost.GatewayNetName()
	inspect, err := cli.NetworkInspect(ctx, netName, containerops.InspectOptions{})
	if err != nil {
		if !containerops.IsNetworkNotFound(err) {
			return "", fmt.Errorf("inspect %s: %w", netName, err)
		}
		labels := map[string]string{
			"agency.role":      "infra",
			"agency.component": netName,
			"agency.instance":  runtimehost.InfraInstanceName(),
		}
		if createErr := containerops.CreateMediationNetwork(ctx, cli, netName, labels); createErr != nil && !containerops.IsNetworkAlreadyExists(createErr) {
			return "", fmt.Errorf("create %s: %w", netName, createErr)
		}
		inspect, err = cli.NetworkInspect(ctx, netName, containerops.InspectOptions{})
		if err != nil {
			return "", fmt.Errorf("verify %s: %w", netName, err)
		}
	}
	for _, cfg := range inspect.IPAM.Config {
		if gateway := strings.TrimSpace(cfg.Gateway); gateway != "" {
			// Apple containers reach the host through the inspected gateway IP,
			// but the host daemon must bind an address present on the host.
			// Infra advertises the gateway IP separately via host aliases.
			return "0.0.0.0:8200", nil
		}
	}
	return "", fmt.Errorf("network %s has no gateway address", netName)
}

func runServe(httpAddr string) error {
	cfg := config.Load()
	if buildID == "" || buildID == "unknown" {
		if derived := deriveLocalBuildID(cfg.SourceDir, sourceDir); derived != "" {
			buildID = derived
		}
	}

	logger := agencylog.New("gateway", buildID)
	slog.SetDefault(logger)

	cfg.Version = version
	cfg.BuildID = buildID
	// Dev build: use source_dir stamped at build time if config doesn't override
	if cfg.SourceDir == "" && sourceDir != "" {
		cfg.SourceDir = sourceDir
	}
	logger.Info("agency home", "path", cfg.Home)
	backendName := normalizeRuntimeBackendName(cfg.Hub.DeploymentBackend)
	backendEndpoint := runtimehost.ResolvedBackendEndpoint(backendName, cfg.Hub.DeploymentBackendConfig)
	backendMode := runtimehost.ResolvedBackendMode(backendName, cfg.Hub.DeploymentBackendConfig)
	if err := validateConfiguredBackend(cfg); err != nil {
		return err
	}

	// Ensure audit directory has correct permissions (0700) — retroactively fix
	// dirs created before this hardening was in place.
	auditDir := filepath.Join(cfg.Home, "audit")
	if info, err := os.Stat(auditDir); err == nil && info.IsDir() {
		os.Chmod(auditDir, 0700)
	}

	// Write PID file
	pidFile := daemon.PIDFile()
	if pidFile != "" {
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
			logger.Warn("could not write PID file", "err", err)
		}
	}

	// Runtime container backend client. When a container runtime backend is
	// configured, an unreachable client is fatal downstream in api.Startup.
	var dc *runtimehost.Client
	if runtimehost.IsContainerBackend(backendName) {
		dc = runtimehost.TryNewClientForBackend(backendName, cfg.Hub.DeploymentBackendConfig, logger)
	}
	infraBackendName := ""
	infraBackendConfig := cfg.Hub.DeploymentBackendConfig
	var infraDC *runtimehost.Client
	if runtimehost.IsContainerBackend(backendName) {
		infraBackendName = backendName
		infraDC = dc
	}
	if dc != nil {
		if backendMode != "" {
			logger.Info("container backend connected", "backend", backendName, "endpoint", backendEndpoint, "mode", backendMode)
		} else {
			logger.Info("container backend connected", "backend", backendName, "endpoint", backendEndpoint)
		}
	} else if runtimehost.IsContainerBackend(backendName) {
		if backendMode != "" {
			logger.Warn("container backend unavailable", "backend", backendName, "endpoint", backendEndpoint, "mode", backendMode)
		} else {
			logger.Warn("container backend unavailable", "backend", backendName, "endpoint", backendEndpoint)
		}
	} else {
		logger.Info("gateway starting without a container backend client", "backend", backendName)
	}
	if infraDC != nil && infraDC != dc {
		infraEndpoint := runtimehost.ResolvedBackendEndpoint(infraBackendName, infraBackendConfig)
		infraMode := runtimehost.ResolvedBackendMode(infraBackendName, infraBackendConfig)
		if infraMode != "" {
			logger.Info("infra container backend connected", "backend", infraBackendName, "endpoint", infraEndpoint, "mode", infraMode)
		} else {
			logger.Info("infra container backend connected", "backend", infraBackendName, "endpoint", infraEndpoint)
		}
	}
	var backendHealthStatus *runtimehost.Status
	if infraDC != nil {
		backendHealthStatus = runtimehost.NewStatus(infraDC)
	}

	// Startup reconciliation — clean up orphaned containers/networks from
	// previous gateway runs. Runs before the HTTP server starts; errors are
	// logged but never fatal (reconcile is best-effort).
	if dc != nil {
		knownAgents := listAgentNames(cfg.Home)
		reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 30*time.Second)
		orchestrate.Reconcile(reconcileCtx, dc.RawClient(), knownAgents, logger)
		reconcileCancel()
	}

	// WebSocket hub
	wsHub := ws.NewHub(logger)
	ws.StartCommsBridge(wsHub, logger)

	// Event bus
	audit := logs.NewWriter(cfg.Home)
	auditFn := func(eventType string, data map[string]interface{}) {
		audit.Write("_system", eventType, data)
	}
	eventBus := events.NewBus(logger, auditFn)

	// Register delivery handlers
	commsPort := os.Getenv("AGENCY_GATEWAY_PROXY_PORT")
	if commsPort == "" {
		commsPort = "8202"
	}
	agentDelivery := events.NewAgentDelivery("http://localhost:" + commsPort)
	outboundDelivery := events.NewOutboundDelivery()
	ntfyDelivery := events.NewNtfyDelivery()
	eventBus.RegisterDelivery(events.DestAgent, agentDelivery.Deliver)
	eventBus.RegisterDelivery(events.DestWebhook, outboundDelivery.Deliver)
	eventBus.RegisterDelivery(events.DestNtfy, ntfyDelivery.Deliver)

	// Scheduler
	scheduler := events.NewScheduler(eventBus)
	scheduler.Start()
	defer scheduler.Stop()

	// Webhook manager
	webhookMgr := events.NewWebhookManager(cfg.Home)

	// Notification store — file-backed persistence for notification destinations
	notifStore := events.NewNotificationStore(cfg.Home)

	// Load notification subscriptions from store (with migration from config.yaml)
	notifConfigs, _ := notifStore.Load()
	if len(notifConfigs) == 0 && len(cfg.Notifications) > 0 {
		// Migration: copy from config.yaml to notifications.yaml
		for _, nc := range cfg.Notifications {
			notifStore.Add(nc) //nolint:errcheck
		}
		notifConfigs = notifStore.List()
		logger.Info("migrated notification configs from config.yaml to notifications.yaml", "count", len(notifConfigs))
	}
	notifSubs := events.BuildNotificationSubscriptions(notifConfigs)
	for _, sub := range notifSubs {
		eventBus.Subscriptions().Add(sub)
	}

	// Wire channel events from the comms bridge to the event bus.
	wsHub.SetEventPublisher(func(channel, messageID, content, author string) {
		metadata := map[string]interface{}{"author": author, "channel": channel}
		if strings.HasPrefix(channel, models.DMChannelPrefix) {
			target := strings.TrimPrefix(channel, models.DMChannelPrefix)
			metadata["channel_type"] = "direct"
			metadata["dm_target"] = target
		}
		event := models.NewChannelEvent(channel, messageID,
			map[string]interface{}{"content": content},
			metadata,
		)
		eventBus.Publish(event)
	})

	// Wire operator-alertable agent signals to event bus as platform events.
	// Signals like "error" (budget/enforcer), "escalation" (XPIA), "self_halt"
	// become operator_alert events, routed to ntfy/webhook via subscriptions.
	wsHub.SetAgentSignalPublisher(func(agent, signalType string, data map[string]interface{}) {
		if data == nil {
			data = make(map[string]interface{})
		}
		data["signal_type"] = signalType
		events.EmitAgentEvent(eventBus, "operator_alert", agent, data)
	})

	// Rebuild subscriptions from active missions
	missionMgr := orchestrate.NewMissionManager(cfg.Home)
	missions, _ := missionMgr.List()
	for _, m := range missions {
		if m.Status == "active" {
			events.OnMissionAssigned(eventBus, m)
			for _, t := range m.Triggers {
				if t.Source == "schedule" && t.Cron != "" {
					scheduler.Register(t.Name, t.Cron, "") //nolint:errcheck
				}
			}
		}
	}

	// Mission health monitor — checks active missions every 60 seconds.
	// Alerts are emitted as platform events (mission_health_alert) which
	// flow through the event bus to any configured notification subscribers.
	healthCtx, healthCancel := context.WithCancel(context.Background())
	defer healthCancel()
	var healthMgr *orchestrate.MissionHealthMonitor

	// Shared suppression tracker — marks agents undergoing intentional
	// stop/restart so runtime watchers don't fire spurious alerts.
	stopSuppress := orchestrate.NewStopSuppression(30 * time.Second)

	// Enforcer watcher — listens to host-backend events for enforcer runtime
	// exits. Fires a platform event so the operator knows an agent has lost
	// API mediation (ASK Tenet 3: mediation is complete).
	if dc != nil {
		enforcerWatcher, enfWatchErr := orchestrate.NewEnforcerWatcherWithClient(
			func(agentName, reason string) {
				events.EmitAgentEvent(eventBus, "enforcer_exited", agentName, map[string]interface{}{
					"reason": reason,
				})
			},
			logger,
			stopSuppress,
			dc,
		)
		if enfWatchErr != nil {
			logger.Warn("enforcer watcher unavailable", "error", enfWatchErr)
		} else {
			enforcerWatcher.Start(healthCtx)
		}
	}

	// Workspace watcher — listens for workspace runtime crashes and
	// auto-restarts through host-backend events. Emits platform events for operator alerting.
	// Comms and knowledge now route through the enforcer mediation proxy,
	// so no infra reconnect is needed on restart.
	if dc != nil {
		workspaceWatcher, wsWatchErr := orchestrate.NewWorkspaceWatcherWithClient(
			func(agentName, reason string) {
				events.EmitAgentEvent(eventBus, "workspace_crashed", agentName, map[string]interface{}{
					"reason": reason,
				})
			},
			logger,
			stopSuppress,
			dc,
		)
		if wsWatchErr != nil {
			logger.Warn("workspace watcher unavailable", "error", wsWatchErr)
		} else {
			workspaceWatcher.Start(healthCtx)
		}
	}

	// Audit summarizer — aggregates enforcer JSONL logs into per-mission metrics.
	// Runs every 15 minutes; also available on-demand via POST /api/v1/admin/audit/summarize.
	knowledgeURL := "http://localhost:8201"
	auditSummarizer := auditpkg.NewAuditSummarizer(cfg.Home, knowledgeURL, logger)
	auditSummarizer.Start(healthCtx)

	// Initialize all gateway components via Startup.
	startup, err := api.StartupWithInfraClient(cfg, dc, infraDC, logger)
	if err != nil {
		logger.Error("gateway startup failed", "err", err)
		os.Exit(1)
	}
	if startup.InstanceStore != nil {
		runtimeDelivery := events.NewRuntimeDelivery(startup.InstanceStore)
		eventBus.RegisterDelivery(events.DestRuntime, runtimeDelivery.Deliver)
	}
	if dc != nil {
		var healthErr error
		healthMgr, healthErr = orchestrate.NewMissionHealthMonitorWithRuntime(
			missionMgr,
			startup.Runtime,
			func(missionName, reason string) {
				events.EmitMissionEvent(eventBus, "mission_health_alert", missionName, map[string]interface{}{
					"reason": reason,
				})
			},
			func(name, reason string) error {
				return missionMgr.Pause(name, reason)
			},
			logger,
			dc,
		)
		if healthErr != nil {
			logger.Warn("mission health monitor unavailable", "error", healthErr)
		} else {
			healthMgr.Start(healthCtx)
		}
	}

	// Principal registry — shared instance for auth + permission middleware.
	var reg *registry.Registry
	if regDB, regErr := registry.Open(filepath.Join(cfg.Home, "registry.db")); regErr == nil {
		reg = regDB
		if cfg.Token != "" {
			reg.SetGatewayToken(cfg.Token)
		}
		defer reg.Close()
	} else {
		logger.Warn("principal registry unavailable — permission enforcement disabled", "error", regErr)
	}

	// Wire the WebSocket hub's scope filter: registry resolves each client's
	// authorization scope at connect time; the audit writer records subscribe
	// attempts that exceed scope. Both are optional — when nil, the hub
	// defaults to allow-all (backward compatibility).
	wsHub.SetRegistry(reg)
	wsHub.SetAuditor(audit)

	// Routing optimizer — tracks LLM call patterns and generates
	// cost-saving suggestions. Persisted to ~/.agency/routing-stats.json.
	optimizer := routing.NewOptimizer(
		filepath.Join(cfg.Home, "routing-stats.json"),
		routing.WithLocalYAMLPath(filepath.Join(cfg.Home, "infrastructure", "routing.local.yaml")),
	)
	if err := optimizer.Load(); err != nil {
		logger.Warn("routing optimizer: failed to load persisted state", "err", err)
	}

	// Background goroutine: compute stats, generate suggestions, and persist
	// every 60 minutes. Also saves on shutdown.
	go func() {
		ticker := time.NewTicker(60 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				optimizer.ComputeStats()
				optimizer.GenerateSuggestions()
				if err := optimizer.Save(); err != nil {
					logger.Warn("routing optimizer: save failed", "err", err)
				}
			case <-healthCtx.Done():
				if err := optimizer.Save(); err != nil {
					logger.Warn("routing optimizer: shutdown save failed", "err", err)
				}
				return
			}
		}
	}()

	// REST API
	r := chi.NewRouter()
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.RealIP)
	r.Use(api.CorrelationID)
	r.Use(corsMiddleware)
	r.Use(api.BearerAuth(cfg.Token, cfg.EgressToken, reg))
	routeOpts := api.RouteOptions{
		Hub:             wsHub,
		EventBus:        eventBus,
		Scheduler:       scheduler,
		WebhookMgr:      webhookMgr,
		NotifStore:      notifStore,
		StopSuppress:    stopSuppress,
		AuditSummarizer: auditSummarizer,
		Registry:        reg,
		Optimizer:       optimizer,
	}
	if healthMgr != nil {
		routeOpts.HealthMonitor = healthMgr
	}
	routeOpts.BackendHealth = backendHealthStatus
	api.RegisterAll(r, cfg, dc, logger, startup, routeOpts)

	// Wire auto-restore: when the container backend reconnects, automatically bring up infra.
	if cfg.AutoRestoreInfra && infraBackendName != "" && backendHealthStatus != nil {
		backendHealthStatus.OnReconnect = func() {
			logger.Info("container backend reconnected — auto-restoring infrastructure", "backend", infraBackendName)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				newDC := runtimehost.TryNewClientForBackend(infraBackendName, infraBackendConfig, logger)
				if newDC == nil {
					logger.Warn("auto-restore: container backend reconnect detected but client creation failed", "backend", infraBackendName)
					return
				}
				infra, err := orchestrate.NewInfra(cfg.Home, cfg.Version, newDC, logger, cfg.HMACKey)
				if err != nil {
					logger.Warn("auto-restore: failed to create infra manager", "err", err)
					return
				}
				infra.SourceDir = cfg.SourceDir
				infra.BuildID = cfg.BuildID
				infra.GatewayAddr = cfg.GatewayAddr
				infra.GatewayToken = cfg.Token
				infra.EgressToken = cfg.EgressToken
				infra.RuntimeBackendName = backendName
				if err := infra.EnsureRunning(ctx); err != nil {
					logger.Warn("auto-restore: infra up failed", "err", err)
				} else {
					logger.Info("auto-restore: infrastructure restored")
				}
			}()
		}
	}

	httpServer := &http.Server{
		Addr:         httpAddr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	// Start servers
	if !strings.HasPrefix(httpAddr, "127.0.0.1") && !strings.HasPrefix(httpAddr, "localhost") {
		logger.Warn("gateway listening on non-localhost address — ensure network access is restricted", "addr", httpAddr)
	}
	errCh := make(chan error, 1)

	go func() {
		logger.Info("HTTP API listening", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()

	// Unix socket listener for container-to-gateway communication.
	// Socket lives in ~/.agency/run/ so infra containers can mount the
	// directory (not the file) and survive gateway restarts.
	sockDir := filepath.Join(cfg.Home, "run")
	os.MkdirAll(sockDir, 0755)
	// Proxy-safe socket — bridged to TCP by the gateway-proxy container.
	// Does NOT include credential resolution endpoints.
	sockPath := filepath.Join(sockDir, "gateway.sock")
	os.Remove(sockPath)                                // clean up stale socket
	os.Remove(filepath.Join(cfg.Home, "gateway.sock")) // clean up legacy location
	unixListener, err := net.Listen("unix", sockPath)
	if err != nil {
		logger.Warn("could not create Unix socket", "err", err)
	} else {
		os.Chmod(sockPath, 0666) // world-readable — proxy container runs as nobody; access controlled by bind mount scope
		// Restricted router: only the endpoints infra containers need.
		// No BearerAuth — containers don't hold the operator token.
		sockRouter := chi.NewRouter()
		sockRouter.Use(chiMiddleware.Recoverer)
		api.RegisterSocketRoutes(sockRouter, cfg, dc, logger, startup, routeOpts)
		unixServer := &http.Server{
			Handler:      sockRouter,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 5 * time.Minute,
		}
		go func() {
			logger.Info("Unix socket listening", "path", sockPath)
			if err := unixServer.Serve(unixListener); err != nil && err != http.ErrServerClosed {
				logger.Warn("unix socket error", "err", err)
			}
		}()
		defer func() {
			unixServer.Close()
			os.Remove(sockPath)
		}()
	}

	// Credential-only socket — mounted exclusively by egress for credential
	// resolution. Never bridged to TCP (ASK Tenet 7: credentials never
	// traverse a Docker network).
	credSockPath := filepath.Join(sockDir, "gateway-cred.sock")
	os.Remove(credSockPath)
	credListener, err := net.Listen("unix", credSockPath)
	if err != nil {
		logger.Warn("could not create credential socket", "err", err)
	} else {
		os.Chmod(credSockPath, 0666) // world-connectable — egress runs as root; access controlled by bind mount scope
		credRouter := chi.NewRouter()
		credRouter.Use(chiMiddleware.Recoverer)
		api.RegisterCredentialSocketRoutes(credRouter, cfg, dc, logger, startup, routeOpts)
		credServer := &http.Server{
			Handler:      credRouter,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 5 * time.Minute,
		}
		go func() {
			logger.Info("Credential socket listening", "path", credSockPath)
			if err := credServer.Serve(credListener); err != nil && err != http.ErrServerClosed {
				logger.Warn("credential socket error", "err", err)
			}
		}()
		defer func() {
			credServer.Close()
			os.Remove(credSockPath)
		}()
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutting down", "signal", sig)
	case err := <-errCh:
		logger.Error("server error", "err", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = httpServer.Shutdown(ctx)

	// Clean up PID file
	if pidFile != "" {
		os.Remove(pidFile)
	}

	logger.Info("shutdown complete")
	return nil
}

func validateConfiguredBackend(cfg *config.Config) error {
	backendName := normalizeRuntimeBackendName(cfg.Hub.DeploymentBackend)
	if runtimehost.IsContainerBackend(backendName) {
		if err := runtimehost.ValidateBackendConfig(backendName, cfg.Hub.DeploymentBackendConfig); err != nil {
			return fmt.Errorf("backend config: %w", err)
		}
	}
	return nil
}

func deriveLocalBuildID(paths ...string) string {
	for _, candidate := range paths {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		commitCmd := exec.Command("git", "-C", candidate, "rev-parse", "--short", "HEAD")
		commitOut, err := commitCmd.Output()
		if err != nil {
			continue
		}
		commit := strings.TrimSpace(string(commitOut))
		if commit == "" {
			continue
		}

		dirtyCmd := exec.Command("git", "-C", candidate, "status", "--porcelain")
		dirtyOut, err := dirtyCmd.Output()
		if err != nil {
			return commit
		}
		if strings.TrimSpace(string(dirtyOut)) != "" {
			return commit + "-dirty"
		}
		return commit
	}
	return ""
}

// listAgentNames returns the names of all agent directories under ~/.agency/agents/.
// Used by startup reconciliation to identify which agents are still configured.
func listAgentNames(home string) []string {
	agentsDir := filepath.Join(home, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// readPassword reads a line from stdin with echo disabled (masked input).
func readPassword() ([]byte, error) {
	fd := int(syscall.Stdin)
	return term.ReadPassword(fd)
}

func dockerDaemonConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		return filepath.Join(home, ".docker", "daemon.json"), nil
	case "linux":
		if isWSL() {
			return "", fmt.Errorf("Docker Desktop on WSL must be configured from Windows Docker Desktop settings")
		}
		return "/etc/docker/daemon.json", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func dockerPoolConfigured(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	rawPools, ok := cfg["default-address-pools"]
	if !ok {
		return false, nil
	}
	pools, ok := rawPools.([]interface{})
	if !ok {
		return false, nil
	}
	for _, raw := range pools {
		pool, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if size, ok := jsonNumberToInt(pool["size"]); ok && size >= 24 {
			return true, nil
		}
	}
	return false, nil
}

func jsonNumberToInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	default:
		return 0, false
	}
}

func writeDaemonConfig(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err == nil {
		if err := os.WriteFile(path, content, 0644); err == nil {
			return nil
		}
	}

	tmp, err := os.CreateTemp("", "agency-daemon-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	defer os.Remove(tmpPath)

	dir := filepath.Dir(path)
	if out, err := exec.Command("sudo", "mkdir", "-p", dir).CombinedOutput(); err != nil {
		return fmt.Errorf("sudo mkdir %s: %w: %s", dir, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("sudo", "install", "-m", "0644", tmpPath, path).CombinedOutput(); err != nil {
		return fmt.Errorf("sudo install %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func configureDockerPool(path string) error {
	var existing map[string]interface{}

	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("cannot parse existing %s: %w", path, err)
		}
	} else if os.IsNotExist(err) {
		existing = make(map[string]interface{})
	} else {
		return fmt.Errorf("cannot read existing %s: %w", path, err)
	}

	existing["default-address-pools"] = []map[string]interface{}{
		{"base": "172.16.0.0/12", "size": 24},
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}

	return writeDaemonConfig(path, append(out, '\n'))
}

func restartDockerForNetworkPool() error {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e", `quit app "Docker"`).Run()
		time.Sleep(2 * time.Second)
		if !tryStartDockerDesktop(false) {
			return fmt.Errorf("could not restart Docker Desktop automatically")
		}
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)
			retry := exec.Command("docker", "info")
			retry.Stdout = nil
			retry.Stderr = nil
			if retry.Run() == nil {
				return nil
			}
		}
		return fmt.Errorf("Docker Desktop did not become ready after restart")
	case "linux":
		if out, err := exec.Command("sudo", "systemctl", "restart", "docker").CombinedOutput(); err != nil {
			return fmt.Errorf("restart docker: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	default:
		return nil
	}
}

func reconcileDockerNetworkPool(configure bool) (bool, string, error) {
	path, err := dockerDaemonConfigPath()
	if err != nil {
		return false, "", err
	}
	configured, err := dockerPoolConfigured(path)
	if err != nil {
		return false, path, err
	}
	if configured || !configure {
		return configured, path, nil
	}

	fmt.Println("Configuring Docker network pool for /24 subnets...")
	if err := configureDockerPool(path); err != nil {
		return false, path, err
	}
	if err := restartDockerForNetworkPool(); err != nil {
		return false, path, err
	}
	return true, path, nil
}
