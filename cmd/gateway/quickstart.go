package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/geoffbelknap/agency/internal/apiclient"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/daemon"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var providerEnvVars = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"openai":    "OPENAI_API_KEY",
	"google":    "GEMINI_API_KEY",
}

var providerDisplayNames = map[string]string{
	"anthropic": "Anthropic",
	"openai":    "OpenAI",
	"google":    "Google Gemini",
}

var (
	qsBold  = lipgloss.NewStyle().Bold(true)
	qsGreen = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	qsRed   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	qsCyan  = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	qsDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// qsSpinner displays an animated spinner with a status message on the current line.
type qsSpinner struct {
	mu     sync.Mutex
	msg    string
	stop   chan struct{}
	done   chan struct{}
	frames []string
}

func newQSSpinner() *qsSpinner {
	return &qsSpinner{
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func (s *qsSpinner) update(status string) {
	s.mu.Lock()
	prev := s.msg
	s.msg = status
	s.mu.Unlock()
	if prev != "" {
		fmt.Printf("\r  %s %s\n", qsGreen.Render("✓"), prev)
	}
}

func (s *qsSpinner) run() {
	defer close(s.done)
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			if msg != "" {
				frame := qsCyan.Render(s.frames[i%len(s.frames)])
				fmt.Printf("\r  %s %s", frame, msg)
			}
			i++
		}
	}
}

func (s *qsSpinner) finish() {
	s.mu.Lock()
	msg := s.msg
	s.msg = ""
	s.mu.Unlock()
	close(s.stop)
	<-s.done
	if msg != "" {
		fmt.Printf("\r  %s %s\n", qsGreen.Render("✓"), msg)
	}
}

type agentChoice struct {
	label  string
	preset string
	name   string
	task   string
}

var agentChoices = []agentChoice{
	{"General assistant — research, write, analyze, code", "henry", "henry", "Look at my current directory and suggest something useful you could help me with."},
	{"Security operations — triage alerts, investigate threats, audit posture", "engineer", "security-analyst", "Give me a brief status report on what you're ready to monitor."},
	{"Code review — review PRs, find bugs, suggest improvements", "code-reviewer", "reviewer", "Look at my current directory and summarize what this project is."},
	{"Research & analysis — deep dives, report writing, data synthesis", "researcher", "researcher", "What are you capable of? Give me three things I should try first."},
}

type quickstartOptions struct {
	provider string
	key      string
	preset   string
	name     string
	noDemo   bool
	verbose  bool
}

func quickstartCmd() *cobra.Command {
	opts := quickstartOptions{}

	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Set up Agency from scratch in one command",
		Long: `Quickstart walks you through standing up Agency end-to-end:

  1. Checks your environment (Docker, etc.)
  2. Configures an LLM provider and API key
  3. Starts infrastructure containers
  4. Creates your first agent
  5. Sends a demo task to verify everything works

Run with --no-demo to skip the demo task.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuickstart(opts)
		},
	}

	cmd.Flags().StringVar(&opts.provider, "provider", "", "LLM provider (anthropic, openai, gemini, ollama)")
	cmd.Flags().StringVar(&opts.key, "key", "", "API key for the provider")
	cmd.Flags().StringVar(&opts.preset, "preset", "", "Agent preset to use")
	cmd.Flags().StringVar(&opts.name, "name", "", "Name for the first agent")
	cmd.Flags().BoolVar(&opts.noDemo, "no-demo", false, "Skip the demo task")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "Show detailed output")

	return cmd
}

// detectProvider reads ~/.agency/config.yaml and returns the configured llm_provider.
// Returns "" if no provider is configured.
func detectProvider() string {
	home := os.Getenv("AGENCY_HOME")
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".agency")
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		return ""
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	if p, ok := cfg["llm_provider"].(string); ok && p != "" {
		return p
	}
	return ""
}

// promptProvider displays a numbered menu and returns the chosen provider key.
func promptProvider() string {
	fmt.Println()
	fmt.Println(qsBold.Render("  Choose an LLM provider:"))
	fmt.Println()
	fmt.Printf("    1. Anthropic %s\n", qsDim.Render("(recommended)"))
	fmt.Println("    2. OpenAI")
	fmt.Println("    3. Google Gemini")
	fmt.Println()
	fmt.Print("  Enter choice [1]: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	choice := strings.TrimSpace(scanner.Text())

	switch choice {
	case "2":
		return "openai"
	case "3":
		return "google"
	default:
		return "anthropic"
	}
}

// promptAPIKey reads an API key with masked input.
func promptAPIKey(provider string) (string, error) {
	name := providerDisplayNames[provider]
	if name == "" {
		name = provider
	}
	fmt.Printf("  %s API key: ", name)
	raw, err := readPassword()
	fmt.Println() // newline after masked input
	if err != nil {
		return "", fmt.Errorf("read API key: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// validateAPIKey sends a cheap probe to verify the key is valid.
// Returns nil on success (HTTP 200 or 429), error on 401 or connection failure.
func validateAPIKey(provider, key string) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	var req *http.Request
	var err error

	switch provider {
	case "anthropic":
		req, err = http.NewRequest("POST", "https://api.anthropic.com/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-20250514","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`))
		if err != nil {
			return err
		}
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("content-type", "application/json")
	case "openai":
		req, err = http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+key)
	case "google":
		req, err = http.NewRequest("GET", "https://generativelanguage.googleapis.com/v1beta/openai/models", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+key)
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	// Discard body — untrusted data, don't interpret.
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	switch {
	case resp.StatusCode == 200, resp.StatusCode == 429:
		return nil
	case resp.StatusCode == 401:
		return fmt.Errorf("invalid API key (HTTP 401)")
	default:
		return fmt.Errorf("unexpected response (HTTP %d)", resp.StatusCode)
	}
}

