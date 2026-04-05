# Quickstart Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** New `agency quickstart` command that gets a user from zero to "agent just did work" in under 10 minutes — 5 phases: environment, provider, infrastructure, agent, demo.

**Architecture:** Single new file `cmd/gateway/quickstart.go` with a cobra command. Reuses existing patterns: `config.RunInit()` for setup, `readPassword()` for key input, spinner for progress, streaming APIs for infra/agent start. Phase 5 demo uses a WebSocket client to subscribe to the agent's DM channel and stream the response in real-time. Each phase auto-detects completion and skips if already done.

**Tech Stack:** Go, Cobra, Lipgloss, gorilla/websocket (already a dependency), existing apiclient

**Spec:** `docs/specs/quickstart-wizard.md`

---

### Task 1: Scaffold quickstart command and register it

**Files:**
- Create: `cmd/gateway/quickstart.go`
- Modify: `cmd/gateway/main.go`

- [ ] **Step 1: Create quickstart.go with command skeleton**

Create `cmd/gateway/quickstart.go`:

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func quickstartCmd() *cobra.Command {
	var (
		provider string
		apiKey   string
		preset   string
		name     string
		noDemo   bool
		verbose  bool
	)

	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Get started with Agency in under 10 minutes",
		Long:  "Interactive wizard: checks environment, configures a provider, starts infrastructure, creates an agent, and runs a demo task.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuickstart(quickstartOptions{
				provider: provider,
				apiKey:   apiKey,
				preset:   preset,
				name:     name,
				noDemo:   noDemo,
				verbose:  verbose,
			})
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "Skip provider prompt (anthropic, openai, google)")
	cmd.Flags().StringVar(&apiKey, "key", "", "Skip key prompt (requires --provider)")
	cmd.Flags().StringVar(&preset, "preset", "", "Skip agent choice prompt (use this preset)")
	cmd.Flags().StringVar(&name, "name", "", "Override default agent name")
	cmd.Flags().BoolVar(&noDemo, "no-demo", false, "Skip the demo task")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show detailed output for all phases")

	return cmd
}

type quickstartOptions struct {
	provider string
	apiKey   string
	preset   string
	name     string
	noDemo   bool
	verbose  bool
}

func runQuickstart(opts quickstartOptions) error {
	fmt.Println()
	fmt.Println(bold.Render("  Agency Quickstart"))
	fmt.Println()

	// Phase 1: Environment
	// Phase 2: Provider
	// Phase 3: Infrastructure
	// Phase 4: Agent
	// Phase 5: Demo

	fmt.Println()
	fmt.Println("  Quickstart complete! (phases not yet implemented)")
	return nil
}
```

- [ ] **Step 2: Register in main.go**

In `cmd/gateway/main.go`, find where `setupCmd()` is registered (around line 222) and add quickstart right after:

```go
	quickstartC := quickstartCmd()
	quickstartC.GroupID = "platform"
	root.AddCommand(quickstartC)
```

- [ ] **Step 3: Build and verify**

```bash
go build ./cmd/gateway/ && ./agency quickstart --help
```

Expected: Help text showing the command and its flags.

- [ ] **Step 4: Commit**

```bash
git add cmd/gateway/quickstart.go cmd/gateway/main.go
git commit -m "feat: scaffold agency quickstart command"
```

---

### Task 2: Phase 1 — Environment check

**Files:**
- Modify: `cmd/gateway/quickstart.go`

- [ ] **Step 1: Implement Phase 1**

Add to `runQuickstart()`, replacing the Phase 1 placeholder:

```go
	// Phase 1: Environment — check Docker
	if err := checkDocker(); err != nil {
		fmt.Printf("  %s environment     Docker not available\n", red.Render("✗"))
		fmt.Println()
		fmt.Println(err.Error())
		fmt.Println()
		fmt.Println("Run `agency quickstart` again after installing Docker.")
		return fmt.Errorf("Docker required")
	}
	fmt.Printf("  %s environment     Docker running\n", green.Render("✓"))
