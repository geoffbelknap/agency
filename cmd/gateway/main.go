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
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/geoffbelknap/agency/internal/api"
	"github.com/geoffbelknap/agency/internal/apiclient"
	"github.com/geoffbelknap/agency/internal/update"
	auditpkg "github.com/geoffbelknap/agency/internal/audit"
	agencyCLI "github.com/geoffbelknap/agency/internal/cli"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/daemon"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/registry"
	"github.com/geoffbelknap/agency/internal/routing"
	"github.com/geoffbelknap/agency/internal/ws"
)

var (
	version   = "dev"
	commit    = "none"
	date      = "unknown"
	buildID   = "unknown"
	sourceDir = "" // stamped by Makefile ldflags for dev builds
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

	fmt.Println("Agency — An operating system for AI agents")
	fmt.Println()
	fmt.Println("Usage: agency <command> [options]")

	for _, g := range groups {
		cmds := grouped[g.id]
		if len(cmds) == 0 {
			continue
		}
		fmt.Printf("\n  %s\n", g.title)
		for _, c := range cmds {
			if c.HasSubCommands() {
				fmt.Printf("    %s  %s\n", pad(c.Name()+" ...", 28), c.Short)
			} else {
				use := c.Use
				if i := len(c.Name()); i < len(use) {
					use = c.Name() + use[i:]
				}
				fmt.Printf("    %s  %s\n", pad(use, 28), c.Short)
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
			fmt.Printf("  %s  %s\n", pad(use, 28), c.Short)
		}
	} else {
		fmt.Printf("Usage: %s\n", cmd.UseLine())
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

func main() {
	root := &cobra.Command{
		Use:          "agency",
		Short:        "Agency — An operating system for AI agents",
		Version:      fmt.Sprintf("%s (%s, %s)", version, buildID, date),
		SilenceUsage: true,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	}
	root.SetHelpFunc(customHelp)
	root.SetUsageFunc(func(cmd *cobra.Command) error { customHelp(cmd, nil); return nil })

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
	agencyHome := filepath.Join(os.Getenv("HOME"), ".agency")
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
	// Operators on Linux Docker Engine should set gateway_addr: "0.0.0.0:8200"
	// in ~/.agency/config.yaml so containers can reach the gateway via
	// host.docker.internal. Docker Desktop (Mac/Windows) tunnels through a VM
	// so 127.0.0.1 is reachable and no override is needed.
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
		noInfra   bool
		noBrowser bool
		cliMode   bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up the Agency platform (config, daemon, infrastructure)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check Docker first — fail fast with clear guidance
			if !noInfra {
				if err := checkDocker(); err != nil {
					return err
				}
			}

			if cliMode {
				// Quick setup: if --name or --preset flags are set, skip prompts
				if name != "" || preset != "" {
					return runSetup(provider, apiKey, notifyURL, noInfra, true, noBrowser)
				}

				// Interactive: prompt for provider/key if not set via flags
				if provider == "" && !cmd.Flags().Changed("provider") {
					scanner := bufio.NewScanner(os.Stdin)

					fmt.Println("Agency Setup")
					fmt.Println()
					fmt.Println("LLM Provider:")
					fmt.Println("  1. Anthropic (recommended)")
					fmt.Println("  2. OpenAI")
					fmt.Println("  3. Google")
					fmt.Println("  4. Skip (configure later)")
					fmt.Println()
					fmt.Print("Select [1-4, default 1]: ")

					choice := "1"
					if scanner.Scan() {
						if t := scanner.Text(); t != "" {
							choice = t
						}
					}

					switch choice {
					case "1":
						provider = "anthropic"
					case "2":
						provider = "openai"
					case "3":
						provider = "google"
					case "4":
						provider = ""
					default:
						provider = "anthropic"
					}

					if provider != "" && apiKey == "" {
						fmt.Printf("\n%s API key: ", provider)
						if keyBytes, err := readPassword(); err == nil {
							apiKey = strings.TrimSpace(string(keyBytes))
							fmt.Println() // newline after masked input
						}
					}
				}

				return runSetup(provider, apiKey, notifyURL, noInfra, true, noBrowser)
			}

			// Default: web-assisted setup — no prompts
			return runSetup("", "", notifyURL, noInfra, false, noBrowser)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Agent name (quick setup)")
	cmd.Flags().StringVar(&preset, "preset", "", "Agent preset (quick setup)")
	cmd.Flags().StringVar(&provider, "provider", "", "LLM provider (anthropic, openai, google)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "LLM provider API key")
	cmd.Flags().StringVar(&notifyURL, "notify-url", "", "Notification URL (ntfy or webhook) for operator alerts")
	cmd.Flags().BoolVar(&noInfra, "no-infra", false, "Skip Docker check and infrastructure startup")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't open the web UI in a browser (also respected via AGENCY_NO_BROWSER=1)")
	cmd.Flags().BoolVar(&cliMode, "cli", false, "Run full interactive setup in the terminal")

	return cmd
}

func daemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the gateway daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !daemon.IsRunning(8200) {
				fmt.Println("Daemon is not running.")
				return nil
			}
			if err := daemon.Stop(); err != nil {
				return err
			}
			fmt.Println("Daemon stopped.")
			return nil
		},
	}
}

func daemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the gateway daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemon.IsRunning(8200) {
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
					if !daemon.IsRunning(8200) {
						break
					}
				}
			}
			fmt.Println("Starting daemon...")
			if err := daemon.Start(8200); err != nil {
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
			if daemon.IsRunning(8200) {
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


// webHost derives the web UI hostname from the gateway address config.
// Returns "localhost" if the gateway binds to 0.0.0.0 or if the address
// cannot be parsed.
func webHost() string {
	cfg := config.Load()
	if host, _, err := net.SplitHostPort(cfg.GatewayAddr); err == nil && host != "" {
		if host != "0.0.0.0" {
			return host
		}
	}
	return "localhost"
}

func checkDocker() error {
	wsl := isWSL()

	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Fprintln(os.Stderr, "Docker is not installed.")
		fmt.Fprintln(os.Stderr, "")
		switch {
		case runtime.GOOS == "darwin":
			fmt.Fprintln(os.Stderr, "  Install Docker Desktop: https://docs.docker.com/desktop/install/mac-install/")
		case wsl:
			fmt.Fprintln(os.Stderr, "  Install Docker Desktop for Windows and enable WSL integration:")
			fmt.Fprintln(os.Stderr, "    1. Install: https://docs.docker.com/desktop/install/windows-install/")
			fmt.Fprintln(os.Stderr, "    2. Open Docker Desktop → Settings → Resources → WSL Integration")
			fmt.Fprintln(os.Stderr, "    3. Enable integration for your WSL distro")
		case runtime.GOOS == "linux":
			fmt.Fprintln(os.Stderr, "  Install Docker: curl -fsSL https://get.docker.com | sh")
		default:
			fmt.Fprintln(os.Stderr, "  Install Docker: https://docs.docker.com/get-docker/")
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Then re-run: agency setup")
		return fmt.Errorf("Docker is required but not installed")
	}

	// Check if Docker daemon is responsive
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Docker is installed but not running.")
		fmt.Fprintln(os.Stderr, "")
		switch {
		case runtime.GOOS == "darwin":
			fmt.Fprintln(os.Stderr, "  Open Docker Desktop and wait for it to start.")
		case wsl:
			fmt.Fprintln(os.Stderr, "  Open Docker Desktop on Windows and ensure WSL integration is enabled:")
			fmt.Fprintln(os.Stderr, "    Docker Desktop → Settings → Resources → WSL Integration")
		case runtime.GOOS == "linux":
			fmt.Fprintln(os.Stderr, "  Start Docker: sudo systemctl start docker")
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Then re-run: agency setup")
		return fmt.Errorf("Docker is not running")
	}

	return nil
}

func runSetup(provider, apiKey, notifyURL string, noInfra, cliMode, noBrowser bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	agencyHome := filepath.Join(home, ".agency")

	pendingKeys, err := config.RunInit(config.InitOptions{
		Provider:  provider,
		APIKey:    apiKey,
		NotifyURL: notifyURL,
	})
	if err != nil {
		return err
	}

	fmt.Println("Agency platform initialized at", agencyHome)
	fmt.Println()

	// Start the daemon
	fmt.Println("Starting daemon...")
	if err := daemon.Start(8200); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: daemon did not start: %v\n", err)
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  agency serve     # Start the daemon manually")
		return nil
	}

	fmt.Println("Daemon started successfully.")

	// Store LLM credentials in the encrypted credential store
	if len(pendingKeys) > 0 {
		cfg := config.Load()
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
			providerEnvMap := map[string]string{
				"anthropic": "ANTHROPIC_API_KEY",
				"openai":    "OPENAI_API_KEY",
				"google":    "GOOGLE_API_KEY",
			}
			cfg := config.Load()
			c := apiclient.NewClient("http://" + cfg.GatewayAddr)
			for _, provider := range existingProviders {
				envVar := providerEnvMap[provider]
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

	fmt.Println()
	if cliMode {
		fmt.Println("You're ready to go:")
		fmt.Println()
		fmt.Println("  agency create my-agent  # Create an agent")
		fmt.Println("  agency start my-agent   # Start an agent")
		fmt.Println("  agency status           # Check platform status")
		fmt.Println()
		fmt.Printf("  Open https://%s:8280 for the web UI\n", webHost())
	} else {
		setupURL := fmt.Sprintf("https://%s:8280/setup", webHost())
		if !noBrowser {
			_ = openBrowser(setupURL)
		}
		fmt.Printf("Finish setup at: %s\n", setupURL)
	}

	return nil
}

func runServe(httpAddr string) error {
	logger := log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: true,
		Prefix:         "agency",
	})

	cfg := config.Load()
	cfg.Version = version
	cfg.BuildID = buildID
	// Dev build: use source_dir stamped at build time if config doesn't override
	if cfg.SourceDir == "" && sourceDir != "" {
		cfg.SourceDir = sourceDir
	}
	logger.Info("agency home", "path", cfg.Home)

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

	// Docker client — optional, gateway starts in degraded mode if unavailable
	dc := docker.TryNewClient(logger)
	if dc != nil {
		logger.Info("docker connected")
	} else {
		logger.Warn("docker unavailable — gateway starting in degraded mode")
	}
	dockerStatus := docker.NewStatus(dc)

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
	ws.StartCommsRelay(wsHub, logger)

	// Event bus
	audit := logs.NewWriter(cfg.Home)
	auditFn := func(eventType string, data map[string]interface{}) {
		audit.Write("_system", eventType, data)
	}
	eventBus := events.NewBus(logger, auditFn)

	// Register delivery handlers
	agentDelivery := events.NewAgentDelivery("http://localhost:8202")
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

	// Wire channel events from comms relay to event bus
	wsHub.SetEventPublisher(func(channel, messageID, content, author string) {
		event := models.NewChannelEvent(channel, messageID,
			map[string]interface{}{"content": content},
			map[string]interface{}{"author": author, "channel": channel},
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
	if dc != nil {
		var healthErr error
		healthMgr, healthErr = orchestrate.NewMissionHealthMonitorWithClient(
			missionMgr,
			func(missionName, reason string) {
				events.EmitMissionEvent(eventBus, "mission_health_alert", missionName, map[string]interface{}{
					"reason": reason,
				})
			},
			func(name, reason string) error {
				return missionMgr.Pause(name, reason)
			},
			logger,
			dc.RawClient(),
		)
		if healthErr != nil {
			logger.Warn("mission health monitor unavailable", "error", healthErr)
		} else {
			healthMgr.Start(healthCtx)
		}
	}

	// Shared suppression tracker — marks agents undergoing intentional
	// stop/restart so container watchers don't fire spurious alerts.
	stopSuppress := orchestrate.NewStopSuppression(30 * time.Second)

	// Enforcer watcher — listens to Docker event stream for enforcer container
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
			dc.RawClient(),
		)
		if enfWatchErr != nil {
			logger.Warn("enforcer watcher unavailable", "error", enfWatchErr)
		} else {
			enforcerWatcher.Start(healthCtx)
		}
	}

	// Workspace watcher — listens for workspace container crashes and
	// auto-restarts. Emits platform events for operator alerting.
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
			dc.RawClient(),
		)
		if wsWatchErr != nil {
			logger.Warn("workspace watcher unavailable", "error", wsWatchErr)
		} else {
			workspaceWatcher.Start(healthCtx)
		}
	}

	// Audit summarizer — aggregates enforcer JSONL logs into per-mission metrics.
	// Runs every 15 minutes; also available on-demand via POST /api/v1/audit/summarize.
	knowledgeURL := "http://localhost:8201"
	auditSummarizer := auditpkg.NewAuditSummarizer(cfg.Home, knowledgeURL, logger)
	auditSummarizer.Start(healthCtx)

	// Initialize all gateway components via Startup.
	startup, err := api.Startup(cfg, dc, logger)
	if err != nil {
		logger.Fatal("gateway startup failed", "err", err)
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
	r.Use(corsMiddleware)
	r.Use(api.BearerAuth(cfg.Token, cfg.EgressToken, reg))
	routeOpts := api.RouteOptions{
		Hub:          wsHub,
		EventBus:     eventBus,
		Scheduler:    scheduler,
		WebhookMgr:   webhookMgr,
		NotifStore:   notifStore,
		StopSuppress:    stopSuppress,
		AuditSummarizer: auditSummarizer,
		Registry:        reg,
		Optimizer:       optimizer,
	}
	if healthMgr != nil {
		routeOpts.HealthMonitor = healthMgr
	}
	routeOpts.DockerStatus = dockerStatus
	api.RegisterAll(r, cfg, dc, logger, startup, routeOpts)

	// Wire auto-restore: when Docker reconnects, automatically bring up infra.
	if cfg.AutoRestoreInfra {
		dockerStatus.OnReconnect = func() {
			logger.Info("Docker reconnected — auto-restoring infrastructure")
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				newDC := docker.TryNewClient(logger)
				if newDC == nil {
					logger.Warn("auto-restore: Docker reconnect detected but client creation failed")
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
	os.Remove(sockPath)                              // clean up stale socket
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