func runQuickstart(opts quickstartOptions) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\n\n  Interrupted. Completed phases are saved — run `agency quickstart` again to resume.")
		os.Exit(0)
	}()

	fmt.Println()
	fmt.Println(qsBold.Render("Agency Quickstart"))
	fmt.Println(qsDim.Render("Setting up your agent platform"))
	fmt.Println()

	var pendingKeys []config.KeyEntry

	// Phase 1: Environment — check Docker
	if err := checkDocker(); err != nil {
		fmt.Printf("  %s environment     Docker not available\n", qsRed.Render("✗"))
		fmt.Println()
		fmt.Println(err.Error())
		fmt.Println()
		fmt.Println("Run `agency quickstart` again after installing Docker.")
		return fmt.Errorf("Docker required")
	}
	fmt.Printf("  %s environment     Docker running\n", qsGreen.Render("✓"))

	// Phase 2: Provider — detect or prompt for LLM provider and API key
	providerName := opts.provider
	apiKey := opts.key
	needsPrompt := false

	if providerName == "" {
		providerName = detectProvider()
	}

	if providerName != "" && apiKey == "" {
		// Provider already configured, no new key needed
		displayName := providerDisplayNames[providerName]
		if displayName == "" {
			displayName = providerName
		}
		fmt.Printf("  %s provider        %s already configured\n", qsGreen.Render("✓"), displayName)
	} else {
		needsPrompt = (providerName == "" || apiKey == "")
	}

	if needsPrompt {
		if providerName == "" {
			providerName = promptProvider()
		}

		const maxAttempts = 3
		validated := false
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			var err error
			apiKey, err = promptAPIKey(providerName)
			if err != nil {
				return fmt.Errorf("provider setup failed: %w", err)
			}
			if apiKey == "" {
				fmt.Printf("  %s No key entered.\n", qsRed.Render("✗"))
				continue
			}

			fmt.Printf("  %s Validating key...", qsDim.Render("…"))
			if err := validateAPIKey(providerName, apiKey); err != nil {
				fmt.Printf("\r  %s Validation failed: %s\n", qsRed.Render("✗"), err)
				if attempt < maxAttempts {
					fmt.Printf("  %s Attempt %d of %d. Try again.\n", qsDim.Render("…"), attempt, maxAttempts)
				}
				continue
			}

			fmt.Printf("\r  %s Key validated.                \n", qsGreen.Render("✓"))
			validated = true
			break
		}

		if !validated {
			fmt.Println()
			fmt.Printf("  Could not validate API key after %d attempts.\n", maxAttempts)
			fmt.Println("  See: https://github.com/geoffbelknap/agency#provider-setup")
			return fmt.Errorf("provider validation failed")
		}
	}

	// Run config init to set up ~/.agency/ and collect pending keys
	if apiKey != "" {
		var err error
		pendingKeys, err = config.RunInit(config.InitOptions{
			Provider: providerName,
			APIKey:   apiKey,
		})
		if err != nil {
			fmt.Printf("  %s config init failed: %s\n", qsRed.Render("✗"), err)
			return fmt.Errorf("config init: %w", err)
		}
	} else if providerName != "" {
		// No new key but ensure ~/.agency/ exists
		var err error
		pendingKeys, err = config.RunInit(config.InitOptions{
			Provider: providerName,
		})
		if err != nil {
			fmt.Printf("  %s config init failed: %s\n", qsRed.Render("✗"), err)
			return fmt.Errorf("config init: %w", err)
		}
	}

	if needsPrompt {
		displayName := providerDisplayNames[providerName]
		if displayName == "" {
			displayName = providerName
		}
		fmt.Printf("  %s provider        %s\n", qsGreen.Render("✓"), displayName)
	}

	// Phase 3: Infrastructure — start daemon and bring up services
	cfg := config.Load()
	c := apiclient.NewClient("http://" + cfg.GatewayAddr)

	gatewayRunning := c.CheckGateway() == nil

	// If the daemon was already running, it may have started before RunInit
	// created config.yaml (e.g., `make install` starts daemon, then quickstart
	// runs RunInit). Restart so it picks up the new tokens and egress_token.
	// Without this, the daemon runs with empty auth tokens and containers
	// (especially egress) get empty GATEWAY_TOKEN — breaking credential swap.
	if gatewayRunning {
		daemon.Stop()
		time.Sleep(1 * time.Second)
		gatewayRunning = false
	}

	if !gatewayRunning {
		if err := daemon.Start(8200); err != nil {
			fmt.Printf("  %s infrastructure  failed to start daemon: %s\n", qsRed.Render("✗"), err)
			return fmt.Errorf("daemon start: %w", err)
		}

		// Wait for gateway to become reachable (up to 15 seconds)
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if c.CheckGateway() == nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if c.CheckGateway() != nil {
			fmt.Printf("  %s infrastructure  gateway did not start within 15s\n", qsRed.Render("✗"))
			return fmt.Errorf("gateway did not start")
		}

		// Reload config and client — daemon now has tokens from RunInit
		cfg = config.Load()
		c = apiclient.NewClient("http://" + cfg.GatewayAddr)
	}

	// Store pending credentials (always — gateway may have been running
	// but credentials not yet stored, e.g. after make install)
	for _, key := range pendingKeys {
		if _, err := c.CredentialSet(map[string]interface{}{
			"name":     key.EnvVar,
			"value":    key.Key,
			"kind":     "provider",
			"scope":    "platform",
			"protocol": "api-key",
			"protocol_config": map[string]interface{}{
				"domains": config.ProviderDomains(key.Provider),
			},
		}); err != nil {
			fmt.Printf("  %s infrastructure  failed to store credential: %s\n", qsRed.Render("✗"), err)
			return fmt.Errorf("credential store: %w", err)
		}
	}

	// Ensure infra services are running
	status, err := c.InfraStatus()
	allHealthy := err == nil && len(status.Components) > 0
	if allHealthy {
		for _, comp := range status.Components {
			if comp["status"] != "running" {
				allHealthy = false
				break
			}
		}
	}

	if allHealthy {
		fmt.Printf("  %s infrastructure  all services healthy\n", qsGreen.Render("✓"))
	} else {
		fmt.Printf("  %s infrastructure  starting services...\n", qsDim.Render("…"))
		if err := c.InfraUpStream(func(component, status string) {
			fmt.Printf("    %s %s\n", qsGreen.Render("✓"), component)
		}); err != nil {
			// Non-fatal if most services started (gateway-proxy may fail on macOS)
			if strings.Contains(err.Error(), "gateway-proxy") && !strings.Contains(err.Error(), "knowledge") && !strings.Contains(err.Error(), "comms") {
				fmt.Printf("  %s infrastructure  services running (gateway-proxy unavailable)\n", qsGreen.Render("✓"))
			} else {
				fmt.Printf("  %s infrastructure  %s\n", qsRed.Render("✗"), err)
				return fmt.Errorf("infra start: %w", err)
			}
		} else {
			fmt.Printf("  %s infrastructure  all services running\n", qsGreen.Render("✓"))
		}
	}

	// Phase 3b: Hub sync + provider install
	// After infra is up and credentials are stored, install the provider
	// so routing.yaml and egress config are populated.
	if providerName != "" {
		c.HubUpdate()
		c.Post("/api/v1/hub/install", map[string]interface{}{
			"component": providerName,
		})
	}

	// Phase 4: Agent — create and start first agent
	var runningAgent string
	var choice agentChoice

	agents, err := c.ListAgents()
	if err == nil {
		for _, a := range agents {
			if s, _ := a["status"].(string); s == "running" {
				name, _ := a["name"].(string)
				runningAgent = name
				break
			}
		}
	}

	if runningAgent != "" {
		// Agent exists — check if it needs a restart (stale build)
		needsRestart := false
		if agentInfo, err := c.ShowAgent(runningAgent); err == nil {
			if agentBuild, _ := agentInfo["build_id"].(string); agentBuild != "" && agentBuild != buildID {
				needsRestart = true
			}
		}

		if needsRestart {
			fmt.Printf("  %s agent           %s needs restart (new build)\n", qsCyan.Render("●"), runningAgent)
			c.StopAgent(runningAgent)
			sp := newQSSpinner()
			go sp.run()
			sp.update(fmt.Sprintf("agent           restarting %s...", runningAgent))
			c.StartAgentStream(runningAgent, func(component, status string) {
				sp.update(fmt.Sprintf("agent           %s", component))
			})
			sp.finish()
			fmt.Printf("  %s agent           %s restarted\n", qsGreen.Render("✓"), runningAgent)
		} else {
			fmt.Printf("  %s agent           %s already running\n", qsGreen.Render("✓"), runningAgent)
		}
	} else {
		// Determine agent choice
		if opts.preset != "" {
			// Match --preset flag to choices
			found := false
			for _, ac := range agentChoices {
				if ac.preset == opts.preset {
					choice = ac
					found = true
					break
				}
			}
			if !found {
				// Use the preset directly with a generic name
				choice = agentChoice{
					label:  opts.preset,
					preset: opts.preset,
					name:   opts.preset,
					task:   "What can you help me with?",
				}
			}
		} else {
			// Prompt user
			fmt.Println()
			fmt.Println(qsBold.Render("  Choose your first agent:"))
			fmt.Println()
			for i, ac := range agentChoices {
				fmt.Printf("    %d. %s\n", i+1, ac.label)
			}
			fmt.Println()
			fmt.Print("  Enter choice [1]: ")

			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			input := strings.TrimSpace(scanner.Text())

			idx := 0
			switch input {
			case "2":
				idx = 1
			case "3":
				idx = 2
			case "4":
				idx = 3
			default:
				idx = 0
			}
			choice = agentChoices[idx]
		}

		agentName := choice.name
		if opts.name != "" {
			agentName = opts.name
		}

		// Create the agent
		if _, err := c.CreateAgent(agentName, choice.preset); err != nil {
			fmt.Printf("  %s agent           failed to create: %s\n", qsRed.Render("✗"), err)
			return fmt.Errorf("agent create: %w", err)
		}

		// Start with spinner
		sp := newQSSpinner()
		go sp.run()
		sp.update(fmt.Sprintf("agent           starting %s...", agentName))

		startErr := c.StartAgentStream(agentName, func(component, status string) {
			sp.update(fmt.Sprintf("agent           %s", component))
		})
		sp.finish()

		if startErr != nil {
			fmt.Printf("  %s agent           start failed: %s\n", qsRed.Render("✗"), startErr)
			// Run doctor for diagnostics
			if doctorOut, derr := c.Get("/api/v1/admin/doctor"); derr == nil {
				fmt.Printf("\n%s\n", string(doctorOut))
			}
			fmt.Printf("\n  Try: agency start %s --verbose\n", agentName)
			return fmt.Errorf("agent start: %w", startErr)
		}

		runningAgent = agentName
		fmt.Printf("  %s agent           %s running\n", qsGreen.Render("✓"), agentName)
	}

	// Phase 5: Demo
	if !opts.noDemo && runningAgent != "" {
		demoTask := choice.task
		if demoTask == "" {
			demoTask = "What are you capable of? Give me three things I should try first."
		}

		fmt.Println()
		fmt.Println("  Your agent is ready. Let's try it out:")
		fmt.Println()
		fmt.Printf("  > %s is thinking...", qsBold.Render(runningAgent))

		if err := streamDemoResponse(c, c.BaseURL, runningAgent, demoTask); err != nil {
			fmt.Println()
			fmt.Println()
			fmt.Println("  Agent started but the first task is taking a while.")
			fmt.Printf("  Check %s or open %s\n", qsBold.Render("agency status"), qsBold.Render("http://localhost:8280"))
		}
	}

	// What's next footer
	fmt.Println()
	fmt.Println("  " + qsDim.Render("────────────────────────────────────────"))
	fmt.Println("  Agent is running. What's next:")
	if runningAgent != "" {
		fmt.Printf("    • Send tasks:  %s\n", qsBold.Render(fmt.Sprintf("agency send %s \"your task here\"", runningAgent)))
	}
	fmt.Printf("    • Web UI:      %s\n", qsBold.Render("http://localhost:8280"))
	fmt.Printf("    • Status:      %s\n", qsBold.Render("agency status"))
	fmt.Printf("    • More agents: %s\n", qsBold.Render("agency hub search"))
	if choice.preset == "engineer" {
		fmt.Printf("    • Full team:   %s\n", qsBold.Render("agency hub install security-ops"))
	}
	if choice.preset == "code-reviewer" && runningAgent != "" {
		fmt.Printf("    • Review PRs:  %s\n", qsBold.Render(fmt.Sprintf("agency send %s \"review my latest commit\"", runningAgent)))
	}
	fmt.Println()

	return nil
}