```

`checkDocker()` already exists in `cmd/gateway/main.go` (used by setup command). It returns nil if Docker is running, error with platform-specific guidance if not.

- [ ] **Step 2: Build and test**

```bash
go build ./cmd/gateway/ && ./agency quickstart
```

Expected: "✓ environment     Docker running" (assuming Docker is running).

- [ ] **Step 3: Commit**

```bash
git add cmd/gateway/quickstart.go
git commit -m "feat(quickstart): phase 1 — environment check"
```

---

### Task 3: Phase 2 — Provider detection and configuration

**Files:**
- Modify: `cmd/gateway/quickstart.go`

This is the most complex phase. It needs to: detect existing provider config, prompt if missing, validate the API key, and store credentials.

- [ ] **Step 1: Add provider detection and prompting**

Add imports at top of quickstart.go:

```go
import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoffbelknap/agency/internal/apiclient"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/daemon"
)
```

Add these functions to quickstart.go:

```go
// providerEnvVars maps provider names to credential env var names.
var providerEnvVars = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"openai":    "OPENAI_API_KEY",
	"google":    "GEMINI_API_KEY",
}

// providerDisplayNames maps provider names to display text.
var providerDisplayNames = map[string]string{
	"anthropic": "Anthropic",
	"openai":    "OpenAI",
	"google":    "Google Gemini",
}

// detectProvider checks if a provider is already configured.
// Returns the provider name or empty string.
func detectProvider() string {
	cfg := config.Load()
	if cfg.Provider != "" {
		return cfg.Provider
	}
	return ""
}

// promptProvider asks the user to select a provider.
func promptProvider() string {
	fmt.Println()
	fmt.Println("  Which LLM provider do you want to use?")
	fmt.Println()
	fmt.Println("    1. Anthropic (recommended)")
	fmt.Println("    2. OpenAI")
	fmt.Println("    3. Google Gemini")
	fmt.Println()
	fmt.Print("  Choice [1]: ")

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
	fmt.Printf("  API key: ")
	keyBytes, err := readPassword()
	fmt.Println() // newline after masked input
	if err != nil {
		return "", fmt.Errorf("read API key: %w", err)
	}
	return strings.TrimSpace(string(keyBytes)), nil
}

// validateAPIKey sends a cheap probe request to verify the key works.
func validateAPIKey(provider, key string) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	switch provider {
	case "anthropic":
		body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
		req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		io.ReadAll(io.LimitReader(resp.Body, 1024))
		if resp.StatusCode == 401 {
			return fmt.Errorf("invalid API key")
		}
		if resp.StatusCode >= 400 && resp.StatusCode != 429 {
			return fmt.Errorf("API error: HTTP %d", resp.StatusCode)
		}
		return nil

	case "openai":
		req, _ := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		io.ReadAll(io.LimitReader(resp.Body, 1024))
		if resp.StatusCode == 401 {
			return fmt.Errorf("invalid API key")
		}
		return nil

	case "google":
		req, _ := http.NewRequest("GET", "https://generativelanguage.googleapis.com/v1beta/openai/models", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		io.ReadAll(io.LimitReader(resp.Body, 1024))
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return fmt.Errorf("invalid API key")
		}
		return nil
	}
	return fmt.Errorf("unknown provider: %s", provider)
}
```

- [ ] **Step 2: Wire Phase 2 into runQuickstart**

Replace the Phase 2 placeholder in `runQuickstart()`:

```go
	// Phase 2: Provider
	providerName := opts.provider
	apiKey := opts.apiKey

	if providerName == "" {
		providerName = detectProvider()
	}

	if providerName != "" && apiKey == "" {
		// Provider configured — check if credential exists
		fmt.Printf("  %s provider        %s — already configured\n", green.Render("✓"), providerDisplayNames[providerName])
	} else {
		// Need to configure provider
		if providerName == "" {
			providerName = promptProvider()
		}

		if apiKey == "" {
			attempts := 0
			for {
				var err error
				apiKey, err = promptAPIKey(providerName)
				if err != nil {
					return err
				}
				fmt.Print("  Validating... ")
				if err := validateAPIKey(providerName, apiKey); err != nil {
					attempts++
					fmt.Println(red.Render("✗"))
					fmt.Printf("  Key didn't work: %s\n", err)
					if attempts >= 3 {
						fmt.Println("  Having trouble? See https://geoffbelknap.github.io/agency/getting-api-keys")
						return fmt.Errorf("API key validation failed after %d attempts", attempts)
					}
					fmt.Print("  Try again? [Y/n] ")
					scanner := bufio.NewScanner(os.Stdin)
					scanner.Scan()
					if strings.ToLower(strings.TrimSpace(scanner.Text())) == "n" {
						return fmt.Errorf("cancelled")
					}
					continue
				}
				fmt.Println(green.Render("✓"))
				break
			}
		}

		// Initialize config if needed (creates ~/.agency/ structure)
		pendingKeys, err := config.RunInit(config.InitOptions{
			Provider: providerName,
			APIKey:   apiKey,
		})
		if err != nil {
			return fmt.Errorf("config init: %w", err)
		}

		// Store pending keys after daemon is up (Phase 3 will start daemon)
		// Save them for later
		_ = pendingKeys // used after daemon starts in Phase 3

		fmt.Printf("  %s provider        %s\n", green.Render("✓"), providerDisplayNames[providerName])
	}
