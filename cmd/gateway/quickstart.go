package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/geoffbelknap/agency/internal/apiclient"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/daemon"
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

	if gatewayRunning {
		// Gateway is running — check infra health
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
				fmt.Printf("  %s infrastructure  %s\n", qsRed.Render("✗"), err)
				return fmt.Errorf("infra start: %w", err)
			}
			fmt.Printf("  %s infrastructure  all services running\n", qsGreen.Render("✓"))
		}
	} else {
		// Gateway not running — start daemon, wait, store keys, bring up infra
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

		// Reload client to pick up token generated by daemon start
		c = apiclient.NewClient("http://" + cfg.GatewayAddr)

		// Store pending credentials
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

		fmt.Printf("  %s infrastructure  starting services...\n", qsDim.Render("…"))
		if err := c.InfraUpStream(func(component, status string) {
			fmt.Printf("    %s %s\n", qsGreen.Render("✓"), component)
		}); err != nil {
			fmt.Printf("  %s infrastructure  %s\n", qsRed.Render("✗"), err)
			return fmt.Errorf("infra start: %w", err)
		}
		fmt.Printf("  %s infrastructure  all services running\n", qsGreen.Render("✓"))
	}

	fmt.Println()
	fmt.Println(qsGreen.Render("Quickstart complete!"))
	return nil
}
