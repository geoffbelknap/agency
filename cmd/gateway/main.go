package main

import (
	"bufio"
	"context"
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
	"github.com/geoffbelknap/agency/internal/backendhealth"
	agencyCLI "github.com/geoffbelknap/agency/internal/cli"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/daemon"
	"github.com/geoffbelknap/agency/internal/events"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	agencylog "github.com/geoffbelknap/agency/internal/logging"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
	"github.com/geoffbelknap/agency/internal/registry"
	"github.com/geoffbelknap/agency/internal/routing"
	"github.com/geoffbelknap/agency/internal/runtimeprovision"
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
	if runtimeCmd, _, err := root.Find([]string{"runtime"}); err == nil && runtimeCmd != nil {
		runtimeCmd.AddCommand(runtimeProvisionCmd())
	}

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
	// Host-side clients and local microVM services continue to use localhost.
	cmd.Flags().StringVar(&httpAddr, "http", "127.0.0.1:8200", "HTTP API listen address")

	return cmd
}

func setupCmd() *cobra.Command {
	var (
		name      string
		preset    string
		provider  string
		apiKey    string
		notifyURL string
		backend   string
		noInfra   bool
		noBrowser bool
		cliMode   bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up the Agency platform (config, daemon, infrastructure)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Select the runtime backend before anything writes config or starts
			// a daemon. MicroVM backends are selected automatically by host OS.
			var (
				backendName string
				backendCfg  map[string]string
			)
			if !noInfra {
				b, cfg, err := selectRuntimeBackend(backend)
				if err != nil {
					return err
				}
				backendName, backendCfg = b, cfg
			}

			if cliMode {
				// Quick setup: if --name or --preset flags are set, skip prompts
				if name != "" || preset != "" {
					return runSetup(provider, apiKey, notifyURL, backendName, backendCfg, noInfra, true, noBrowser)
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

				return runSetup(provider, apiKey, notifyURL, backendName, backendCfg, noInfra, true, noBrowser)
			}

			// Default: web-assisted setup — no prompts.
			// Pass through --provider/--api-key if given (supports non-interactive use).
			return runSetup(provider, apiKey, notifyURL, backendName, backendCfg, noInfra, false, noBrowser)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Agent name (quick setup)")
	cmd.Flags().StringVar(&preset, "preset", "", "Agent preset (quick setup)")
	cmd.Flags().StringVar(&provider, "provider", "", "LLM provider adapter name")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "LLM provider API key")
	cmd.Flags().StringVar(&notifyURL, "notify-url", "", "Notification URL (ntfy or webhook) for operator alerts")
	cmd.Flags().StringVar(&backend, "backend", "", "MicroVM runtime backend to use; defaults to firecracker on Linux/WSL and apple-vf-microvm on macOS. Also respected via AGENCY_RUNTIME_BACKEND.")
	cmd.Flags().BoolVar(&noInfra, "no-infra", false, "Skip runtime backend checks and infrastructure startup")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't open the web UI in a browser (also respected via AGENCY_NO_BROWSER=1)")
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

// webHost derives the local web UI hostname. The web UI serves on loopback,
// even when the gateway daemon listens on a backend bridge address for
// VM-backed runtimes.
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

func normalizeRuntimeBackendName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return defaultRuntimeBackendForHost()
	}
	if name == hostruntimebackend.BackendFirecracker || name == hostruntimebackend.BackendAppleVFMicroVM || name == hostruntimebackend.BackendMicroagent {
		return name
	}
	return runtimehost.NormalizeContainerBackend(name)
}

func selectRuntimeBackend(override string) (string, map[string]string, error) {
	override = strings.TrimSpace(strings.ToLower(override))
	if override == "" {
		override = strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_RUNTIME_BACKEND")))
	}
	// Honor an existing choice persisted in config.yaml so re-running setup
	// without a flag doesn't silently flip between supported microVM backends.
	// Flag and env still win over the persisted value.
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
		return backend, withMicroVMArtifactConfig(backend, nil), nil
	}
	if override == hostruntimebackend.BackendFirecracker || override == hostruntimebackend.BackendAppleVFMicroVM || override == hostruntimebackend.BackendMicroagent {
		fmt.Fprintf(os.Stderr, "Using %s runtime backend.\n", override)
		return override, withMicroVMArtifactConfig(override, mergeBackendSocketConfig(configuredCfg, nil)), nil
	}
	return "", nil, fmt.Errorf("runtime backend %q is not supported; Agency supports firecracker on Linux/WSL, apple-vf-microvm on macOS, and microagent as an opt-in adapter", override)
}

