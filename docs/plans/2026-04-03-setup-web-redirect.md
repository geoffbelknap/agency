# Setup Web-Redirect Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `agency setup` redirect to the web UI wizard by default, preserving the full interactive CLI flow behind `--cli`.

**Architecture:** Add a `--cli` flag to the setup command. When not set, skip interactive prompts and open the browser to `http://<host>:8280/setup` after infra is up. Extract the web-host derivation into a reusable helper. Add an `openBrowser` helper for cross-platform browser launch.

**Tech Stack:** Go, Cobra, `os/exec`, `runtime.GOOS`

---

### Task 1: Add `openBrowser` helper

**Files:**
- Modify: `cmd/gateway/main.go` (insert after `isWSL()` at line ~426)

- [ ] **Step 1: Add the `openBrowser` function**

Insert after the closing brace of `isWSL()` (line 426):

```go
// openBrowser attempts to open url in the system default browser.
// Best-effort — returns an error but callers should ignore it.
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch {
	case runtime.GOOS == "darwin":
		cmd = "open"
		args = []string{url}
	case isWSL():
		// Try wslview first (wslu package), fall back to cmd.exe
		if _, err := exec.LookPath("wslview"); err == nil {
			cmd = "wslview"
			args = []string{url}
		} else {
			cmd = "cmd.exe"
			args = []string{"/c", "start", url}
		}
	default: // linux, freebsd, etc.
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/gateway/`
Expected: success, no errors

- [ ] **Step 3: Commit**

```
git add cmd/gateway/main.go
git commit -m "feat(setup): add openBrowser helper for cross-platform browser launch"
```

---

### Task 2: Add `webHost` helper

**Files:**
- Modify: `cmd/gateway/main.go` (extract from `runSetup` lines 586-596, insert after `openBrowser`)

The web-host derivation logic is currently inline in `runSetup`. Extract it so both the default and `--cli` paths can use it.

- [ ] **Step 1: Add the `webHost` function**

Insert after `openBrowser`:

```go
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
```

- [ ] **Step 2: Replace the inline derivation in `runSetup`**

Replace lines 585-597 (the `webHost` variable declaration through the `fmt.Printf` for the web UI) with:

```go
	fmt.Printf("  Open http://%s:8280 for the web UI\n", webHost())
```

Remove the now-unused `noInfra` parameter dependency from the web-host block. The `noInfra` guard was protecting against loading config when infra wasn't started, but `config.Load()` reads a file — it doesn't need infra running. The extracted `webHost()` function is safe to call regardless.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./cmd/gateway/`
Expected: success, no errors

- [ ] **Step 4: Commit**

```
git add cmd/gateway/main.go
git commit -m "refactor(setup): extract webHost helper from runSetup"
```

---

### Task 3: Add `--cli` flag and split the setup flow

**Files:**
- Modify: `cmd/gateway/main.go` — `setupCmd()` (lines 276-357) and `runSetup()` (lines 476-600)

- [ ] **Step 1: Add `--cli` flag to `setupCmd`**

In the `var` block at line 277, add:

```go
		cliMode  bool
```

At line 354, add the flag registration:

```go
	cmd.Flags().BoolVar(&cliMode, "cli", false, "Run full interactive setup in the terminal")
```

- [ ] **Step 2: Gate the interactive prompts behind `--cli`**

Wrap the interactive provider/key block (lines 297-343) so it only runs when `cliMode` is true. Replace lines 297-345 with:

```go
			if cliMode {
				// Quick setup: if --name or --preset flags are set, skip prompts
				if name != "" || preset != "" {
					return runSetup(provider, apiKey, notifyURL, noInfra, true)
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

				return runSetup(provider, apiKey, notifyURL, noInfra, true)
			}

			// Default: web-assisted setup — no prompts
			return runSetup("", "", notifyURL, noInfra, false)
```

- [ ] **Step 3: Add `cliMode` parameter to `runSetup` and split the output**

Change the `runSetup` signature from:

```go
func runSetup(provider, apiKey, notifyURL string, noInfra bool) error {
```

to:

```go
func runSetup(provider, apiKey, notifyURL string, noInfra, cliMode bool) error {
```

Replace the "You're ready to go" output block (lines 578-597) with:

```go
	fmt.Println()
	if cliMode {
		fmt.Println("You're ready to go:")
		fmt.Println()
		fmt.Println("  agency create my-agent  # Create an agent")
		fmt.Println("  agency start my-agent   # Start an agent")
		fmt.Println("  agency status           # Check platform status")
		fmt.Println()
		fmt.Printf("  Open http://%s:8280 for the web UI\n", webHost())
	} else {
		setupURL := fmt.Sprintf("http://%s:8280/setup", webHost())
		_ = openBrowser(setupURL)
		fmt.Printf("Finish setup at: %s\n", setupURL)
	}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./cmd/gateway/`
Expected: success, no errors

- [ ] **Step 5: Commit**

```
git add cmd/gateway/main.go
git commit -m "feat(setup): default to web-assisted setup, add --cli for interactive mode"
```

---

### Task 4: Manual smoke test

No automated tests exist for the setup command (it requires Docker, filesystem, and daemon). Verify manually.

- [ ] **Step 1: Build**

Run: `go build -o agency ./cmd/gateway/`
Expected: success

- [ ] **Step 2: Test `--help` shows new flag**

Run: `./agency setup --help`
Expected: output includes `--cli` flag with description "Run full interactive setup in the terminal"

- [ ] **Step 3: Test default setup flow (if Docker available)**

Run: `./agency setup`
Expected: no provider prompts, infra starts, browser opens (or URL printed), output ends with `Finish setup at: http://localhost:8280/setup`

- [ ] **Step 4: Test `--cli` flow (if Docker available)**

Run: `./agency setup --cli`
Expected: interactive provider menu appears, same behavior as before

- [ ] **Step 5: Test `--no-infra` with default flow**

Run: `./agency setup --no-infra`
Expected: no Docker check, no infra startup, browser opens to setup URL, URL printed

- [ ] **Step 6: Commit build cleanup**

Remove the built binary if created in the repo directory:
```
rm -f ./agency
```