func streamDemoResponse(client *apiclient.Client, baseURL, agentName, task string) error {
	dmChannel := "dm-" + agentName

	// Connect WebSocket
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/ws"
	header := http.Header{}
	if client.Token != "" {
		header.Set("X-Agency-Token", client.Token)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("WebSocket connect: %w", err)
	}
	defer conn.Close()

	// Subscribe to the DM channel
	sub := map[string]interface{}{
		"type":     "subscribe",
		"channels": []string{dmChannel},
	}
	conn.WriteJSON(sub)

	// Send the task
	client.SendMessage(dmChannel, task)

	// Listen for agent response with timeout.
	// Recover from gorilla/websocket panics on failed connections.
	defer func() {
		if r := recover(); r != nil {
			// WebSocket panic — swallow it, the caller handles the timeout fallback
		}
	}()

	deadline := time.Now().Add(60 * time.Second)
	var connFailed bool

	for time.Now().Before(deadline) && !connFailed {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // read timeout, retry
			}
			connFailed = true
			break // any other error — stop reading
		}

		var event map[string]interface{}
		if json.Unmarshal(msgBytes, &event) != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		if eventType != "message" {
			continue
		}

		msg, _ := event["message"].(map[string]interface{})
		author, _ := msg["author"].(string)
		if author == "_operator" || author == "" {
			continue
		}

		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}

		// Clear the "thinking" line and print response
		fmt.Print("\r                                          \r")
		fmt.Println()
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			fmt.Printf("  %s\n", line)
		}
		return nil
	}

	return fmt.Errorf("timeout")
}