```

Wait — the pending keys need to be stored after the daemon starts. Let me restructure so Phase 2 captures the keys and Phase 3 stores them after starting the daemon. Update the code to save `pendingKeys` to a variable accessible in Phase 3:

Move the `pendingKeys` variable to the top of `runQuickstart()` and adjust:

```go
func runQuickstart(opts quickstartOptions) error {
	fmt.Println()
	fmt.Println(bold.Render("  Agency Quickstart"))
	fmt.Println()

	var pendingKeys []config.KeyEntry

	// Phase 1: Environment
	// ... (existing code)

	// Phase 2: Provider
	providerName := opts.provider
	apiKey := opts.apiKey

	if providerName == "" {
		providerName = detectProvider()
	}

	if providerName != "" && apiKey == "" {
		fmt.Printf("  %s provider        %s — already configured\n", green.Render("✓"), providerDisplayNames[providerName])
	} else {
		if providerName == "" {
			providerName = promptProvider()
		}

		if apiKey == "" {
			attempts := 0
			for {
				var err error
				apiKey, err = promptAPIKey(providerName)
				if err != nil {
					return err
				}
				fmt.Print("  Validating... ")
				if err := validateAPIKey(providerName, apiKey); err != nil {
					attempts++
					fmt.Println(red.Render("✗"))
					fmt.Printf("  Key didn't work: %s\n", err)
					if attempts >= 3 {
						fmt.Println("  Having trouble? See https://geoffbelknap.github.io/agency/getting-api-keys")
						return fmt.Errorf("API key validation failed after %d attempts", attempts)
					}
					fmt.Print("  Try again? [Y/n] ")
					scanner := bufio.NewScanner(os.Stdin)
					scanner.Scan()
					if strings.ToLower(strings.TrimSpace(scanner.Text())) == "n" {
						return fmt.Errorf("cancelled")
					}
					continue
				}
				fmt.Println(green.Render("✓"))
				break
			}
		}

		keys, err := config.RunInit(config.InitOptions{
			Provider: providerName,
			APIKey:   apiKey,
		})
		if err != nil {
			return fmt.Errorf("config init: %w", err)
		}
		pendingKeys = keys

		fmt.Printf("  %s provider        %s\n", green.Render("✓"), providerDisplayNames[providerName])
	}

	// ... Phase 3 will use pendingKeys
	_ = pendingKeys
```

- [ ] **Step 3: Build and test**

```bash
go build ./cmd/gateway/ && ./agency quickstart
```

Expected: Environment check passes, provider detected as already configured (or prompts if not).

- [ ] **Step 4: Commit**

```bash
git add cmd/gateway/quickstart.go
git commit -m "feat(quickstart): phase 2 — provider detection, prompt, and validation"
```

---

### Task 4: Phase 3 — Infrastructure

**Files:**
- Modify: `cmd/gateway/quickstart.go`

- [ ] **Step 1: Implement Phase 3**

Add after the Phase 2 code:

```go
	// Phase 3: Infrastructure
	cfg := config.Load()
	gatewayAddr := "http://localhost:8200"
	if cfg.GatewayAddr != "" {
		gatewayAddr = "http://" + cfg.GatewayAddr
	}

	c := apiclient.NewClient(gatewayAddr)
	gatewayRunning := c.CheckGateway() == nil

	if gatewayRunning {
		// Check infra health
		infraOK := true
		if status, err := c.Get("/api/v1/infra/status"); err == nil {
			var resp map[string]interface{}
			if json.Unmarshal(status, &resp) == nil {
				if comps, ok := resp["components"].(map[string]interface{}); ok {
					for _, v := range comps {
						comp, ok := v.(map[string]interface{})
						if !ok {
							continue
						}
						if s, _ := comp["status"].(string); s != "running" && s != "healthy" {
							infraOK = false
							break
						}
					}
				}
			}
		} else {
			infraOK = false
		}

		if infraOK {
			fmt.Printf("  %s infrastructure  all services healthy\n", green.Render("✓"))
		} else {
			// Infra partially up — run infra up
			fmt.Printf("  %s infrastructure  Starting services...\n", cyan.Render("●"))
			if err := c.InfraUpStream(func(component, status string) {
				fmt.Printf("    %s %s\n", green.Render("✓"), component)
			}); err != nil {
				return fmt.Errorf("infrastructure startup failed: %w", err)
			}
			fmt.Printf("  %s infrastructure  all services healthy\n", green.Render("✓"))
		}
	} else {
		// Start daemon + infra
		fmt.Printf("  %s infrastructure  Starting services...\n", cyan.Render("●"))

		if err := daemon.Start(8200); err != nil {
			return fmt.Errorf("failed to start daemon: %w", err)
		}

		// Wait for gateway to be reachable
		for i := 0; i < 30; i++ {
			if c.CheckGateway() == nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if c.CheckGateway() != nil {
			return fmt.Errorf("daemon started but gateway not reachable")
		}

		// Store pending credentials now that daemon is up
		if len(pendingKeys) > 0 {
			for _, key := range pendingKeys {
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
				c.CredentialSet(body)
			}
		}

		// Start infrastructure
		fmt.Printf("    %s gateway\n", green.Render("✓"))
		if err := c.InfraUpStream(func(component, status string) {
			fmt.Printf("    %s %s\n", green.Render("✓"), component)
		}); err != nil {
			return fmt.Errorf("infrastructure startup failed: %w", err)
		}
		fmt.Printf("  %s infrastructure  all services healthy\n", green.Render("✓"))
	}
```

- [ ] **Step 2: Build and test**

```bash
go build ./cmd/gateway/ && ./agency quickstart
```

Expected: Phases 1-3 complete. If already running, should skip with checkmarks.

- [ ] **Step 3: Commit**

```bash
git add cmd/gateway/quickstart.go
git commit -m "feat(quickstart): phase 3 — infrastructure detection and startup"
```

---

### Task 5: Phase 4 — Agent creation

**Files:**
- Modify: `cmd/gateway/quickstart.go`

- [ ] **Step 1: Define agent choice mapping**

Add to quickstart.go:

```go
type agentChoice struct {
	label  string
	preset string
	name   string
	task   string
}

var agentChoices = []agentChoice{
	{
		label:  "General assistant — research, write, analyze, code",
		preset: "henry",
		name:   "henry",
		task:   "Look at my current directory and suggest something useful you could help me with.",
	},
	{
		label:  "Security operations — triage alerts, investigate threats, audit posture",
		preset: "engineer",
		name:   "security-analyst",
		task:   "Give me a brief status report on what you're ready to monitor.",
	},
	{
		label:  "Code review — review PRs, find bugs, suggest improvements",
		preset: "code-reviewer",
		name:   "reviewer",
		task:   "Look at my current directory and summarize what this project is.",
	},
	{
		label:  "Research & analysis — deep dives, report writing, data synthesis",
		preset: "researcher",
		name:   "researcher",
		task:   "What are you capable of? Give me three things I should try first.",
	},
}
```

- [ ] **Step 2: Implement Phase 4**

Add after Phase 3:

```go
	// Phase 4: Agent
	// Check if an agent is already running
	var runningAgent string
	if agents, err := c.ListAgents(); err == nil {
		for _, a := range agents {
			if s, _ := a["status"].(string); s == "running" {
				runningAgent, _ = a["name"].(string)
				break
			}
		}
	}

	var choice agentChoice
	if runningAgent != "" {
		fmt.Printf("  %s agent           %s — already running\n", green.Render("✓"), bold.Render(runningAgent))
	} else {
		// Select agent type
		choiceIdx := 0
		if opts.preset != "" {
			// Find matching preset
			for i, ac := range agentChoices {
				if ac.preset == opts.preset || ac.name == opts.preset {
					choiceIdx = i
					break
				}
			}
		} else {
			fmt.Println()
			fmt.Println("  What would you like your first agent to do?")
			fmt.Println()
			for i, ac := range agentChoices {
				fmt.Printf("    %d. %s\n", i+1, ac.label)
			}
			fmt.Println()
			fmt.Print("  Choice [1]: ")

			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			input := strings.TrimSpace(scanner.Text())
			switch input {
			case "2":
				choiceIdx = 1
			case "3":
				choiceIdx = 2
			case "4":
				choiceIdx = 3
			}
		}

		choice = agentChoices[choiceIdx]
		agentName := choice.name
		if opts.name != "" {
			agentName = opts.name
		}

		// Create agent
		if _, err := c.CreateAgent(agentName, choice.preset); err != nil {
			return fmt.Errorf("create agent: %w", err)
		}

		// Start agent with spinner
		s := newSpinner()
		go s.run()
		s.update(fmt.Sprintf("Starting %s", agentName))
		err := c.StartAgentStream(agentName, func(component, status string) {
			s.update(status)
		})
		s.finish()
		if err != nil {
			// Run doctor on failure
			fmt.Printf("  %s agent           start failed: %s\n", red.Render("✗"), err)
			fmt.Println("  Running diagnostics...")
			if doctorOut, doctorErr := c.Get("/api/v1/admin/doctor"); doctorErr == nil {
				fmt.Println(string(doctorOut))
			}
			return fmt.Errorf("agent start failed: %w", err)
		}
		fmt.Printf("  %s agent           %s (%s)\n", green.Render("✓"), bold.Render(agentName), choice.preset)
		runningAgent = agentName
	}
```

- [ ] **Step 3: Build and test**

```bash
go build ./cmd/gateway/ && ./agency quickstart
```

Expected: Phases 1-4 complete. Agent choice prompt appears, agent created and started.

- [ ] **Step 4: Commit**

```bash
git add cmd/gateway/quickstart.go
git commit -m "feat(quickstart): phase 4 — agent selection, creation, and start"
```

---

### Task 6: Phase 5 — Demo task with WebSocket streaming

**Files:**
- Modify: `cmd/gateway/quickstart.go`

This is the magic — send a task and stream the agent's response in real-time.

- [ ] **Step 1: Add WebSocket imports**

Add `"github.com/gorilla/websocket"` and `"net/url"` to the imports.

- [ ] **Step 2: Implement demo streaming**

Add these functions to quickstart.go:

```go
// streamDemoResponse sends a task via DM and streams the agent's response
// via WebSocket. Returns the response text or error. Caps at 60 seconds.
func streamDemoResponse(client *apiclient.Client, baseURL, agentName, task string) (string, error) {
	dmChannel := "dm-" + agentName

	// Ensure DM channel exists
	client.CreateChannel(dmChannel, "DM channel for "+agentName)

	// Connect WebSocket
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/ws"
	header := http.Header{}
	token := client.Token
	if token != "" {
		header.Set("X-Agency-Token", token)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return "", fmt.Errorf("WebSocket connect: %w", err)
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

	// Listen for agent response with timeout
	deadline := time.Now().Add(60 * time.Second)
	var response strings.Builder
	gotResponse := false

	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			if time.Now().After(deadline) {
				break
			}
			continue
		}

		var event map[string]interface{}
		if json.Unmarshal(msgBytes, &event) != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		if eventType != "message" {
			continue
		}

		// Check if this is a message from the agent (not our outgoing message)
		msg, _ := event["message"].(map[string]interface{})
		author, _ := msg["author"].(string)
		if author == "_operator" || author == "" {
			continue
		}

		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}

		if !gotResponse {
			// Clear the "thinking" line
			fmt.Print("\r                              \r")
			gotResponse = true
		}

		response.WriteString(content)

		// Print content with indentation
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			fmt.Printf("  %s\n", line)
		}
		break // DM responses come as a single message
	}

	if !gotResponse {
		return "", fmt.Errorf("timeout")
	}

	return response.String(), nil
}
```

- [ ] **Step 3: Wire Phase 5 into runQuickstart**

Add after Phase 4:

```go
	// Phase 5: Demo
	if opts.noDemo || runningAgent == "" {
		// Skip demo
	} else {
		demoTask := choice.task
		if demoTask == "" {
			// Agent was already running, no choice made — use generic task
			demoTask = "What are you capable of? Give me three things I should try first."
		}

		fmt.Println()
		fmt.Println("  Your agent is ready. Let's try it out:")
		fmt.Println()
		fmt.Printf("  > %s is thinking...", bold.Render(runningAgent))

		_, err := streamDemoResponse(c, gatewayAddr, runningAgent, demoTask)
		if err != nil {
			fmt.Println()
			fmt.Println()
			fmt.Printf("  Agent started but the first task is taking a while.\n")
			fmt.Printf("  Check %s or open %s\n", bold.Render("agency status"), bold.Render("http://localhost:8280"))
		}
	}

	// Print "What's next" footer
	fmt.Println()
	fmt.Println("  " + dim.Render("────────────────────────────────────────"))
	fmt.Printf("  Agent is running. What's next:\n")
	fmt.Printf("    • Send tasks:  %s\n", bold.Render(fmt.Sprintf("agency send %s \"your task here\"", runningAgent)))
	fmt.Printf("    • Web UI:      %s\n", bold.Render("http://localhost:8280"))
	fmt.Printf("    • Status:      %s\n", bold.Render("agency status"))
	fmt.Printf("    • More agents: %s\n", bold.Render("agency hub search"))

	// Choice-specific suggestions
	if choice.preset == "engineer" {
		fmt.Printf("    • Full team:   %s\n", bold.Render("agency hub install security-ops"))
	}
	if choice.preset == "code-reviewer" {
		fmt.Printf("    • Review PRs:  %s\n", bold.Render(fmt.Sprintf("agency send %s \"review my latest commit\"", runningAgent)))
	}
	fmt.Println()

	return nil