func withMicroVMArtifactConfig(backend string, cfg map[string]string) map[string]string {
	return withMicroagentArtifactConfig(backend, withFirecrackerArtifactConfig(backend, withAppleVFArtifactConfig(backend, cfg)))
}

func withMicroagentArtifactConfig(backend string, cfg map[string]string) map[string]string {
	if backend != hostruntimebackend.BackendMicroagent {
		return cfg
	}
	home := config.Load().Home
	sourceRoot := config.Load().SourceDir
	defaults := map[string]string{
		"binary_path":          "microagent",
		"state_dir":            filepath.Join(home, "runtime", "microagent"),
		"entrypoint":           "/app/entrypoint.sh",
		"enforcer_binary_path": filepath.Join(sourceRoot, "bin", "agency-enforcer-host"),
		"mke2fs_path":          defaultMke2fsPath(),
	}
	envPaths := map[string]string{
		"binary_path":          strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_BIN")),
		"state_dir":            strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_STATE_DIR")),
		"entrypoint":           strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_ENTRYPOINT")),
		"enforcer_binary_path": strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_ENFORCER_BIN")),
		"rootfs_oci_ref":       strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_ROOTFS_OCI_REF")),
		"mke2fs_path":          strings.TrimSpace(os.Getenv("AGENCY_MKE2FS")),
	}
	out := make(map[string]string, len(cfg)+len(defaults)+len(envPaths))
	for k, v := range cfg {
		out[k] = v
	}
	for key, value := range envPaths {
		if strings.TrimSpace(out[key]) == "" && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	for key, value := range defaults {
		if strings.TrimSpace(out[key]) == "" && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func withAppleVFArtifactConfig(backend string, cfg map[string]string) map[string]string {
	if backend != hostruntimebackend.BackendAppleVFMicroVM {
		return cfg
	}
	home := config.Load().Home
	sourceRoot := config.Load().SourceDir
	defaults := map[string]string{
		"kernel_path":              hostruntimebackend.DefaultAppleVFKernelPath(home),
		"helper_binary":            filepath.Join(sourceRoot, "bin", "agency-apple-vf-helper"),
		"enforcer_binary_path":     filepath.Join(sourceRoot, "bin", "agency-enforcer-host"),
		"vsock_bridge_binary_path": filepath.Join(sourceRoot, "bin", "agency-vsock-http-bridge-linux-arm64"),
		"mke2fs_path":              defaultMke2fsPath(),
	}
	envPaths := map[string]string{
		"kernel_path":              strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_KERNEL")),
		"helper_binary":            strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_HELPER_BIN")),
		"enforcer_binary_path":     strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_ENFORCER_BIN")),
		"vsock_bridge_binary_path": strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN")),
		"rootfs_oci_ref":           strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_ROOTFS_OCI_REF")),
		"mke2fs_path":              strings.TrimSpace(os.Getenv("AGENCY_MKE2FS")),
	}
	out := make(map[string]string, len(cfg)+len(defaults)+len(envPaths))
	for k, v := range cfg {
		out[k] = v
	}
	for key, value := range envPaths {
		if strings.TrimSpace(out[key]) == "" && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	for key, value := range defaults {
		if strings.TrimSpace(out[key]) == "" && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func withFirecrackerArtifactConfig(backend string, cfg map[string]string) map[string]string {
	if backend != hostruntimebackend.BackendFirecracker {
		return cfg
	}
	home := config.Load().Home
	sourceRoot := config.Load().SourceDir
	artifactDir := filepath.Join(home, "runtime", "firecracker", "artifacts")
	defaultArch := runtime.GOARCH
	switch defaultArch {
	case "amd64":
		defaultArch = "x86_64"
	case "arm64":
		defaultArch = "aarch64"
	}
	defaultVersion := "v1.12.1"
	defaultKernelPath, err := runtimeprovision.DefaultFirecrackerKernelPath(home, defaultArch)
	if err != nil {
		defaultKernelPath = filepath.Join(artifactDir, "vmlinux")
	}
	defaults := map[string]string{
		"binary_path":              filepath.Join(artifactDir, defaultVersion, "firecracker-"+defaultVersion+"-"+defaultArch),
		"kernel_path":              defaultKernelPath,
		"enforcer_binary_path":     filepath.Join(sourceRoot, "bin", "enforcer"),
		"vsock_bridge_binary_path": filepath.Join(sourceRoot, "bin", "agency-vsock-http-bridge"),
		"mke2fs_path":              defaultMke2fsPath(),
	}
	envPaths := map[string]string{
		"binary_path":              strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_BIN")),
		"kernel_path":              strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_KERNEL")),
		"enforcer_binary_path":     strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_ENFORCER_BIN")),
		"vsock_bridge_binary_path": strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_VSOCK_BRIDGE_BIN")),
		"rootfs_oci_ref":           strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_ROOTFS_OCI_REF")),
		"mke2fs_path":              strings.TrimSpace(os.Getenv("AGENCY_MKE2FS")),
	}
	out := make(map[string]string, len(cfg)+len(defaults)+len(envPaths))
	for k, v := range cfg {
		out[k] = v
	}
	for key, value := range envPaths {
		if strings.TrimSpace(out[key]) == "" && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	for key, value := range defaults {
		if strings.TrimSpace(out[key]) == "" && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func defaultMke2fsPath() string {
	const homebrewMke2fs = "/opt/homebrew/opt/e2fsprogs/sbin/mke2fs"
	if info, err := os.Stat(homebrewMke2fs); err == nil && !info.IsDir() {
		return homebrewMke2fs
	}
	return "mke2fs"
}

func routeBackendHealth(status *runtimehost.Status) backendhealth.Recorder {
	if status == nil {
		return nil
	}
	return status
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

func runSetup(provider, apiKey, notifyURL, backend string, backendCfg map[string]string, noInfra, cliMode, noBrowser bool) error {
	provider = normalizeProvider(provider)
	gatewayAddr := ""
	if !noInfra {
		if err := ensureMicroVMRuntimeArtifacts(context.Background(), backend, backendCfg, func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}); err != nil {
			return err
		}
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
	// daemon fails to start (for example backend readiness fails mid-setup),
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

	var dc *runtimehost.Client
	var infraDC *runtimehost.Client
	logger.Info("gateway starting with microVM runtime backend", "backend", backendName)
	var backendHealthStatus *runtimehost.Status

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
	routeOpts.BackendHealth = routeBackendHealth(backendHealthStatus)
	api.RegisterAll(r, cfg, dc, logger, startup, routeOpts)

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

	// Unix socket listener for host-service-to-gateway communication.
	// Socket lives in ~/.agency/run/ so host services can bind the directory
	// and survive gateway restarts.
	sockDir := filepath.Join(cfg.Home, "run")
	os.MkdirAll(sockDir, 0755)
	// Proxy-safe socket — bridged to TCP by the gateway proxy service.
	// Does NOT include credential resolution endpoints.
	sockPath := filepath.Join(sockDir, "gateway.sock")
	os.Remove(sockPath)                                // clean up stale socket
	os.Remove(filepath.Join(cfg.Home, "gateway.sock")) // clean up legacy location
	unixListener, err := net.Listen("unix", sockPath)
	if err != nil {
		logger.Warn("could not create Unix socket", "err", err)
	} else {
		os.Chmod(sockPath, 0666) // world-readable; access controlled by bind mount scope
		// Restricted router: only the endpoints host infra services need.
		// No BearerAuth — services don't hold the operator token.
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
	// traverse a shared runtime network).
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
		return fmt.Errorf("runtime backend %q is legacy container execution and is no longer supported; use firecracker on Linux/WSL, apple-vf-microvm on macOS, or microagent as an opt-in adapter", backendName)
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

// readPassword reads a line from stdin with echo disabled (masked input).
func readPassword() ([]byte, error) {
	fd := int(syscall.Stdin)
	return term.ReadPassword(fd)
}