```

- [ ] **Step 4: Build and test**

```bash
go build ./cmd/gateway/ && ./agency quickstart
```

Expected: Full 5-phase flow. Demo sends task and streams response.

- [ ] **Step 5: Commit**

```bash
git add cmd/gateway/quickstart.go
git commit -m "feat(quickstart): phase 5 — demo task with WebSocket response streaming"
```

---

### Task 7: Error handling and Ctrl-C cleanup

**Files:**
- Modify: `cmd/gateway/quickstart.go`

- [ ] **Step 1: Add signal handling for clean Ctrl-C exit**

Add at the top of `runQuickstart()`:

```go
	// Handle Ctrl-C gracefully — completed phases stay completed
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\n\n  Interrupted. Completed phases are saved — run `agency quickstart` again to resume.")
		cancel()
		os.Exit(0)
	}()
	_ = ctx // available for future context-aware operations
```

Add `"context"`, `"os/signal"` to imports.

- [ ] **Step 2: Build and test**

```bash
go build ./cmd/gateway/ && ./agency quickstart
```

Test: Press Ctrl-C during provider prompt — should exit cleanly with message.

- [ ] **Step 3: Commit**

```bash
git add cmd/gateway/quickstart.go
git commit -m "feat(quickstart): graceful Ctrl-C handling"
```

---

### Task 8: Update documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add quickstart documentation**

Find the bullet about `agency setup` in CLAUDE.md and add a quickstart bullet after it:

```
- **`agency quickstart`** is the guided first-run wizard. Gets new users from zero to a running agent with a demo task in under 10 minutes. 5 phases: environment check, provider config, infrastructure startup, agent creation, live demo. Each phase auto-skips if already done. `agency setup` is still the idempotent infrastructure command — quickstart is the hand-holding experience.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document agency quickstart command"
```

---

### Task 9: Push and create PR

- [ ] **Step 1: Run tests**

```bash
go test ./... 2>&1 | tail -10
go build ./cmd/gateway/
```

Expected: All pass, clean build.

- [ ] **Step 2: Create branch and push**

```bash
git checkout -b feature/quickstart-wizard
git push -u origin feature/quickstart-wizard
```

- [ ] **Step 3: Create PR**

```bash
gh pr create --title "feat: agency quickstart wizard" --body "$(cat <<'EOF'
## Summary

New `agency quickstart` command — guided 5-phase wizard:

1. **Environment** — Docker check with platform-specific guidance
2. **Provider** — detect existing, prompt if needed, validate API key, store credential
3. **Infrastructure** — start daemon + infra, skip if already running
4. **Agent** — choice of 4 presets, create + 7-phase start with spinner
5. **Demo** — send contextual task, stream response via WebSocket

Each phase auto-detects completion and skips. Ctrl-C exits cleanly with resume guidance.

## CLI flags

`--provider`, `--key`, `--preset`, `--name`, `--no-demo`, `--verbose` for scripted usage.

## Test plan

- [ ] Clean machine: `rm -rf ~/.agency && agency quickstart` — full flow
- [ ] Existing setup: `agency quickstart` — phases skip to demo
- [ ] Bad API key: retry loop, 3-strike guidance link
- [ ] Ctrl-C: clean exit at any phase
- [ ] `agency quickstart --provider anthropic --key sk-... --preset henry --no-demo` — scripted mode

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
