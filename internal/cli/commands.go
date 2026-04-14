package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/apiclient"
	authzcore "github.com/geoffbelknap/agency/internal/authz"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/daemon"
	"github.com/geoffbelknap/agency/internal/features"
	"github.com/geoffbelknap/agency/internal/update"
)

var (
	bold   = lipgloss.NewStyle().Bold(true)
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	quiet bool // suppress spinners and progress animations
)

func IsExperimentalCommand(name string) bool {
	return features.CommandIsExperimental(name)
}

func hideWhenExperimentalDisabled(cmd *cobra.Command) *cobra.Command {
	if !features.CommandVisible(cmd.Name()) {
		cmd.Hidden = true
	}
	return cmd
}

// spinner displays an animated spinner with a status message on the current line.
type spinner struct {
	mu     sync.Mutex
	msg    string
	stop   chan struct{}
	done   chan struct{}
	frames []string
}

func newSpinner() *spinner {
	return &spinner{
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// update finishes the current step with a checkmark and starts spinning on the next.
func (s *spinner) update(status string) {
	s.mu.Lock()
	prev := s.msg
	s.msg = status
	s.mu.Unlock()
	if prev != "" {
		fmt.Printf("\r  %s %s\n", green.Render("✓"), prev)
	}
}

// run starts the spinner animation. Call in a goroutine.
func (s *spinner) run() {
	defer close(s.done)
	if quiet {
		<-s.stop
		return
	}
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
				frame := cyan.Render(s.frames[i%len(s.frames)])
				fmt.Printf("\r  %s %s", frame, msg)
			}
			i++
		}
	}
}

// finish stops the spinner and prints the final step with a checkmark.
func (s *spinner) finish() {
	s.mu.Lock()
	msg := s.msg
	s.msg = ""
	s.mu.Unlock()
	close(s.stop)
	<-s.done
	if msg != "" {
		fmt.Printf("\r  %s %s\n", green.Render("✓"), msg)
	}
}

func gatewayURL() string {
	url := os.Getenv("AGENCY_GATEWAY_URL")
	if url != "" {
		return url
	}
	return "http://" + config.Load().GatewayAddr
}

func gatewayPort() int {
	if _, port, err := net.SplitHostPort(config.Load().GatewayAddr); err == nil {
		if p, err := strconv.Atoi(port); err == nil {
			return p
		}
	}
	return 8200
}

func newClient() *Client {
	return NewClient(gatewayURL())
}

// cliVersion is set by RegisterCommands from the root cobra command's version.
var cliVersion string
var cliBuildID string

// requireGateway creates a client and checks connectivity.
// If the gateway is not reachable, it attempts to auto-start the daemon.
// If the daemon is running an older version, it auto-restarts.
func requireGateway() (*Client, error) {
	c := newClient()
	if err := c.CheckGateway(); err == nil {
		// Connected — check for version mismatch
		checkDaemonVersion(c)
		return c, nil
	}

	// Gateway not reachable — try to auto-start the daemon
	fmt.Println("Daemon not running, starting...")
	if err := daemon.EnsureRunning(gatewayPort()); err != nil {
		return nil, fmt.Errorf("gateway not running and auto-start failed: %w\nStart manually with: agency serve", err)
	}

	// Retry connection after daemon start
	if err := c.CheckGateway(); err != nil {
		return nil, fmt.Errorf("daemon started but gateway still not reachable: %w", err)
	}
	return c, nil
}

// checkDaemonVersion compares the CLI version/build with the running daemon.
// If they differ, it auto-restarts the daemon so upgrades take effect immediately.
func checkDaemonVersion(c *Client) {
	if cliVersion == "" || cliVersion == "dev" {
		return
	}
	health, err := c.Health()
	if err != nil {
		return
	}
	daemonVersion, _ := health["version"].(string)
	daemonBuildID, _ := health["build_id"].(string)
	versionMatch := daemonVersion != "" && daemonVersion == cliVersion
	buildMatch := cliBuildID == "" || daemonBuildID == cliBuildID
	if versionMatch && buildMatch {
		return
	}
	fmt.Fprintf(
		os.Stderr,
		"Daemon build mismatch (daemon: %s/%s, cli: %s/%s). Restarting daemon...\n",
		daemonVersion,
		daemonBuildID,
		cliVersion,
		cliBuildID,
	)
	if err := daemon.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not stop old daemon: %v\n", err)
		return
	}
	time.Sleep(500 * time.Millisecond)
	if err := daemon.Start(gatewayPort()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not restart daemon: %v\n", err)
		return
	}
	// Wait for new daemon to be ready
	for i := 0; i < 10; i++ {
		time.Sleep(300 * time.Millisecond)
		if err := c.CheckGateway(); err == nil {
			fmt.Fprintf(os.Stderr, "Daemon restarted.\n")
			return
		}
	}
}

// RegisterCommands adds all CLI subcommands to the root cobra command.
func RegisterCommands(root *cobra.Command) {
	root.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress spinners and progress animations")

	// Extract semver from root version string ("0.1.1 (abc1234, ...)" → "0.1.1")
	if v := root.Version; v != "" {
		if idx := strings.IndexByte(v, ' '); idx > 0 {
			cliVersion = v[:idx]
			if open := strings.IndexByte(v, '('); open >= 0 {
				rest := v[open+1:]
				if comma := strings.IndexByte(rest, ','); comma > 0 {
					cliBuildID = strings.TrimSpace(rest[:comma])
				}
			}
		} else {
			cliVersion = v
		}
	}

	// Background update check — starts immediately, result printed after command runs
	agencyHome := os.Getenv("AGENCY_HOME")
	if agencyHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			agencyHome = home + "/.agency"
		}
	}
	var waitForUpdate func() *update.Result
	if cliVersion != "" && cliVersion != "dev" {
		waitForUpdate = update.Check(cliVersion, agencyHome)
	}
	root.PersistentPostRun = func(cmd *cobra.Command, args []string) {
		if waitForUpdate == nil {
			return
		}
		if r := waitForUpdate(); r != nil && r.Newer() {
			fmt.Fprintf(os.Stderr, "\nA new version of agency is available: %s → %s\n", r.Current, r.Latest)
			fmt.Fprintf(os.Stderr, "  brew upgrade agency\n\n")
		}
	}

	// Define command groups for organized help output
	root.AddGroup(
		&cobra.Group{ID: "daily", Title: "Daily Operations:"},
		&cobra.Group{ID: "agent", Title: "Agent Lifecycle:"},
		&cobra.Group{ID: "manage", Title: "Management:"},
	)

	// ── Daily operations — flat top-level verbs ──────────────────────────
	for _, cmd := range []*cobra.Command{
		startCmd(), stopCmd(), restartCmd(), sendCmd(), statusCmd(), showCmd(), listCmd(), logCmd(),
	} {
		cmd.GroupID = "daily"
		root.AddCommand(cmd)
	}

	// ── Agent lifecycle — top-level, less frequent ───────────────────────
	for _, cmd := range []*cobra.Command{
		createCmd(), deleteCmd(), haltCmd(), resumeCmd(), grantCmd(), revokeCmd(),
	} {
		cmd.GroupID = "agent"
		root.AddCommand(cmd)
	}

	// ── Grouped subcommands ─────────────────────────────────────────────
	for _, cmd := range []*cobra.Command{
		channelCmd(), infraCmd(), hideWhenExperimentalDisabled(hubCmd()), hideWhenExperimentalDisabled(teamCmd()), capCmd(),
		hideWhenExperimentalDisabled(intakeCmd()), knowledgeCmd(), policyCmd(), adminCmd(),
		contextCmd(), hideWhenExperimentalDisabled(missionCmd()), hideWhenExperimentalDisabled(eventCmd()),
		hideWhenExperimentalDisabled(webhookCmd()), hideWhenExperimentalDisabled(meeseeksCmd()),
		hideWhenExperimentalDisabled(notificationsCmd()), auditCmd(),
		credentialCmd(), hideWhenExperimentalDisabled(cacheCmd()), hideWhenExperimentalDisabled(registryCmd()),
		hideWhenExperimentalDisabled(packageCmd()), hideWhenExperimentalDisabled(instanceCmd()), hideWhenExperimentalDisabled(authzCmd()),
	} {
		cmd.GroupID = "manage"
		root.AddCommand(cmd)
	}

	// ── Integration ─────────────────────────────────────────────────────
	root.AddCommand(mcpServerCmd())
	root.AddCommand(runtimeAuthorityServeCmd())
}

func packageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "package",
		Short: "Inspect V2 packages",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List installed V2 packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			items, err := c.ListPackages(cmd.Context())
			if err != nil {
				return err
			}
			for _, item := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", item.Kind, item.Name, item.Version, item.Trust)
			}
			return nil
		},
	})
	return cmd
}

func instanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Inspect V2 instances",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List local V2 instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			items, err := c.ListInstances(cmd.Context())
			if err != nil {
				return err
			}
			for _, item := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", item.ID, item.Name)
			}
			return nil
		},
	})
	var fromPackageKind, fromPackageName, fromPackageInstanceName, fromPackageNodeID string
	var fromPackageConfigJSON, fromPackageNodeConfigJSON string
	createFromPackageCmd := &cobra.Command{
		Use:   "create-from-package",
		Short: "Scaffold a V2 instance from an installed package",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if strings.TrimSpace(fromPackageKind) == "" || strings.TrimSpace(fromPackageName) == "" {
				return fmt.Errorf("--kind and --package are required")
			}
			body := map[string]any{
				"kind": fromPackageKind,
				"name": fromPackageName,
			}
			if strings.TrimSpace(fromPackageInstanceName) != "" {
				body["instance_name"] = fromPackageInstanceName
			}
			if strings.TrimSpace(fromPackageNodeID) != "" {
				body["node_id"] = fromPackageNodeID
			}
			if strings.TrimSpace(fromPackageConfigJSON) != "" {
				var cfg map[string]any
				if err := json.Unmarshal([]byte(fromPackageConfigJSON), &cfg); err != nil {
					return fmt.Errorf("--config must be valid JSON object: %w", err)
				}
				body["config"] = cfg
			}
			if strings.TrimSpace(fromPackageNodeConfigJSON) != "" {
				var cfg map[string]any
				if err := json.Unmarshal([]byte(fromPackageNodeConfigJSON), &cfg); err != nil {
					return fmt.Errorf("--node-config must be valid JSON object: %w", err)
				}
				body["node_config"] = cfg
			}
			inst, err := c.CreateInstanceFromPackage(cmd.Context(), body)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(inst)
		},
	}
	createFromPackageCmd.Flags().StringVar(&fromPackageKind, "kind", "", "Installed package kind")
	createFromPackageCmd.Flags().StringVar(&fromPackageName, "package", "", "Installed package name")
	createFromPackageCmd.Flags().StringVar(&fromPackageInstanceName, "name", "", "Instance name override")
	createFromPackageCmd.Flags().StringVar(&fromPackageNodeID, "node-id", "", "Node ID override")
	createFromPackageCmd.Flags().StringVar(&fromPackageConfigJSON, "config", "", "JSON object to merge into instance config")
	createFromPackageCmd.Flags().StringVar(&fromPackageNodeConfigJSON, "node-config", "", "JSON object to merge into scaffolded node config")
	cmd.AddCommand(createFromPackageCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "show <instance>",
		Short: "Show a V2 instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			inst, err := c.ShowInstance(cmd.Context(), instanceID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(inst)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "validate <instance>",
		Short: "Validate a V2 instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			result, err := c.ValidateInstance(cmd.Context(), instanceID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	})
	var updateJSON string
	updateCmd := &cobra.Command{
		Use:   "update <instance>",
		Short: "Patch a V2 instance with a JSON object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(updateJSON) == "" {
				return fmt.Errorf("--json is required")
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(updateJSON), &body); err != nil {
				return fmt.Errorf("--json must be valid JSON object: %w", err)
			}
			result, err := c.UpdateInstance(cmd.Context(), instanceID, body)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	updateCmd.Flags().StringVar(&updateJSON, "json", "", "JSON object patch body")
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "apply <instance>",
		Short: "Compile, refresh, and reconcile a V2 instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			result, err := c.ApplyInstance(cmd.Context(), instanceID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	})
	runtimeCmd := &cobra.Command{
		Use:   "runtime",
		Short: "Manage V2 instance runtime state",
	}
	runtimeCmd.AddCommand(&cobra.Command{
		Use:   "manifest <instance>",
		Short: "Show the current runtime manifest for an instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			result, err := c.ShowRuntimeManifest(cmd.Context(), instanceID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	})
	runtimeCmd.AddCommand(&cobra.Command{
		Use:   "compile <instance>",
		Short: "Compile and persist a runtime manifest for an instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			result, err := c.CompileRuntimeManifest(cmd.Context(), instanceID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	})
	runtimeCmd.AddCommand(&cobra.Command{
		Use:   "reconcile <instance>",
		Short: "Reconcile an instance runtime manifest into runtime state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			result, err := c.ReconcileRuntimeManifest(cmd.Context(), instanceID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	})
	runtimeCmd.AddCommand(&cobra.Command{
		Use:   "start <instance> <node>",
		Short: "Start an authority runtime node",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			result, err := c.StartRuntimeNode(cmd.Context(), instanceID, args[1])
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	})
	runtimeCmd.AddCommand(&cobra.Command{
		Use:   "stop <instance> <node>",
		Short: "Stop an authority runtime node",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			result, err := c.StopRuntimeNode(cmd.Context(), instanceID, args[1])
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	})
	var subject, action, inputJSON string
	var consent bool
	invokeCmd := &cobra.Command{
		Use:   "invoke <instance> <node>",
		Short: "Invoke an active authority runtime node",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			instanceID, err := resolveInstanceRef(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			input := map[string]any{}
			if strings.TrimSpace(inputJSON) != "" {
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					return fmt.Errorf("--input must be valid JSON object: %w", err)
				}
			}
			result, err := c.InvokeRuntimeNode(cmd.Context(), instanceID, args[1], map[string]any{
				"subject":          subject,
				"node_id":          args[1],
				"action":           action,
				"consent_provided": consent,
				"input":            input,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	invokeCmd.Flags().StringVar(&subject, "subject", "", "invoking subject")
	invokeCmd.Flags().StringVar(&action, "action", "", "authority action to invoke")
	invokeCmd.Flags().StringVar(&inputJSON, "input", "", "JSON object input payload")
	invokeCmd.Flags().BoolVar(&consent, "consent", false, "mark consent as already provided")
	runtimeCmd.AddCommand(invokeCmd)
	cmd.AddCommand(runtimeCmd)
	return cmd
}

func resolveInstanceRef(ctx context.Context, c *apiclient.Client, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("instance reference is required")
	}
	items, err := c.ListInstances(ctx)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.ID == ref {
			return item.ID, nil
		}
	}
	matches := []string{}
	for _, item := range items {
		if item.Name == ref {
			matches = append(matches, item.ID)
		}
	}
	switch len(matches) {
	case 0:
		return ref, nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple instances named %q; use an instance ID", ref)
	}
}

func authzCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authz",
		Short: "Resolve V2 authz requests",
	}
	var subject, target, action, instance string
	var consent bool
	resolveCmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve an authorization request",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			decision, err := c.ResolveAuthz(cmd.Context(), authzcore.Request{
				Subject:         subject,
				Target:          target,
				Action:          action,
				Instance:        instance,
				ConsentProvided: consent,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(decision)
		},
	}
	resolveCmd.Flags().StringVar(&subject, "subject", "", "request subject")
	resolveCmd.Flags().StringVar(&target, "target", "", "request target")
	resolveCmd.Flags().StringVar(&action, "action", "", "requested action")
	resolveCmd.Flags().StringVar(&instance, "instance", "", "instance scope")
	resolveCmd.Flags().BoolVar(&consent, "consent", false, "mark consent as already provided")
	cmd.AddCommand(resolveCmd)
	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Daily operations — flat top-level verbs
// ════════════════════════════════════════════════════════════════════════════

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <agent>",
		Short: "Start an agent (7-phase sequence)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			s := newSpinner()
			go s.run()
			s.update(fmt.Sprintf("Starting %s", args[0]))
			err = c.StartAgentStream(args[0], func(component, status string) {
				s.update(status)
			})
			s.finish()
			if err != nil {
				return err
			}
			fmt.Printf("%s %s started\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
}

func stopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "stop <agent>",
		Short: "Stop an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if force {
				if _, err := c.HaltAgent(args[0], "immediate", "force stop"); err != nil {
					// Ignore halt errors (agent may already be stopped/halted)
					_ = err
				}
			}
			if err := c.StopAgent(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Agent %s stopped\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "halt immediately before stopping (for stuck agents)")
	return cmd
}

func restartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <agent>",
		Short: "Restart an agent (stop + full start sequence)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			s := newSpinner()
			go s.run()
			s.update(fmt.Sprintf("Restarting %s", args[0]))
			_, err = c.RestartAgent(args[0])
			s.finish()
			if err != nil {
				return err
			}
			fmt.Printf("%s %s restarted\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
}

func sendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send <agent-or-channel> <message>",
		Short: "Send a message to an agent (DM) or channel",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			target := args[0]
			content := strings.Join(args[1:], " ")
			report, _ := cmd.Flags().GetBool("report")

			// DM routing: if target matches an agent name, route to dm-{agent} channel
			channel := target
			isDM := false
			agents, agentErr := c.ListAgents()
			if agentErr == nil {
				for _, a := range agents {
					if name, ok := a["name"].(string); ok && name == target {
						channel = "dm-" + target
						isDM = true
						break
					}
				}
			}

			var metadata map[string]interface{}
			if report {
				metadata = map[string]interface{}{"report": true}
			}
			if _, err := c.SendMessageWithMetadata(channel, content, metadata); err != nil {
				return err
			}
			if isDM {
				fmt.Printf("%s Message sent to %s (via DM)\n", green.Render("✓"), bold.Render(target))
			} else {
				fmt.Printf("%s Message sent to %s\n", green.Render("✓"), bold.Render(target))
			}
			return nil
		},
	}
	cmd.Flags().Bool("report", false, "Request a report from the agent (sets report metadata)")
	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show platform overview",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}

			// No args: show overview (infra + agents summary)
			infraResp, _ := c.InfraStatus()
			if infraResp != nil && infraResp.Version != "" {
				buildDate := time.Now().Format("2006-01-02")
				fmt.Printf("Agency v%s (%s, %s)\n", infraResp.Version, infraResp.BuildID, buildDate)
				if infraResp.GatewayURL != "" {
					fmt.Printf("  Gateway: %s\n", infraResp.GatewayURL)
				}
				if infraResp.WebURL != "" {
					fmt.Printf("  Web UI:  %s\n", infraResp.WebURL)
				}
				if infraResp.Docker == "unavailable" {
					fmt.Printf("  Docker:  %s\n", red.Render("unavailable"))
				}
				fmt.Println()
			}
			if infraResp != nil && infraResp.InfraLLMDailyUsed > 0 {
				fmt.Printf("Infrastructure LLM: $%.2f / $%.2f today\n\n", infraResp.InfraLLMDailyUsed, infraResp.InfraLLMDailyLimit)
			}
			fmt.Println(bold.Render("Infrastructure:"))
			if infraResp != nil {
				gatewayBuild := infraResp.BuildID
				for _, ic := range infraResp.Components {
					icon := green.Render("●")
					if infraResp.Docker == "unavailable" {
						icon = dim.Render("?")
					} else if ic["health"] != "healthy" && ic["state"] != "running" {
						icon = red.Render("○")
					}
					buildCol := ""
					if bid := ic["build_id"]; bid != "" {
						if bid == gatewayBuild {
							buildCol = green.Render(bid + " ✓")
						} else {
							buildCol = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(bid + " ⚠ stale")
						}
					}
					fmt.Printf("  %s %s %s %s\n", icon, bold.Render(fmt.Sprintf("%-14s", ic["name"])), dim.Render(fmt.Sprintf("%-10s", ic["state"])), buildCol)
				}
			}

			agents, _ := c.ListAgents()
			// Fetch active Meeseeks for observability (best-effort)
			allMeeseeks, _ := c.MeeseeksList("")
			meeseeksByParent := make(map[string][]map[string]interface{})
			for _, m := range allMeeseeks {
				parent, _ := m["parent_agent"].(string)
				if parent != "" {
					meeseeksByParent[parent] = append(meeseeksByParent[parent], m)
				}
			}

			fmt.Printf("\n%s\n", bold.Render("Agents:"))
			if len(agents) == 0 {
				fmt.Println(dim.Render("  No agents"))
			} else {
				fmt.Printf("  %s %s %s %s %s\n",
					bold.Render(fmt.Sprintf("%-20s", "Name")),
					bold.Render(fmt.Sprintf("%-12s", "Status")),
					bold.Render(fmt.Sprintf("%-12s", "Enforcer")),
					bold.Render(fmt.Sprintf("%-16s", "Build")),
					bold.Render("Mission"))
				fmt.Println(dim.Render("  " + strings.Repeat("─", 78)))
				gatewayBuild := ""
				if infraResp != nil {
					gatewayBuild = infraResp.BuildID
				}
				for _, a := range agents {
					name, _ := a["name"].(string)
					status, _ := a["status"].(string)
					enforcer, _ := a["enforcer"].(string)
					buildID, _ := a["build_id"].(string)
					buildCol := ""
					if buildID != "" {
						if buildID == gatewayBuild {
							buildCol = green.Render(buildID + " ✓")
						} else {
							buildCol = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(buildID + " ⚠ stale")
						}
					}
					missionCol := ""
					if m, _ := a["mission"].(string); m != "" {
						ms, _ := a["mission_status"].(string)
						missionCol = fmt.Sprintf("%s (%s)", m, ms)
					}
					fmt.Printf("  %s %s %s %s %s\n",
						bold.Render(fmt.Sprintf("%-20s", name)), renderStatePadded(status, 12), renderStatePadded(enforcer, 12), buildCol, dim.Render(missionCol))

					// Show active Meeseeks nested under their parent agent
					if mksList, ok := meeseeksByParent[name]; ok {
						for i, mks := range mksList {
							mksID, _ := mks["id"].(string)
							mksStatus, _ := mks["status"].(string)
							mksTask, _ := mks["task"].(string)
							if len(mksTask) > 36 {
								mksTask = mksTask[:33] + "..."
							}
							connector := "├"
							if i == len(mksList)-1 {
								connector = "└"
							}
							fmt.Printf("    %s %s  %s  %s\n",
								dim.Render(connector),
								cyan.Render(fmt.Sprintf("%-14s", mksID)),
								renderStatePadded(mksStatus, 10),
								dim.Render(fmt.Sprintf("%q", mksTask)),
							)
						}
					}
				}
			}
			return nil
		},
	}
}

func listCmd() *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			agents, err := c.ListAgents()
			if err != nil {
				return err
			}
			return writeAgentList(os.Stdout, agents, outputFormat)
		},
	}
	cmd.Flags().StringVarP(&outputFormat, "format", "f", "table", "Output format: table, text, or json")
	return cmd
}

func writeAgentList(w io.Writer, agents []map[string]interface{}, outputFormat string) error {
	switch outputFormat {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(agents)
	case "text":
		for _, a := range agents {
			name, _ := a["name"].(string)
			status, _ := a["status"].(string)
			preset, _ := a["preset"].(string)
			lastActive, _ := a["last_active"].(string)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, status, preset, lastActive)
		}
		return nil
	case "table":
	default:
		return fmt.Errorf("unsupported format %q (expected table, text, or json)", outputFormat)
	}
	if len(agents) == 0 {
		fmt.Fprintln(w, "No agents")
		return nil
	}
	fmt.Fprintf(w, "  %-20s %-12s %-12s %-20s\n", "NAME", "STATUS", "PRESET", "LAST ACTIVE")
	for _, a := range agents {
		name, _ := a["name"].(string)
		status, _ := a["status"].(string)
		preset, _ := a["preset"].(string)
		lastActive, _ := a["last_active"].(string)

		icon := dim.Render("○")
		if status == "running" {
			icon = green.Render("●")
		} else if status == "paused" {
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("●")
		}
		fmt.Fprintf(w, "%s %-20s %-12s %-12s %-20s\n", icon, bold.Render(name), status, preset, dim.Render(lastActive))
	}
	return nil
}

func logCmd() *cobra.Command {
	var since, until string
	cmd := &cobra.Command{
		Use:   "log <agent>",
		Short: "Show agent audit log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			events, err := c.AgentLogs(args[0], since, until)
			if err != nil {
				// No audit logs yet is not an error
				fmt.Printf("  No audit logs for %s yet.\n", args[0])
				return nil
			}
			if len(events) == 0 {
				fmt.Printf("  No audit logs for %s yet.\n", args[0])
				return nil
			}
			for _, e := range events {
				ts, _ := e["timestamp"].(string)
				event, _ := e["event"].(string)
				if ts == "" {
					ts, _ = e["ts"].(string)
				}
				if event == "" {
					event, _ = e["type"].(string)
				}
				detail := formatEventDetail(event, e)
				if ts != "" {
					tsDisplay := ts
					if len(ts) >= 19 {
						tsDisplay = ts[:19]
					}
					fmt.Printf("  %s  %s %s\n", dim.Render(tsDisplay), cyan.Render(fmt.Sprintf("%-20s", event)), detail)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "Show logs since (ISO timestamp)")
	cmd.Flags().StringVar(&until, "until", "", "Show logs until (ISO timestamp)")
	return cmd
}

// formatEventDetail extracts a human-readable detail string from an audit log event.
func formatEventDetail(event string, e map[string]interface{}) string {
	str := func(key string) string {
		if v, ok := e[key].(string); ok {
			return v
		}
		return ""
	}
	num := func(key string) int {
		if v, ok := e[key].(float64); ok {
			return int(v)
		}
		return 0
	}

	switch event {
	case "start_phase":
		phaseName := str("phase_name")
		phase := num("phase")
		trigger := str("trigger")
		if phaseName != "" {
			s := fmt.Sprintf("phase %d: %s", phase, phaseName)
			if trigger != "" {
				s += fmt.Sprintf(" (%s)", trigger)
			}
			return s
		}
	case "start_failed", "restart_failed":
		if reason := str("error"); reason != "" {
			return reason
		}
	case "agent_halted":
		parts := []string{}
		if t := str("type"); t != "" {
			parts = append(parts, t)
		}
		if r := str("reason"); r != "" {
			parts = append(parts, r)
		}
		if i := str("initiator"); i != "" {
			parts = append(parts, "by "+i)
		}
		return strings.Join(parts, " — ")
	case "LLM_DIRECT", "LLM_DIRECT_STREAM":
		model := str("model")
		dur := num("duration_ms")
		inTok := num("input_tokens")
		outTok := num("output_tokens")
		if model != "" {
			return fmt.Sprintf("%s  %dms  in:%d out:%d", model, dur, inTok, outTok)
		}
	case "CONFIG_RELOAD":
		return "enforcer config reloaded"
	case "task_delivered":
		if c := str("content"); c != "" {
			if len(c) > 60 {
				c = c[:57] + "..."
			}
			return fmt.Sprintf(`"%s"`, c)
		}
	case "agent_signal_finding":
		return fmt.Sprintf("[%s] %s", str("severity"), str("description"))
	case "agent_signal_task_complete":
		result := str("result")
		if result == "" {
			result = str("summary")
		}
		if len(result) > 60 {
			result = result[:57] + "..."
		}
		return result
	case "agent_signal_progress_update":
		return str("content")
	case "agent_signal_error":
		category := str("category")
		stage := str("stage")
		status := num("status")
		msg := str("message")
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		if category != "" {
			if status != 0 {
				return fmt.Sprintf("%s: %s (%d) %s", category, stage, status, msg)
			}
			return fmt.Sprintf("%s: %s %s", category, stage, msg)
		}
	}
	return ""
}

// ════════════════════════════════════════════════════════════════════════════
// Agent lifecycle — top-level, less frequent
// ════════════════════════════════════════════════════════════════════════════

func createCmd() *cobra.Command {
	var preset string
	cmd := &cobra.Command{
		Use:   "create <name> [--preset X]",
		Short: "Create a new agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.CreateAgent(args[0], preset); err != nil {
				return err
			}
			fmt.Printf("%s Agent %s created\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	cmd.Flags().StringVar(&preset, "preset", "generalist", "Agent preset")
	return cmd
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <agent>",
		Short: "Delete an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.DeleteAgent(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Agent %s deleted\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
}

func haltCmd() *cobra.Command {
	var tier, reason string
	cmd := &cobra.Command{
		Use:   "halt <agent>",
		Short: "Halt an agent (pause with audit)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.HaltAgent(args[0], tier, reason)
			if err != nil {
				return err
			}
			haltID, _ := result["halt_id"].(string)
			fmt.Printf("%s Agent %s halted (id: %s)\n", green.Render("✓"), bold.Render(args[0]), dim.Render(haltID))
			return nil
		},
	}
	cmd.Flags().StringVar(&tier, "tier", "supervised", "Halt tier (supervised/immediate/emergency)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for halt")
	return cmd
}

func resumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <agent>",
		Short: "Resume a halted agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.ResumeAgent(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Agent %s resumed\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
}

func grantCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grant <agent> <capability>",
		Short: "Grant governed capability access to an agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.GrantAgent(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("%s Granted %s to %s\n", green.Render("✓"), bold.Render(args[1]), bold.Render(args[0]))
			return nil
		},
	}
}

func revokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <agent> <capability>",
		Short: "Revoke governed capability access from an agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.RevokeAgent(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("%s Revoked %s from %s\n", green.Render("✓"), bold.Render(args[1]), bold.Render(args[0]))
			return nil
		},
	}
}

// showCmd displays agent details and budget information.
func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <agent>",
		Short: "Show agent details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			agent, err := c.ShowAgent(args[0])
			if err != nil {
				return err
			}
			prettyPrint(agent)

			// Show budget info
			budget, berr := c.GetAgentBudget(args[0])
			if berr == nil && budget != nil {
				fmt.Println()
				fmt.Println(bold.Render("Budget:"))

				dailyUsed, _ := budget["daily_used"].(float64)
				dailyLimit, _ := budget["daily_limit"].(float64)
				monthlyUsed, _ := budget["monthly_used"].(float64)
				monthlyLimit, _ := budget["monthly_limit"].(float64)

				dailyPct := 0.0
				if dailyLimit > 0 {
					dailyPct = dailyUsed / dailyLimit * 100
				}
				monthlyPct := 0.0
				if monthlyLimit > 0 {
					monthlyPct = monthlyUsed / monthlyLimit * 100
				}

				fmt.Printf("  Today:      $%.2f / $%.2f (%.0f%%)\n", dailyUsed, dailyLimit, dailyPct)
				fmt.Printf("  This month: $%.2f / $%.2f (%.0f%%)\n", monthlyUsed, monthlyLimit, monthlyPct)

				todayCalls, _ := budget["today_llm_calls"].(float64)
				todayInput, _ := budget["today_input_tokens"].(float64)
				todayOutput, _ := budget["today_output_tokens"].(float64)
				if todayCalls > 0 || todayInput > 0 || todayOutput > 0 {
					fmt.Println(bold.Render("\nUsage (today):"))
					fmt.Printf("  LLM calls: %.0f\n", todayCalls)
					fmt.Printf("  Input tokens: %.0f\n", todayInput)
					fmt.Printf("  Output tokens: %.0f\n", todayOutput)
				}
			}
			return nil
		},
	}
}

// ════════════════════════════════════════════════════════════════════════════
// Channels (grouped)
// ════════════════════════════════════════════════════════════════════════════

func channelCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "comms", Short: "Channel operations"}

	listCmd := &cobra.Command{
		Use: "list", Short: "List channels",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			includeArchived, _ := cmd.Flags().GetBool("include-archived")
			includeUnavailable, _ := cmd.Flags().GetBool("include-unavailable")
			includeInactive, _ := cmd.Flags().GetBool("include-inactive")
			channels, err := c.ListChannelsWithOptions(
				includeArchived || includeInactive,
				includeUnavailable || includeInactive,
			)
			if err != nil {
				return err
			}
			if len(channels) == 0 {
				fmt.Println(dim.Render("No channels"))
				return nil
			}
			for _, ch := range channels {
				name, _ := ch["name"].(string)
				topic, _ := ch["topic"].(string)
				fmt.Printf("  %s %s\n", bold.Render(fmt.Sprintf("%-20s", name)), dim.Render(topic))
			}
			return nil
		},
	}
	listCmd.Flags().Bool("include-archived", false, "Include archived channels")
	listCmd.Flags().Bool("include-unavailable", false, "Include unavailable orphan DMs")
	listCmd.Flags().Bool("include-inactive", false, "Include archived channels and unavailable orphan DMs")
	cmd.AddCommand(listCmd)

	readCmd := &cobra.Command{
		Use: "read <name>", Short: "Read channel messages", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			messages, err := c.ReadChannel(args[0], limit)
			if err != nil {
				return err
			}
			for _, m := range messages {
				author, _ := m["author"].(string)
				content, _ := m["content"].(string)
				ts, _ := m["timestamp"].(string)
				tsDisplay := ""
				if len(ts) >= 19 {
					tsDisplay = ts[:19]
				} else {
					tsDisplay = ts
				}
				fmt.Printf("  %s  %s: %s\n", dim.Render(tsDisplay), cyan.Render(author), content)
			}
			return nil
		},
	}
	readCmd.Flags().Int("limit", 50, "Number of messages to show")
	cmd.AddCommand(readCmd)

	createChCmd := &cobra.Command{
		Use: "create <name>", Short: "Create a channel", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			topic, _ := cmd.Flags().GetString("topic")
			if _, err := c.CreateChannel(args[0], topic); err != nil {
				return err
			}
			fmt.Printf("%s Channel %s created\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	createChCmd.Flags().String("topic", "", "Channel topic")
	cmd.AddCommand(createChCmd)

	searchCmd := &cobra.Command{
		Use: "search <query>", Short: "Search messages", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			channel, _ := cmd.Flags().GetString("channel")
			results, err := c.SearchMessages(args[0], channel)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println(dim.Render("No results"))
				return nil
			}
			for _, m := range results {
				ch, _ := m["channel"].(string)
				author, _ := m["author"].(string)
				content, _ := m["content"].(string)
				fmt.Printf("  [%s] %s: %s\n", cyan.Render(ch), bold.Render(author), content)
			}
			return nil
		},
	}
	searchCmd.Flags().String("channel", "", "Limit search to channel")
	cmd.AddCommand(searchCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "archive <name>", Short: "Archive a channel", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.ArchiveChannel(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Channel %s archived\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Infrastructure (grouped)
// ════════════════════════════════════════════════════════════════════════════

func infraCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "infra", Short: "Infrastructure management"}

	cmd.AddCommand(&cobra.Command{
		Use: "up", Short: "Start all infrastructure",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			s := newSpinner()
			go s.run()
			err = c.InfraUpStream(func(component, status string) {
				s.update(status)
			})
			s.finish()
			if err != nil {
				return err
			}
			fmt.Printf("%s Infrastructure started\n", green.Render("✓"))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "down", Short: "Stop all infrastructure",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			s := newSpinner()
			go s.run()
			err = c.InfraDownStream(func(component, status string) {
				s.update(status)
			})
			s.finish()
			if err != nil {
				return err
			}
			fmt.Printf("%s Infrastructure stopped\n", green.Render("✓"))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "status", Short: "Show infrastructure status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			resp, err := c.InfraStatus()
			if err != nil {
				return err
			}
			for _, ic := range resp.Components {
				icon := green.Render("●")
				if ic["health"] != "healthy" && ic["state"] != "running" {
					icon = red.Render("○")
				}
				buildCol := ""
				if bid := ic["build_id"]; bid != "" {
					if bid == resp.BuildID {
						buildCol = green.Render(bid + " ✓")
					} else {
						buildCol = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(bid + " ⚠ stale")
					}
				}
				fmt.Printf("  %s %s %s %s\n", icon, bold.Render(fmt.Sprintf("%-14s", ic["name"])), dim.Render(fmt.Sprintf("%-10s", ic["state"])), buildCol)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "rebuild <component>", Short: "Rebuild an infrastructure component", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			s := newSpinner()
			go s.run()
			err = c.InfraRebuildStream(args[0], func(component, status string) {
				s.update(status)
			})
			s.finish()
			if err != nil {
				return err
			}
			fmt.Printf("%s Component %s rebuilt\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "reload", Short: "Reload infrastructure configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			fmt.Print("Reloading infrastructure...")
			result, err := c.InfraReload()
			if err != nil {
				fmt.Println()
				return err
			}
			components, _ := result["components"].([]interface{})
			if len(components) > 0 {
				var names []string
				for _, comp := range components {
					if s, ok := comp.(string); ok {
						names = append(names, s)
					}
				}
				fmt.Printf("\r%s Infrastructure reloaded: %s\n", green.Render("✓"), strings.Join(names, ", "))
			} else {
				fmt.Printf("\r%s Infrastructure configuration reloaded\n", green.Render("✓"))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "capacity",
		Short: "Show host capacity and agent slot availability",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			resp, err := c.InfraCapacity()
			if err != nil {
				return fmt.Errorf("cannot read capacity: %w", err)
			}

			// Extract values with safe type assertions
			getInt := func(key string) int {
				if v, ok := resp[key].(float64); ok {
					return int(v)
				}
				return 0
			}
			getBool := func(key string) bool {
				v, _ := resp[key].(bool)
				return v
			}

			maxAgents := getInt("max_agents")
			running := getInt("running_agents")
			meeseeks := getInt("running_meeseeks")
			available := getInt("available_slots")
			memMB := getInt("host_memory_mb")
			cpuCores := getInt("host_cpu_cores")
			reserveMB := getInt("system_reserve_mb")
			infraMB := getInt("infra_overhead_mb")
			poolOK := getBool("network_pool_configured")

			fmt.Println("Host Capacity:")
			fmt.Printf("  Memory: %d GB (%.1f GB reserved, %.1f GB infra)\n",
				memMB/1024, float64(reserveMB)/1024.0, float64(infraMB)/1024.0)
			fmt.Printf("  CPU: %d cores (2 reserved)\n", cpuCores)
			fmt.Println()
			fmt.Printf("Agents: %d/%d running (%d slots available", running, maxAgents, available)
			if meeseeks > 0 {
				fmt.Printf(", %d used by meeseeks", meeseeks)
			}
			fmt.Println(")")
			fmt.Printf("Meeseeks: %d active (shares agent pool)\n", meeseeks)

			if poolOK {
				fmt.Println("Networks: configured (/24 subnets)")
			} else {
				fmt.Println("Networks: default pool (limited — run agency setup to configure)")
			}

			fmt.Println()
			fmt.Println("Config: ~/.agency/capacity.yaml")
			return nil
		},
	})

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Hub (grouped, includes deploy/teardown lifecycle)
// ════════════════════════════════════════════════════════════════════════════

func hubCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "hub", Short: "Hub and deploy operations"}

	cmd.AddCommand(&cobra.Command{
		Use: "search [query]", Short: "Search hub", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			results, err := c.HubSearch(query, "")
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println(dim.Render("No results"))
				return nil
			}
			for _, r := range results {
				fmt.Printf("  %s %s %s\n", bold.Render(fmt.Sprintf("%-20s", r["name"])), cyan.Render(fmt.Sprintf("%-12s", r["kind"])), dim.Render(r["description"]))
			}
			return nil
		},
	})

	var installAs string
	installCmd := &cobra.Command{
		Use: "install <name>", Short: "Install and activate a hub component", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			kind, _ := cmd.Flags().GetString("kind")
			source, _ := cmd.Flags().GetString("source")
			yes, _ := cmd.Flags().GetBool("yes")

			// Show consent prompt — what this component requires
			info, infoErr := c.HubInfo(args[0], kind)
			if infoErr == nil {
				requires, _ := info["requires"].(map[string]interface{})
				if requires != nil {
					fmt.Printf("\n  %s This component requires:\n\n", yellow.Render("⚠"))

					// Credential → Domain mapping
					creds, _ := requires["credentials"].([]interface{})
					egressDomains, _ := requires["egress_domains"].([]interface{})
					if len(creds) > 0 {
						fmt.Println("  Credential → Domain mapping:")
						for _, cr := range creds {
							cm, _ := cr.(map[string]interface{})
							credName, _ := cm["name"].(string)
							grantName, _ := cm["grant_name"].(string)
							// Find which egress domain this credential maps to
							domain := "(unknown)"
							if grantName != "" {
								// Try to find matching service
								svcInfo, serr := c.HubInfo(grantName, "service")
								if serr == nil {
									if apiBase, ok := svcInfo["api_base"].(string); ok {
										domain = apiBase
									}
								}
							}
							// Check if domain is in egress list
							matched := false
							for _, ed := range egressDomains {
								if eds, ok := ed.(string); ok && strings.Contains(domain, eds) {
									matched = true
									domain = eds
								}
							}
							matchLabel := green.Render("✓ matches service")
							if !matched && domain != "(unknown)" {
								matchLabel = red.Render("✗ credential sent to unrelated domain")
							}
							fmt.Printf("    %-20s → %-30s %s\n", credName, domain, matchLabel)
						}
						fmt.Println()
					}

					// Egress domains
					if len(egressDomains) > 0 {
						fmt.Println("  Egress domains:")
						for _, ed := range egressDomains {
							if eds, ok := ed.(string); ok {
								fmt.Printf("    %s\n", eds)
							}
						}
						fmt.Println()
					}

					// Routes
					routes, _ := info["routes"].([]interface{})
					if len(routes) > 0 {
						fmt.Println("  Routes work items to:")
						for _, r := range routes {
							rm, _ := r.(map[string]interface{})
							target, _ := rm["target"].(map[string]interface{})
							for targetType, targetName := range target {
								fmt.Printf("    → %s: %s\n", targetType, targetName)
							}
						}
						fmt.Println()
					}

					// Source and author
					infoSource, _ := info["_source"].(string)
					author, _ := info["author"].(string)
					license, _ := info["license"].(string)
					if infoSource != "" {
						fmt.Printf("  Source: %s\n", infoSource)
					}
					if author != "" {
						fmt.Printf("  Author: %s\n", author)
					}
					if license != "" {
						fmt.Printf("  License: %s\n", license)
					}

					// Ask for consent (skip with --yes)
					if !yes && (len(creds) > 0 || len(egressDomains) > 0) {
						fmt.Print("\n  Install and grant access? [y/N] ")
						var answer string
						fmt.Scanln(&answer)
						answer = strings.TrimSpace(strings.ToLower(answer))
						if answer != "y" && answer != "yes" {
							fmt.Println("  Cancelled.")
							return nil
						}
					}
					fmt.Println()
				}
			}

			if err := c.HubInstall(args[0], kind, source); err != nil {
				return err
			}
			fmt.Printf("%s Installed and activated %s\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	installCmd.Flags().String("kind", "", "Component kind (connector/pack/preset/service/mission)")
	installCmd.Flags().String("source", "", "Source override")
	installCmd.Flags().StringVar(&installAs, "as", "", "Instance name (defaults to component name)")
	installCmd.Flags().Bool("yes", false, "Skip consent prompt")
	cmd.AddCommand(installCmd)
	cmd.AddCommand(hubDeploymentCmd())

	deployCmd := &cobra.Command{
		Use: "deploy <pack>", Short: "Deploy a pack",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}

			// Build credentials map from --set flags and --credentials file.
			creds := map[string]string{}
			credFile, _ := cmd.Flags().GetString("credentials")
			if credFile != "" {
				data, ferr := os.ReadFile(credFile)
				if ferr != nil {
					return fmt.Errorf("read credentials file: %w", ferr)
				}
				for _, line := range strings.Split(string(data), "\n") {
					line = strings.TrimSpace(line)
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					k, v, found := strings.Cut(line, "=")
					if found {
						creds[strings.TrimSpace(k)] = strings.TrimSpace(v)
					}
				}
			}
			setFlags, _ := cmd.Flags().GetStringArray("set")
			for _, kv := range setFlags {
				k, v, found := strings.Cut(kv, "=")
				if !found {
					return fmt.Errorf("invalid --set value %q: expected KEY=VALUE", kv)
				}
				creds[k] = v
			}

			agencyHome := config.Load().Home
			packPath := args[0]
			// If the arg doesn't look like a file path, resolve it as a hub instance name
			if !strings.Contains(packPath, "/") && !strings.HasSuffix(packPath, ".yaml") {
				info, ierr := c.HubShow(packPath)
				if ierr == nil {
					if id, ok := info["id"].(string); ok {
						if kind, ok := info["kind"].(string); ok && kind == "pack" {
							resolved := filepath.Join(agencyHome, "hub-registry", "packs", id, "pack.yaml")
							if _, ferr := os.Stat(resolved); ferr == nil {
								packPath = resolved
							}
						}
					}
				}
			}
			result, err := c.Deploy(packPath, creds)
			if err != nil {
				return err
			}
			if errMsg, ok := result["error"].(string); ok {
				return fmt.Errorf("%s", errMsg)
			}
			if status, ok := result["status"].(string); ok && status == "credentials_required" {
				missing, _ := result["missing"].([]interface{})
				names := make([]string, 0, len(missing))
				for _, m := range missing {
					if s, ok := m.(string); ok {
						names = append(names, s)
					}
				}
				return fmt.Errorf("missing required credentials: %s", strings.Join(names, ", "))
			}
			agents, _ := result["agents_created"].([]interface{})
			fmt.Printf("%s Pack deployed: %d agents created\n", green.Render("✓"), len(agents))

			// Auto-create and assign missions if the pack YAML has mission_assignments
			packData, _ := os.ReadFile(packPath)
			var packDoc map[string]interface{}
			if yaml.Unmarshal(packData, &packDoc) == nil {
				if assignments, ok := packDoc["mission_assignments"].([]interface{}); ok {
					for _, a := range assignments {
						am, _ := a.(map[string]interface{})
						missionName, _ := am["mission"].(string)
						agentName, _ := am["agent"].(string)
						if missionName == "" || agentName == "" {
							continue
						}

						// Install mission hub component if not already installed
						if _, showErr := c.HubShow(missionName); showErr != nil {
							c.HubInstall(missionName, "mission", "")
						}

						// Ensure mission exists and is assigned to the right agent.
						// Idempotent: handles create, reassign, and resume in all states.

						// Find the hub mission YAML
						var hubMissionData []byte
						if info, err := c.HubShow(missionName); err == nil {
							if id, ok := info["id"].(string); ok {
								hubPath := filepath.Join(agencyHome, "hub-registry", "missions", id, "mission.yaml")
								hubMissionData, _ = os.ReadFile(hubPath)
							}
						}

						existing, getErr := c.MissionShow(missionName)
						if getErr != nil {
							// Mission doesn't exist — create and assign
							if hubMissionData == nil {
								fmt.Printf("  %s Mission %s: hub YAML not found\n", yellow.Render("!"), missionName)
								continue
							}
							if _, cerr := c.MissionCreate(hubMissionData); cerr != nil {
								fmt.Printf("  %s Mission %s create failed: %s\n", yellow.Render("!"), missionName, cerr)
								continue
							}
							fmt.Printf("  %s Mission %s created\n", green.Render("✓"), missionName)
						} else {
							// Mission exists — ensure it's in a state we can assign from
							assignedTo, _ := existing["assigned_to"].(string)
							status, _ := existing["status"].(string)

							if assignedTo == agentName && status == "active" {
								// Already active and assigned — just ensure agent has the file
								missionFile := filepath.Join(agencyHome, "agents", agentName, "mission.yaml")
								srcFile := filepath.Join(agencyHome, "missions", missionName+".yaml")
								if srcData, rerr := os.ReadFile(srcFile); rerr == nil {
									os.WriteFile(missionFile, srcData, 0644)
								}
								fmt.Printf("  %s Mission %s active on %s\n", green.Render("✓"), missionName, agentName)
								continue
							}

							// Need to get mission to "unassigned" state for reassignment.
							// State machine: active→pause→complete→delete, paused→complete→delete,
							// completed→delete, unassigned→delete
							if status == "active" {
								c.MissionPause(missionName)
							}
							if status == "active" || status == "paused" {
								c.MissionComplete(missionName)
							}
							c.MissionDelete(missionName)

							// Recreate from hub YAML
							if hubMissionData == nil {
								fmt.Printf("  %s Mission %s: hub YAML not found for recreate\n", yellow.Render("!"), missionName)
								continue
							}
							if _, cerr := c.MissionCreate(hubMissionData); cerr != nil {
								fmt.Printf("  %s Mission %s recreate failed: %s\n", yellow.Render("!"), missionName, cerr)
								continue
							}
							fmt.Printf("  %s Mission %s recreated\n", green.Render("✓"), missionName)
						}

						// Assign mission to agent (mission is now in "unassigned" state)
						if err := c.MissionAssign(missionName, agentName, "agent"); err != nil {
							fmt.Printf("  %s Mission %s assign failed: %s\n", yellow.Render("!"), missionName, err)
						} else {
							fmt.Printf("  %s Mission %s assigned to %s\n", green.Render("✓"), missionName, agentName)
						}
					}
				}
			}

			return nil
		},
	}
	deployCmd.Flags().StringArray("set", nil, "Set credential KEY=VALUE (may be repeated)")
	deployCmd.Flags().String("credentials", "", "Path to KEY=VALUE credentials env file")
	cmd.AddCommand(deployCmd)

	teardownCmd := &cobra.Command{
		Use: "teardown <pack>", Short: "Teardown a deployed pack",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			del, _ := cmd.Flags().GetBool("delete")
			if err := c.Teardown(args[0], del); err != nil {
				return err
			}
			fmt.Printf("%s Pack %s torn down\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	teardownCmd.Flags().Bool("delete", false, "Delete agents and resources")
	cmd.AddCommand(teardownCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List available hub components",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			// Get everything available
			available, err := c.HubSearch("", "")
			if err != nil {
				return err
			}
			if len(available) == 0 {
				fmt.Println(dim.Render("No components available"))
				return nil
			}
			// Get installed set for marking
			installed, _ := c.HubList()
			installedSet := map[string]bool{}
			for _, i := range installed {
				installedSet[i["component"]] = true
			}
			for _, r := range available {
				name := r["name"]
				kind := r["kind"]
				desc := r["description"]
				// Truncate description to first line / 60 chars
				if idx := strings.Index(desc, "\n"); idx > 0 {
					desc = desc[:idx]
				}
				if len(desc) > 60 {
					desc = desc[:57] + "..."
				}
				marker := "  "
				if installedSet[name] {
					marker = green.Render("✓ ")
				}
				fmt.Printf("%s%s %s %s\n", marker, bold.Render(fmt.Sprintf("%-20s", name)), cyan.Render(fmt.Sprintf("%-12s", kind)), dim.Render(desc))
			}
			return nil
		},
	})

	removeCmd := &cobra.Command{
		Use: "remove <name>", Short: "Remove a hub component", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			kind, _ := cmd.Flags().GetString("kind")
			if err := c.HubRemove(args[0], kind); err != nil {
				return err
			}
			fmt.Printf("%s Removed %s\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	removeCmd.Flags().String("kind", "", "Component kind (auto-detected if unambiguous)")
	cmd.AddCommand(removeCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "update", Short: "Refresh hub sources (does not upgrade)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			report, err := c.HubUpdate()
			if err != nil {
				return err
			}

			// Sources
			anyNew := false
			for _, su := range report.Sources {
				if su.OldCommit != su.NewCommit && su.NewCommit != "" {
					anyNew = true
				}
			}

			if !anyNew && len(report.Available) == 0 {
				fmt.Printf("%s Hub sources up to date\n", green.Render("✓"))
				return nil
			}

			fmt.Printf("%s Hub sources updated\n", green.Render("✓"))
			if anyNew {
				fmt.Println("  Sources:")
				for _, su := range report.Sources {
					if su.OldCommit != su.NewCommit && su.NewCommit != "" {
						fmt.Printf("    %-12s %d new commits (%s → %s)\n", su.Name, su.CommitCount, su.OldCommit, su.NewCommit)
					}
				}
			}
			if len(report.Available) > 0 {
				fmt.Println("  Upgrades available:")
				for _, u := range report.Available {
					if u.Kind == "managed" {
						fmt.Printf("    %-20s %-10s %s\n", u.Name, u.Category, u.Summary)
					} else {
						fmt.Printf("    %-20s %-10s %s → %s\n", u.Name, u.Kind, u.InstalledVersion, u.AvailableVersion)
					}
				}
				fmt.Println("  Run 'agency hub upgrade' to apply.")
			}
			for _, w := range report.Warnings {
				fmt.Printf("  %s %s\n", yellow.Render("!"), w)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "check [name]", Short: "Check if a component is working", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				// Doctor — all components
				var result map[string]interface{}
				if err := c.GetJSON("/api/v1/hub/doctor", &result); err != nil {
					return err
				}
				components, _ := result["components"].([]interface{})
				if len(components) == 0 {
					fmt.Println(dim.Render("No components installed"))
					return nil
				}
				fmt.Printf("  %-20s %-12s %-12s %s\n", bold.Render("NAME"), bold.Render("KIND"), bold.Render("HEALTH"), bold.Render("SUMMARY"))
				for _, comp := range components {
					cm, _ := comp.(map[string]interface{})
					name, _ := cm["name"].(string)
					kind, _ := cm["kind"].(string)
					status, _ := cm["status"].(string)
					summary, _ := cm["summary"].(string)
					statusColor := green
					if status == "unhealthy" {
						statusColor = red
					} else if status == "degraded" {
						statusColor = yellow
					}
					fmt.Printf("  %-20s %-12s %-12s %s\n", name, dim.Render(kind), statusColor.Render(status), dim.Render(summary))
				}
				return nil
			}
			// Single component
			var result map[string]interface{}
			if err := c.GetJSON("/api/v1/hub/"+args[0]+"/check", &result); err != nil {
				return err
			}
			status, _ := result["status"].(string)
			kind, _ := result["kind"].(string)
			summary, _ := result["summary"].(string)
			statusColor := green
			icon := "✓"
			if status == "unhealthy" {
				statusColor = red
				icon = "✗"
			} else if status == "degraded" {
				statusColor = yellow
				icon = "!"
			}
			fmt.Printf("%s %s (%s)  %s\n\n", statusColor.Render(icon), bold.Render(args[0]), dim.Render(kind), statusColor.Render(strings.ToUpper(status)))
			checks, _ := result["checks"].([]interface{})
			for _, ch := range checks {
				cc, _ := ch.(map[string]interface{})
				name, _ := cc["name"].(string)
				st, _ := cc["status"].(string)
				detail, _ := cc["detail"].(string)
				fix, _ := cc["fix"].(string)
				checkIcon := green.Render("✓")
				if st == "fail" {
					checkIcon = red.Render("✗")
				} else if st == "warn" {
					checkIcon = yellow.Render("!")
				}
				fmt.Printf("  %s %-22s %s\n", checkIcon, name, detail)
				if fix != "" && st == "fail" {
					fmt.Printf("    %s %s\n", dim.Render("Fix:"), fix)
				}
			}
			if summary != "" {
				fmt.Printf("\n  %s\n", dim.Render(summary))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "doctor", Short: "Check overall hub health",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Alias for hub check (no args)
			c, err := requireGateway()
			if err != nil {
				return err
			}
			var result map[string]interface{}
			if err := c.GetJSON("/api/v1/hub/doctor", &result); err != nil {
				return err
			}
			components, _ := result["components"].([]interface{})
			healthy := 0
			unhealthy := 0
			for _, comp := range components {
				cm, _ := comp.(map[string]interface{})
				if s, _ := cm["status"].(string); s == "healthy" {
					healthy++
				} else {
					unhealthy++
				}
			}
			if unhealthy == 0 {
				fmt.Printf("%s Hub healthy: %d components, all checks passing\n", green.Render("✓"), len(components))
			} else {
				fmt.Printf("%s Hub issues: %d healthy, %d unhealthy\n", yellow.Render("!"), healthy, unhealthy)
				fmt.Println("\n  Run 'agency hub check' for details")
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "outdated", Short: "Show available upgrades",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			upgrades, err := c.HubOutdated()
			if err != nil {
				return err
			}
			if len(upgrades) == 0 {
				fmt.Println("All components up to date.")
				return nil
			}
			fmt.Println("Upgrades available:")
			for _, u := range upgrades {
				if u.Kind == "managed" {
					fmt.Printf("  %-20s %-10s %s\n", u.Name, u.Category, u.Summary)
				} else {
					fmt.Printf("  %-20s %-10s %s → %s\n", u.Name, u.Kind, u.InstalledVersion, u.AvailableVersion)
				}
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "upgrade [component...]", Short: "Apply available upgrades",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			var components []string
			if len(args) > 0 {
				components = args
			}
			report, err := c.HubUpgrade(components)
			if err != nil {
				return err
			}

			hasErrors := false

			// Show managed file results (only for full upgrade)
			if len(report.Files) > 0 {
				fmt.Println("  Synced:")
				for _, f := range report.Files {
					label := f.Category
					switch f.Status {
					case "upgraded":
						detail := f.Summary
						if detail == "" {
							detail = "updated"
						}
						fmt.Printf("    %-12s %s\n", label, detail)
					case "unchanged":
						fmt.Printf("    %-12s unchanged\n", label)
					case "error":
						hasErrors = true
						fmt.Printf("    %-12s %s %s\n", label, red.Render("✗"), f.Summary)
					}
				}
			}

			// Show component results
			var upgraded, errors []string
			for _, cu := range report.Components {
				switch cu.Status {
				case "upgraded":
					upgraded = append(upgraded, fmt.Sprintf("    %-20s %-10s %s → %s", cu.Name, cu.Kind, cu.OldVersion, cu.NewVersion))
				case "error":
					hasErrors = true
					errors = append(errors, fmt.Sprintf("    %-20s %-10s %s → %s  %s %s", cu.Name, cu.Kind, cu.OldVersion, cu.NewVersion, red.Render("✗"), cu.Error))
				}
			}
			if len(upgraded) > 0 {
				fmt.Println("  Upgraded:")
				for _, line := range upgraded {
					fmt.Println(line)
				}
			}
			if len(errors) > 0 {
				fmt.Println("  Errors:")
				for _, line := range errors {
					fmt.Println(line)
				}
			}

			if hasErrors {
				fmt.Printf("%s Hub upgraded (with errors)\n", yellow.Render("✓"))
			} else {
				fmt.Printf("%s Hub upgraded\n", green.Render("✓"))
			}
			for _, w := range report.Warnings {
				fmt.Printf("  %s %s\n", yellow.Render("!"), w)
			}
			return nil
		},
	})

	// ── Source management ──

	addSourceCmd := &cobra.Command{
		Use: "add-source <name> <url-or-registry>", Short: "Add a hub source", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srcType, _ := cmd.Flags().GetString("type")
			branch, _ := cmd.Flags().GetString("branch")
			if branch == "" {
				branch = "main"
			}

			// Auto-detect source type if not specified
			if srcType == "" {
				if strings.Contains(args[1], ".git") || strings.HasPrefix(args[1], "https://github.com") {
					srcType = "git"
				} else {
					srcType = "oci"
				}
			}

			home, _ := os.UserHomeDir()
			cfgPath := home + "/.agency/config.yaml"
			data, err := os.ReadFile(cfgPath)
			if err != nil {
				return fmt.Errorf("read config: %w", err)
			}

			// Build entry based on source type
			var entry string
			if srcType == "oci" {
				entry = fmt.Sprintf("    - name: %s\n      type: oci\n      registry: %s\n", args[0], args[1])
			} else {
				entry = fmt.Sprintf("    - name: %s\n      url: %s\n      branch: %s\n", args[0], args[1], branch)
			}

			content := string(data)
			if !strings.Contains(content, "hub:") {
				content += "\nhub:\n  sources:\n" + entry
			} else {
				// Find end of sources list and append
				content = strings.Replace(content, "      branch: main\n", "      branch: main\n"+entry, 1)
			}
			if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
				return err
			}
			fmt.Printf("%s Added %s source %s (%s)\n", green.Render("✓"), srcType, bold.Render(args[0]), args[1])
			fmt.Println("  Run 'agency hub update' to fetch components from this source")
			return nil
		},
	}
	addSourceCmd.Flags().String("type", "", "Source type: oci or git (auto-detected if omitted)")
	addSourceCmd.Flags().String("branch", "main", "Git branch (only for git sources)")
	cmd.AddCommand(addSourceCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "remove-source <name>", Short: "Remove a hub source", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "default" {
				return fmt.Errorf("cannot remove the default source")
			}
			// TODO: implement config editing to remove source
			fmt.Printf("Remove the '%s' source entry from ~/.agency/config.yaml\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "list-sources", Short: "List configured hub sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, _ := os.UserHomeDir()
			data, _ := os.ReadFile(home + "/.agency/config.yaml")
			var cfg map[string]interface{}
			yaml.Unmarshal(data, &cfg)
			hubCfg, _ := cfg["hub"].(map[string]interface{})
			sources, _ := hubCfg["sources"].([]interface{})
			if len(sources) == 0 {
				fmt.Println(dim.Render("No hub sources configured"))
				return nil
			}
			fmt.Printf("  %-15s %-8s %s\n", bold.Render("NAME"), bold.Render("TYPE"), bold.Render("LOCATION"))
			for _, s := range sources {
				sm, _ := s.(map[string]interface{})
				name, _ := sm["name"].(string)
				sourceType, _ := sm["type"].(string)
				if sourceType == "" {
					sourceType = "git"
				}
				url, _ := sm["url"].(string)
				location := url
				if sourceType == "oci" {
					location, _ = sm["registry"].(string)
				}
				fmt.Printf("  %-15s %-8s %s\n", name, sourceType, location)
			}
			return nil
		},
	})

	// ── Scaffolding ──

	cmd.AddCommand(&cobra.Command{
		Use: "create <kind> <name>", Short: "Scaffold a new hub component", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, name := args[0], args[1]
			dir := name
			os.MkdirAll(dir, 0755)

			var content string
			switch kind {
			case "connector":
				content = fmt.Sprintf("kind: connector\nname: %s\nversion: \"0.1.0\"\ndescription: \"\"\nauthor: \"\"\nlicense: MIT\n\nrequires:\n  credentials: []\n  egress_domains: []\n\nsource:\n  type: poll\n  url: \"\"\n  interval: 5m\n\ngraph_ingest: []\n\nroutes: []\n", name)
				os.WriteFile(dir+"/connector.yaml", []byte(content), 0644)
			case "service":
				content = fmt.Sprintf("service: %s\ndisplay_name: \"\"\napi_base: \"\"\ndescription: \"\"\ncredential:\n  env_var: \"\"\n  header: Authorization\n  format: \"Bearer {key}\"\n  scoped_prefix: agency-scoped-%s\n\ntools: []\n", name, name)
				os.WriteFile(dir+"/service.yaml", []byte(content), 0644)
			case "preset":
				content = fmt.Sprintf("name: %s\ntype: standard\nmodel_tier: standard\ndescription: \"\"\n\ntools:\n  - python3\n  - curl\n\ncapabilities:\n  - file_read\n\nidentity:\n  purpose: \"\"\n  body: \"\"\n\nhard_limits: []\n\nescalation:\n  always_escalate: []\n  flag_before_proceeding: []\n", name)
				os.WriteFile(dir+"/preset.yaml", []byte(content), 0644)
			case "mission":
				content = fmt.Sprintf("kind: mission\nname: %s\nversion: \"0.1.0\"\ndescription: \"\"\n\ninstructions: |\n  \n\ntriggers: []\n\nbudget:\n  per_task: 0.10\n  daily: 1.00\n", name)
				os.WriteFile(dir+"/mission.yaml", []byte(content), 0644)
			case "pack":
				content = fmt.Sprintf("kind: pack\nname: %s\nversion: \"0.1.0\"\ndescription: \"\"\nauthor: \"\"\n\nrequires:\n  connectors: []\n  services: []\n  presets: []\n  missions: []\n\nteam:\n  name: %s\n  agents: []\n  channels: []\n\nmission_assignments: []\n", name, name)
				os.WriteFile(dir+"/pack.yaml", []byte(content), 0644)
				os.WriteFile(dir+"/AGENTS.md", []byte("# "+name+"\n\nContext for agents in this pack.\n"), 0644)
			default:
				return fmt.Errorf("unknown kind: %s (use connector, service, preset, mission, or pack)", kind)
			}

			os.WriteFile(dir+"/README.md", []byte("# "+name+"\n\nA "+kind+" for Agency.\n"), 0644)
			fmt.Printf("%s Scaffolded %s %s in ./%s/\n", green.Render("✓"), kind, bold.Render(name), dir)
			return nil
		},
	})

	// ── Validation and Publishing ──

	cmd.AddCommand(&cobra.Command{
		Use: "audit <path>", Short: "Validate a component before publishing", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			// Find the component YAML
			var componentFile string
			var componentKind string
			for _, kind := range []string{"connector", "service", "preset", "mission", "pack"} {
				path := dir + "/" + kind + ".yaml"
				if _, err := os.Stat(path); err == nil {
					componentFile = path
					componentKind = kind
					break
				}
			}
			if componentFile == "" {
				return fmt.Errorf("no component YAML found in %s (expected connector.yaml, service.yaml, etc.)", dir)
			}

			data, err := os.ReadFile(componentFile)
			if err != nil {
				return err
			}

			var doc map[string]interface{}
			if err := yaml.Unmarshal(data, &doc); err != nil {
				fmt.Printf("%s Invalid YAML: %s\n", red.Render("✗"), err)
				return nil
			}

			issues := 0

			// Required fields
			for _, field := range []string{"name", "version"} {
				if v, ok := doc[field]; !ok || fmt.Sprint(v) == "" {
					fmt.Printf("  %s Missing required field: %s\n", red.Render("✗"), field)
					issues++
				}
			}

			// Kind match
			if k, ok := doc["kind"].(string); ok && k != componentKind {
				fmt.Printf("  %s Kind mismatch: file is %s.yaml but kind field says %s\n", red.Render("✗"), componentKind, k)
				issues++
			}

			// Description
			if desc, _ := doc["description"].(string); desc == "" {
				fmt.Printf("  %s Missing description\n", yellow.Render("!"))
				issues++
			}

			// Version format
			if v, _ := doc["version"].(string); v != "" && !strings.Contains(v, ".") {
				fmt.Printf("  %s Version %q doesn't look like semver\n", yellow.Render("!"), v)
			}

			// Connector-specific checks
			if componentKind == "connector" {
				// Check graph_ingest templates for dunder access
				content := string(data)
				if strings.Contains(content, "__") && strings.Contains(content, "{{") {
					fmt.Printf("  %s Template contains dunder access (__) — potential sandbox escape\n", red.Render("✗"))
					issues++
				}

				// Check egress domains
				if requires, ok := doc["requires"].(map[string]interface{}); ok {
					if domains, ok := requires["egress_domains"].([]interface{}); ok {
						for _, d := range domains {
							ds, _ := d.(string)
							if strings.HasPrefix(ds, "169.254") || strings.HasPrefix(ds, "0.0.0.0") {
								fmt.Printf("  %s Blocked egress domain: %s\n", red.Render("✗"), ds)
								issues++
							}
						}
					}
				}
			}

			// README check
			if _, err := os.Stat(dir + "/README.md"); os.IsNotExist(err) {
				fmt.Printf("  %s No README.md\n", yellow.Render("!"))
			}

			if issues == 0 {
				fmt.Printf("%s %s %s passed audit\n", green.Render("✓"), componentKind, bold.Render(fmt.Sprint(doc["name"])))
			} else {
				fmt.Printf("\n  %d issue(s) found\n", issues)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "publish <path>", Short: "Publish a component to a hub source", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			sourceName, _ := cmd.Flags().GetString("source")
			if sourceName == "" {
				sourceName = "default"
			}

			// Find component YAML
			var componentFile string
			var componentKind string
			for _, kind := range []string{"connector", "service", "preset", "mission", "pack"} {
				path := dir + "/" + kind + ".yaml"
				if _, err := os.Stat(path); err == nil {
					componentFile = path
					componentKind = kind
					break
				}
			}
			if componentFile == "" {
				return fmt.Errorf("no component YAML found in %s", dir)
			}

			data, err := os.ReadFile(componentFile)
			if err != nil {
				return err
			}
			var doc map[string]interface{}
			yaml.Unmarshal(data, &doc)
			name, _ := doc["name"].(string)
			if name == "" {
				return fmt.Errorf("component has no name field")
			}

			// Find source URL from config
			home, _ := os.UserHomeDir()
			cfgData, _ := os.ReadFile(home + "/.agency/config.yaml")
			var cfg map[string]interface{}
			yaml.Unmarshal(cfgData, &cfg)
			hubCfg, _ := cfg["hub"].(map[string]interface{})
			sources, _ := hubCfg["sources"].([]interface{})

			var sourceURL string
			for _, s := range sources {
				sm, _ := s.(map[string]interface{})
				if sn, _ := sm["name"].(string); sn == sourceName {
					sourceURL, _ = sm["url"].(string)
				}
			}
			if sourceURL == "" {
				return fmt.Errorf("source %q not found in config", sourceName)
			}

			// Determine destination in hub cache
			cacheDir := home + "/.agency/hub-cache/" + sourceName
			destDir := cacheDir + "/" + componentKind + "s/" + name

			// Copy component files to hub cache
			os.MkdirAll(destDir, 0755)
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				src, _ := os.ReadFile(dir + "/" + e.Name())
				os.WriteFile(destDir+"/"+e.Name(), src, 0644)
			}

			// Git operations in the hub cache repo
			fmt.Printf("Publishing %s %s to %s...\n", componentKind, bold.Render(name), sourceName)

			branchName := fmt.Sprintf("publish-%s-%s", componentKind, name)
			gitDir := cacheDir

			// Create branch, add, commit, push, create PR
			cmds := []struct {
				args []string
				desc string
			}{
				{[]string{"git", "-C", gitDir, "checkout", "main"}, "checkout main"},
				{[]string{"git", "-C", gitDir, "pull", "--ff-only"}, "pull latest"},
				{[]string{"git", "-C", gitDir, "checkout", "-b", branchName}, "create branch"},
				{[]string{"git", "-C", gitDir, "add", componentKind + "s/" + name}, "stage files"},
				{[]string{"git", "-C", gitDir, "commit", "-m", fmt.Sprintf("feat: add %s %s", componentKind, name)}, "commit"},
				{[]string{"git", "-C", gitDir, "push", "-u", "origin", branchName}, "push"},
			}

			for _, c := range cmds {
				out, err := exec.Command(c.args[0], c.args[1:]...).CombinedOutput()
				if err != nil {
					return fmt.Errorf("%s failed: %s\n%s", c.desc, err, strings.TrimSpace(string(out)))
				}
			}

			// Create PR via gh CLI
			prCmd := exec.Command("gh", "pr", "create",
				"--repo", strings.TrimSuffix(strings.TrimPrefix(sourceURL, "https://github.com/"), ".git"),
				"--title", fmt.Sprintf("feat: add %s %s", componentKind, name),
				"--body", fmt.Sprintf("Publishes %s `%s` to the hub.\n\nCreated via `agency hub publish`.", componentKind, name),
				"--head", branchName,
			)
			prCmd.Dir = gitDir
			prOut, prErr := prCmd.CombinedOutput()
			if prErr != nil {
				fmt.Printf("%s Pushed to branch %s but PR creation failed: %s\n", yellow.Render("!"), branchName, strings.TrimSpace(string(prOut)))
				fmt.Println("  Create the PR manually on GitHub")
			} else {
				prURL := strings.TrimSpace(string(prOut))
				fmt.Printf("%s Published! PR: %s\n", green.Render("✓"), prURL)
			}

			// Switch back to main
			exec.Command("git", "-C", gitDir, "checkout", "main").Run()

			return nil
		},
	})

	infoCmd := &cobra.Command{
		Use: "info <name>", Short: "Show component details", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			kind, _ := cmd.Flags().GetString("kind")
			info, err := c.HubInfo(args[0], kind)
			if err != nil {
				return err
			}
			prettyPrint(info)
			return nil
		},
	}
	infoCmd.Flags().String("kind", "", "Component kind filter")
	cmd.AddCommand(infoCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "show <name-or-id>", Short: "Show component instance detail",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.HubShow(args[0])
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	})

	activateCmd := &cobra.Command{
		Use: "activate <name-or-id>", Short: "Activate a component instance",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			setFlags, _ := cmd.Flags().GetStringArray("set")

			// If --dry-run or --set provided, check connector requirements first
			if dryRun || len(setFlags) > 0 {
				var reqs map[string]interface{}
				if err := c.GetJSON("/api/v1/hub/connectors/"+args[0]+"/requirements", &reqs); err != nil {
					// Not a connector or no requires block — fall through
					if !dryRun {
						goto activate
					}
					fmt.Printf("  %s No requirements declared for %s\n", dim.Render("-"), bold.Render(args[0]))
					return nil
				}

				if dryRun {
					printRequirementsTable(reqs)
					return nil
				}

				// --set: provision credentials via configure endpoint
				if len(setFlags) > 0 {
					creds := map[string]string{}
					for _, kv := range setFlags {
						k, v, found := strings.Cut(kv, "=")
						if !found {
							return fmt.Errorf("invalid --set value %q: expected KEY=VALUE", kv)
						}
						creds[k] = v
					}
					var configResult map[string]interface{}
					if err := c.PostJSON("/api/v1/hub/connectors/"+args[0]+"/configure", map[string]interface{}{"credentials": creds}, &configResult); err != nil {
						return fmt.Errorf("configure: %w", err)
					}
					if ready, ok := configResult["ready"].(bool); ok && !ready {
						fmt.Printf("%s Some requirements still unmet after --set\n", red.Render("!"))
						// Re-check and show table
						var reqs2 map[string]interface{}
						if err := c.GetJSON("/api/v1/hub/connectors/"+args[0]+"/requirements", &reqs2); err == nil {
							printRequirementsTable(reqs2)
						}
						return fmt.Errorf("connector requirements not fully met")
					}
				}
			} else {
				// No --set, no --dry-run: check if requirements are met
				var reqs map[string]interface{}
				if err := c.GetJSON("/api/v1/hub/connectors/"+args[0]+"/requirements", &reqs); err == nil {
					if ready, ok := reqs["ready"].(bool); ok && !ready {
						fmt.Printf("\n  %s has unmet requirements:\n\n", bold.Render(args[0]))
						printRequirementsTable(reqs)
						fmt.Printf("\n  Provide credentials with: agency hub activate %s --set KEY=VALUE\n", args[0])
						fmt.Printf("  Check status with:        agency hub activate %s --dry-run\n\n", args[0])
						return fmt.Errorf("connector requirements not met — use --set to provide credentials")
					}
				}
			}

		activate:
			if err := c.HubActivate(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s %s activated\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	activateCmd.Flags().StringArray("set", nil, "Set credential KEY=VALUE (may be repeated)")
	activateCmd.Flags().Bool("dry-run", false, "Check requirements without activating")
	cmd.AddCommand(activateCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "deactivate <name-or-id>", Short: "Deactivate a component instance",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.HubDeactivate(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s %s deactivated\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	// ── Provider management ──

	providerCmd := &cobra.Command{Use: "provider", Short: "Provider management"}

	providerAddCmd := &cobra.Command{
		Use:   "add <name> <base-url>",
		Short: "Discover and configure a local or custom LLM provider",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			baseURL := args[1]
			credential, _ := cmd.Flags().GetString("credential")
			noProbe, _ := cmd.Flags().GetBool("no-probe")

			var credValue string
			if credential != "" {
				fmt.Printf("Enter API key for %s: ", credential)
				fmt.Scanln(&credValue)
			}

			if noProbe {
				fmt.Println("Skipping discovery. Writing skeleton config...")
				return writeProviderSkeleton(name, baseURL, credential)
			}

			fmt.Printf("Discovering models at %s...\n", baseURL)
			models, err := discoverModels(context.Background(), baseURL, credValue)
			if err != nil {
				return fmt.Errorf("discovery failed: %w\n\nTry --no-probe to skip discovery and write a skeleton config", err)
			}

			if len(models) == 0 {
				return fmt.Errorf("no models found at %s", baseURL)
			}

			fmt.Printf("\nFound %d models:\n\n", len(models))
			for _, m := range models {
				fmt.Printf("  %-30s %s\n", m.ID, strings.Join(m.Capabilities, ", "))
			}

			fmt.Printf("\nWrite to routing.local.yaml? [Y/n] ")
			var confirm string
			fmt.Scanln(&confirm)
			if confirm != "" && strings.ToLower(confirm) != "y" {
				fmt.Println("Cancelled.")
				return nil
			}

			if err := writeProviderConfig(name, baseURL, credential, models); err != nil {
				return err
			}

			if credential != "" && credValue != "" {
				c, err := requireGateway()
				if err == nil {
					c.PostJSON("/api/v1/creds", map[string]interface{}{
						"name":  credential,
						"value": credValue,
					}, nil)
				}
			}

			fmt.Printf("%s Provider %s configured with %d models\n", green.Render("✓"), bold.Render(name), len(models))
			return nil
		},
	}
	providerAddCmd.Flags().String("credential", "", "Credential env var name (e.g., CUSTOM_LLM_API_KEY)")
	providerAddCmd.Flags().Bool("no-probe", false, "Skip model discovery, write skeleton config")
	providerCmd.AddCommand(providerAddCmd)

	cmd.AddCommand(providerCmd)

	return cmd
}

func hubDeploymentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "deployment", Short: "Manage durable hub deployments"}

	var createName string
	var createFromFile string
	var createNonInteractive bool
	createCmd := &cobra.Command{
		Use:  "create <pack>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			configVals, credRefs, err := loadDeploymentInput(createFromFile)
			if err != nil {
				return err
			}
			if !createNonInteractive {
				schemaResp, err := c.HubDeploymentSchema(args[0])
				if err != nil {
					return err
				}
				configVals, credRefs = promptForDeploymentValues(schemaResp["schema"], configVals, credRefs)
			}
			result, err := c.HubDeploymentCreate(args[0], createName, configVals, credRefs)
			if err != nil {
				return err
			}
			fmt.Printf("%s Created deployment %s (%s)\n", green.Render("✓"), valueString(result["name"]), valueString(result["id"]))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createName, "name", "", "Deployment name override")
	createCmd.Flags().StringVar(&createFromFile, "from-file", "", "YAML file with config and credrefs")
	createCmd.Flags().BoolVar(&createNonInteractive, "non-interactive", false, "Require all values via --from-file")
	cmd.AddCommand(createCmd)

	var configureFromFile string
	configureCmd := &cobra.Command{
		Use:  "configure <name-or-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			show, err := c.HubDeploymentShow(args[0])
			if err != nil {
				return err
			}
			deployment, _ := show["deployment"].(map[string]interface{})
			configVals := map[string]interface{}{}
			credRefs := map[string]string{}
			for k, v := range mapValue(deployment["config"]) {
				configVals[k] = v
			}
			for k, v := range mapValue(deployment["credrefs"]) {
				if vm, ok := v.(map[string]interface{}); ok {
					if id, ok := vm["credstore_id"].(string); ok {
						credRefs[k] = id
					}
				}
			}
			fileConfig, fileCredRefs, err := loadDeploymentInput(configureFromFile)
			if err != nil {
				return err
			}
			for k, v := range fileConfig {
				configVals[k] = v
			}
			for k, v := range fileCredRefs {
				credRefs[k] = v
			}
			if configureFromFile == "" {
				configVals, credRefs = promptForDeploymentValues(show["schema"], configVals, credRefs)
			}
			if _, err := c.HubDeploymentConfigure(args[0], configVals, credRefs); err != nil {
				return err
			}
			fmt.Printf("%s Configured deployment %s\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	configureCmd.Flags().StringVar(&configureFromFile, "from-file", "", "YAML file with config and credrefs")
	cmd.AddCommand(configureCmd)

	cmd.AddCommand(&cobra.Command{
		Use:  "list",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			items, err := c.HubDeploymentList()
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println(dim.Render("No deployments"))
				return nil
			}
			fmt.Printf("  %-24s %-12s %-20s %s\n", bold.Render("NAME"), bold.Render("PACK"), bold.Render("OWNER"), bold.Render("ID"))
			for _, item := range items {
				owner := "-"
				if om, ok := item["owner"].(map[string]interface{}); ok {
					owner = valueString(om["agency_name"])
					if owner == "" {
						owner = "-"
					}
				}
				pack := "-"
				if pm, ok := item["pack"].(map[string]interface{}); ok {
					pack = valueString(pm["name"])
				}
				fmt.Printf("  %-24s %-12s %-20s %s\n", valueString(item["name"]), pack, owner, valueString(item["id"]))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:  "show <name-or-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.HubDeploymentShow(args[0])
			if err != nil {
				return err
			}
			out, _ := yaml.Marshal(result)
			fmt.Print(string(out))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:  "validate <name-or-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.HubDeploymentValidate(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Deployment %s is valid\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:  "apply <name-or-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.HubDeploymentApply(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Applied deployment %s\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:  "export <name-or-id> <path>",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.HubDeploymentExport(args[0])
			if err != nil {
				return err
			}
			if err := os.WriteFile(args[1], data, 0o644); err != nil {
				return err
			}
			fmt.Printf("%s Exported deployment %s to %s\n", green.Render("✓"), bold.Render(args[0]), args[1])
			return nil
		},
	})

	var importName string
	importCmd := &cobra.Command{
		Use:  "import <path>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			result, err := c.HubDeploymentImport(data, importName)
			if err != nil {
				return err
			}
			fmt.Printf("%s Imported deployment %s (%s)\n", green.Render("✓"), valueString(result["name"]), valueString(result["id"]))
			return nil
		},
	}
	importCmd.Flags().StringVar(&importName, "name", "", "Override deployment name on import")
	cmd.AddCommand(importCmd)

	var claimForce bool
	claimCmd := &cobra.Command{
		Use:  "claim <name-or-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.HubDeploymentClaim(args[0], claimForce); err != nil {
				return err
			}
			fmt.Printf("%s Claimed deployment %s\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	claimCmd.Flags().BoolVar(&claimForce, "force", false, "Force-claim a stale deployment")
	cmd.AddCommand(claimCmd)

	cmd.AddCommand(&cobra.Command{
		Use:  "release <name-or-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.HubDeploymentRelease(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Released deployment %s\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	var keepInstances bool
	destroyCmd := &cobra.Command{
		Use:  "destroy <name-or-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if _, err := c.HubDeploymentDestroy(args[0], keepInstances); err != nil {
				return err
			}
			fmt.Printf("%s Destroyed deployment %s\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	destroyCmd.Flags().BoolVar(&keepInstances, "keep-instances", false, "Retain child hub instances and clear ownership bindings")
	cmd.AddCommand(destroyCmd)

	return cmd
}

func loadDeploymentInput(path string) (map[string]interface{}, map[string]string, error) {
	configVals := map[string]interface{}{}
	credRefs := map[string]string{}
	if strings.TrimSpace(path) == "" {
		return configVals, credRefs, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var body map[string]interface{}
	if err := yaml.Unmarshal(data, &body); err != nil {
		return nil, nil, err
	}
	if cfg, ok := body["config"].(map[string]interface{}); ok {
		configVals = cfg
	}
	if rawCreds, ok := body["credrefs"].(map[string]interface{}); ok {
		for k, v := range rawCreds {
			credRefs[k] = valueString(v)
		}
	}
	return configVals, credRefs, nil
}

func promptForDeploymentValues(schema interface{}, configVals map[string]interface{}, credRefs map[string]string) (map[string]interface{}, map[string]string) {
	reader := bufio.NewReader(os.Stdin)
	schemaMap, _ := schema.(map[string]interface{})
	if cfgFields, ok := schemaMap["config"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(cfgFields))
		for k := range cfgFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			field, _ := cfgFields[key].(map[string]interface{})
			if _, ok := configVals[key]; ok {
				continue
			}
			desc := valueString(field["description"])
			prompt := key
			if desc != "" {
				prompt += " (" + desc + ")"
			}
			if def, ok := field["default"]; ok && def != nil {
				prompt += fmt.Sprintf(" [%v]", def)
			}
			fmt.Printf("%s: ", prompt)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				if def, ok := field["default"]; ok {
					configVals[key] = def
				}
				continue
			}
			switch valueString(field["type"]) {
			case "int":
				if n, err := strconv.Atoi(line); err == nil {
					configVals[key] = n
				}
			case "bool":
				configVals[key] = strings.EqualFold(line, "true") || strings.EqualFold(line, "yes") || line == "1"
			case "list":
				parts := strings.Split(line, ",")
				items := make([]string, 0, len(parts))
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if part != "" {
						items = append(items, part)
					}
				}
				configVals[key] = items
			default:
				configVals[key] = line
			}
		}
	}
	if credFields, ok := schemaMap["credentials"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(credFields))
		for k := range credFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, ok := credRefs[key]; ok {
				continue
			}
			field, _ := credFields[key].(map[string]interface{})
			desc := valueString(field["description"])
			prompt := key + " credstore key"
			if desc != "" {
				prompt += " (" + desc + ")"
			}
			fmt.Printf("%s: ", prompt)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line != "" {
				credRefs[key] = line
			}
		}
	}
	return configVals, credRefs
}

func valueString(v interface{}) string {
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func mapValue(v interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

// ════════════════════════════════════════════════════════════════════════════
// Teams (grouped)
// ════════════════════════════════════════════════════════════════════════════

func teamCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "team", Short: "Team operations"}

	createTeamCmd := &cobra.Command{
		Use: "create <name>", Short: "Create a team", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			agents, _ := cmd.Flags().GetStringSlice("agents")
			if _, err := c.TeamCreate(args[0], agents); err != nil {
				return err
			}
			fmt.Printf("%s Team %s created\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	createTeamCmd.Flags().StringSlice("agents", nil, "Initial team members")
	cmd.AddCommand(createTeamCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List teams",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			teams, err := c.TeamList()
			if err != nil {
				return err
			}
			if len(teams) == 0 {
				fmt.Println(dim.Render("No teams"))
				return nil
			}
			for _, t := range teams {
				name, _ := t["name"].(string)
				members, _ := t["member_count"].(float64)
				fmt.Printf("  %s %s members\n", bold.Render(fmt.Sprintf("%-20s", name)), dim.Render(fmt.Sprintf("%.0f", members)))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "show <name>", Short: "Show team details", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			team, err := c.TeamShow(args[0])
			if err != nil {
				return err
			}
			prettyPrint(team)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "activity <name>", Short: "Show team activity", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			activity, err := c.TeamActivity(args[0])
			if err != nil {
				return err
			}
			for _, a := range activity {
				ts, _ := a["timestamp"].(string)
				event, _ := a["event"].(string)
				agent, _ := a["agent"].(string)
				tsDisplay := ts
				if len(ts) >= 19 {
					tsDisplay = ts[:19]
				}
				fmt.Printf("  %s  %s %s\n", dim.Render(tsDisplay), cyan.Render(fmt.Sprintf("%-12s", agent)), event)
			}
			return nil
		},
	})

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Capabilities (grouped)
// ════════════════════════════════════════════════════════════════════════════

func capCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cap", Short: "Governed capability access"}

	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List registered capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			caps, err := c.CapList()
			if err != nil {
				return err
			}
			for _, cap := range caps {
				name, _ := cap["name"].(string)
				state, _ := cap["state"].(string)
				fmt.Printf("  %s %s\n", bold.Render(fmt.Sprintf("%-20s", name)), renderState(state))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "show <name>", Short: "Show capability definition", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			cap, err := c.CapShow(args[0])
			if err != nil {
				return err
			}
			prettyPrint(cap)
			return nil
		},
	})

	enableCmd := &cobra.Command{
		Use: "enable <name>", Short: "Enable a capability for use", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			key, _ := cmd.Flags().GetString("key")
			agentsFlag, _ := cmd.Flags().GetStringSlice("agents")
			if err := c.CapEnable(args[0], key, agentsFlag); err != nil {
				return err
			}
			fmt.Printf("%s Capability %s enabled\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	enableCmd.Flags().String("key", "", "API key for the capability, when required")
	enableCmd.Flags().StringSlice("agents", nil, "Optional agent scope")
	cmd.AddCommand(enableCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "disable <name>", Short: "Disable a capability", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.CapDisable(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Capability %s disabled\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	addCmd := &cobra.Command{
		Use: "add <name>", Short: "Register a new capability", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			kind, _ := cmd.Flags().GetString("kind")
			if err := c.CapAdd(kind, args[0], nil); err != nil {
				return err
			}
			fmt.Printf("%s Capability %s added\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	addCmd.Flags().String("kind", "service", "Capability kind")
	cmd.AddCommand(addCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "delete <name>", Short: "Delete a capability definition", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.CapDelete(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Capability %s deleted\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Intake (grouped)
// ════════════════════════════════════════════════════════════════════════════

func intakeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "intake", Short: "Intake work items"}

	itemsCmd := &cobra.Command{
		Use: "items", Short: "List work items",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			connector, _ := cmd.Flags().GetString("connector")
			items, err := c.IntakeItems(connector)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println(dim.Render("No work items"))
				return nil
			}
			for _, item := range items {
				id, _ := item["id"].(string)
				connector, _ := item["connector"].(string)
				status, _ := item["status"].(string)
				priority, _ := item["priority"].(string)
				createdAt, _ := item["created_at"].(string)
				age := relativeTime(createdAt)
				prioCol := ""
				if priority != "" && priority != "normal" {
					prioCol = " " + bold.Render(priority)
				}
				fmt.Printf("  %s  %-18s %s  %s%s\n",
					dim.Render(id), cyan.Render(connector), renderStatePadded(status, 10), dim.Render(age), prioCol)
			}
			return nil
		},
	}
	itemsCmd.Flags().String("connector", "", "Filter by connector")
	cmd.AddCommand(itemsCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "poll [connector]", Short: "Trigger an immediate poll for a connector",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			name := args[0]
			fmt.Printf("  Polling %s...\n", cyan.Render(name))
			var result map[string]interface{}
			if err := c.PostJSON("/api/v1/hub/intake/poll/"+name, nil, &result); err != nil {
				return err
			}
			created, _ := result["work_items_created"].(float64)
			fmt.Printf("  %s %s — %.0f work items created\n", green.Render("✓"), name, created)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "stats", Short: "Show intake statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			stats, err := c.IntakeStats()
			if err != nil {
				return err
			}
			total, _ := stats["total"].(float64)
			fmt.Printf("  %s %s\n", bold.Render("Total:"), fmt.Sprintf("%.0f", total))

			if byStatus, ok := stats["by_status"].(map[string]interface{}); ok {
				fmt.Printf("\n  %s\n", bold.Render("By status:"))
				for status, count := range byStatus {
					c, _ := count.(float64)
					fmt.Printf("    %s %4.0f\n", renderStatePadded(status, 12), c)
				}
			}
			if byConn, ok := stats["by_connector"].(map[string]interface{}); ok {
				fmt.Printf("\n  %s\n", bold.Render("By connector:"))
				for conn, count := range byConn {
					c, _ := count.(float64)
					fmt.Printf("    %-22s %4.0f\n", cyan.Render(conn), c)
				}
			}
			return nil
		},
	})

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Knowledge (grouped)
// ════════════════════════════════════════════════════════════════════════════

func knowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "graph", Short: "Knowledge graph operations"}

	cmd.AddCommand(&cobra.Command{
		Use: "query <text>", Short: "Query the knowledge graph", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			text := strings.Join(args, " ")
			data, err := c.KnowledgeQuery(text)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "who-knows <topic>", Short: "Find who knows about a topic", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			topic := strings.Join(args, " ")
			data, err := c.KnowledgeWhoKnows(topic)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "stats", Short: "Show knowledge graph statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeStats()
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	})

	ingestCmd := &cobra.Command{
		Use:   "ingest <file-or-url>",
		Short: "Ingest content into the knowledge graph",
		Long: `Ingest a file or URL into the knowledge graph.

For files: reads content from disk and detects content type from extension.
For stdin: use "-" as the argument to read from stdin.
For URLs: passes the URL as filename (the knowledge service handles URL classification).`,
		Example: `  agency knowledge ingest report.md
  agency knowledge ingest https://example.com/page
  cat notes.txt | agency knowledge ingest - --type text/plain
  agency knowledge ingest data.json --scope '{"principals":["operator:abc-123"]}'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}

			source := args[0]
			contentType, _ := cmd.Flags().GetString("type")
			scopeStr, _ := cmd.Flags().GetString("scope")

			var content, filename string

			if source == "-" {
				// Read from stdin
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
				content = string(b)
				filename = "stdin"
				if contentType == "" {
					contentType = "text/plain"
				}
			} else if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
				// URL — pass as filename, knowledge service handles it
				filename = source
			} else {
				// File on disk
				b, err := os.ReadFile(source)
				if err != nil {
					return fmt.Errorf("reading file: %w", err)
				}
				content = string(b)
				filename = filepath.Base(source)
				if contentType == "" {
					contentType = mime.TypeByExtension(filepath.Ext(source))
				}
			}

			var scope json.RawMessage
			if scopeStr != "" {
				scope = json.RawMessage(scopeStr)
			}

			data, err := c.KnowledgeIngestWithScope(content, filename, contentType, scope)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	ingestCmd.Flags().String("type", "", "Content type (e.g. text/markdown, application/json)")
	ingestCmd.Flags().String("scope", "", "Scope JSON (e.g. '{\"principals\":[\"operator:uuid\"]}')")
	cmd.AddCommand(ingestCmd)

	insightCmd := &cobra.Command{
		Use:   "insight <text>",
		Short: "Save an insight to the knowledge graph",
		Long:  `Save an agent-generated insight with source nodes, confidence level, and optional tags.`,
		Example: `  agency knowledge insight "lateral movement detected from host-A to host-B" --sources id1,id2 --confidence high
  agency knowledge insight "CVE-2024-1234 affects 3 hosts" --sources n1,n2,n3 --confidence medium --tags risk,security`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			insight := args[0]
			sourcesStr, _ := cmd.Flags().GetString("sources")
			confidence, _ := cmd.Flags().GetString("confidence")
			tagsStr, _ := cmd.Flags().GetString("tags")

			var sources []string
			if sourcesStr != "" {
				sources = strings.Split(sourcesStr, ",")
			}
			var tags []string
			if tagsStr != "" {
				tags = strings.Split(tagsStr, ",")
			}

			data, err := c.KnowledgeSaveInsight(insight, sources, confidence, tags)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	insightCmd.Flags().String("sources", "", "Comma-separated source node IDs")
	insightCmd.Flags().String("confidence", "medium", "Confidence level (low, medium, high)")
	insightCmd.Flags().String("tags", "", "Comma-separated tags")
	cmd.AddCommand(insightCmd)

	// Export knowledge graph
	exportCmd := &cobra.Command{
		Use:   "export [file]",
		Short: "Export the knowledge graph to JSON",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeExport("json")
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if err := os.WriteFile(args[0], data, 0644); err != nil {
					return fmt.Errorf("write file: %w", err)
				}
				fmt.Printf("%s Knowledge graph exported to %s\n", green.Render("✓"), args[0])
				return nil
			}
			fmt.Println(string(data))
			return nil
		},
	}
	cmd.AddCommand(exportCmd)

	importCmd := knowledgeGraphImportCmd("import", "Import knowledge graph from a JSON export")
	cmd.AddCommand(importCmd)
	cmd.AddCommand(knowledgeGraphImportCmd("restore", "Restore knowledge graph from a JSON export"))

	cmd.AddCommand(knowledgeOntologyCmd())
	cmd.AddCommand(knowledgeReviewCmd())
	cmd.AddCommand(knowledgePrincipalsCmd())
	cmd.AddCommand(knowledgeClassificationCmd())

	return cmd
}

func knowledgeGraphImportCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <file>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
			if _, err := c.Post("/api/v1/graph/import", json.RawMessage(data)); err != nil {
				return fmt.Errorf("import: %w", err)
			}
			fmt.Printf("%s Knowledge graph imported from %s\n", green.Render("✓"), args[0])
			return nil
		},
	}
}

func knowledgeClassificationCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "classification", Short: "Classification-based access control"}

	cmd.AddCommand(&cobra.Command{
		Use: "show", Short: "Show current classification config",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeClassification()
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	})

	return cmd
}

func knowledgeReviewCmd() *cobra.Command {
	var approveID string
	var rejectID string
	var reason string

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review pending org-structural knowledge contributions",
		Long: `Review org-structural knowledge contributions submitted by agents.

Without flags, lists all pending contributions awaiting review.
Use --approve or --reject with a contribution ID to act on one.`,
		Example: `  agency knowledge review                    # list pending
  agency knowledge review --approve abc-123  # approve
  agency knowledge review --reject  abc-123 --reason "not accurate"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}

			switch {
			case approveID != "":
				data, err := c.KnowledgeReview(approveID, "approve", reason)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			case rejectID != "":
				data, err := c.KnowledgeReview(rejectID, "reject", reason)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			default:
				// List pending contributions
				data, err := c.KnowledgePending()
				if err != nil {
					return err
				}
				// Try structured display — knowledge service returns {"items": [...]}
				var result struct {
					Items []struct {
						ID          string `json:"id"`
						Label       string `json:"label"`
						Kind        string `json:"kind"`
						Summary     string `json:"summary"`
						SourceAgent string `json:"source_agent"`
						SubmittedAt string `json:"submitted_at"`
					} `json:"items"`
				}
				if json.Unmarshal(data, &result) == nil && result.Items != nil {
					if len(result.Items) == 0 {
						fmt.Println("No pending contributions.")
					} else {
						fmt.Printf("Pending contributions (%d):\n\n", len(result.Items))
						for _, p := range result.Items {
							agent := p.SourceAgent
							if agent == "" {
								agent = "unknown"
							}
							fmt.Printf("  %s  [%s] %s — %s\n", cyan.Render(p.ID), dim.Render(agent), p.Kind, p.Label)
							if p.Summary != "" {
								fmt.Printf("         %s\n", dim.Render(p.Summary))
							}
						}
						fmt.Println()
						fmt.Println("Review with:")
						fmt.Println("  agency knowledge review --approve <id>")
						fmt.Println("  agency knowledge review --reject  <id> --reason <reason>")
					}
				} else {
					fmt.Println(string(data))
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&approveID, "approve", "", "Approve contribution with this ID")
	cmd.Flags().StringVar(&rejectID, "reject", "", "Reject contribution with this ID")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for rejection (optional)")

	return cmd
}

func knowledgePrincipalsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "principals", Short: "Principal registry operations"}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List registered principals",
		Example: `  agency knowledge principals list
  agency knowledge principals list --type operator
  agency knowledge principals list --type agent`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			pType, _ := cmd.Flags().GetString("type")
			data, err := c.KnowledgePrincipals(pType)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	listCmd.Flags().String("type", "", "Filter by principal type (operator, agent, team, role, channel)")
	cmd.AddCommand(listCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "register <type> <name>",
		Short: "Register a new principal",
		Example: `  agency knowledge principals register operator alice
  agency knowledge principals register agent scout
  agency knowledge principals register team security-ops`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeRegisterPrincipal(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	})

	return cmd
}

func knowledgeOntologyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ontology", Short: "Knowledge graph ontology operations"}

	cmd.AddCommand(&cobra.Command{
		Use: "show", Short: "Display active merged ontology",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeOntology()
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "types", Short: "List entity types with descriptions",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeOntologyTypes()
			if err != nil {
				return err
			}
			// Parse and display nicely
			var result struct {
				EntityTypes map[string]struct {
					Description string `json:"description"`
				} `json:"entity_types"`
				Count int `json:"count"`
			}
			if json.Unmarshal(data, &result) == nil {
				fmt.Printf("Entity Types (%d):\n\n", result.Count)
				// Sort for consistent output
				names := make([]string, 0, len(result.EntityTypes))
				for k := range result.EntityTypes {
					names = append(names, k)
				}
				sort.Strings(names)
				for _, name := range names {
					et := result.EntityTypes[name]
					fmt.Printf("  %-20s %s\n", name, et.Description)
				}
			} else {
				fmt.Println(string(data))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "relationships", Short: "List relationship types",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeOntologyRelationships()
			if err != nil {
				return err
			}
			var result struct {
				RelationshipTypes map[string]struct {
					Description string `json:"description"`
					Inverse     string `json:"inverse"`
				} `json:"relationship_types"`
				Count int `json:"count"`
			}
			if json.Unmarshal(data, &result) == nil {
				fmt.Printf("Relationship Types (%d):\n\n", result.Count)
				names := make([]string, 0, len(result.RelationshipTypes))
				for k := range result.RelationshipTypes {
					names = append(names, k)
				}
				sort.Strings(names)
				for _, name := range names {
					rt := result.RelationshipTypes[name]
					fmt.Printf("  %-20s %s (inverse: %s)\n", name, rt.Description, rt.Inverse)
				}
			} else {
				fmt.Println(string(data))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "validate", Short: "Check graph nodes against ontology",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeOntologyValidate()
			if err != nil {
				return err
			}
			var result struct {
				ValidNodes   int `json:"valid_nodes"`
				InvalidNodes int `json:"invalid_nodes"`
				Issues       []struct {
					Kind      string `json:"kind"`
					Count     int    `json:"count"`
					Suggested string `json:"suggested"`
					Action    string `json:"action"`
				} `json:"issues"`
				OntologyVersion int `json:"ontology_version"`
			}
			if json.Unmarshal(data, &result) == nil {
				fmt.Printf("Ontology version: %d\n", result.OntologyVersion)
				fmt.Printf("Valid nodes: %d\n", result.ValidNodes)
				fmt.Printf("Invalid nodes: %d\n", result.InvalidNodes)
				if len(result.Issues) > 0 {
					fmt.Println("\nIssues:")
					for _, issue := range result.Issues {
						fmt.Printf("  Type '%s' (%d nodes) -> suggest '%s'\n",
							issue.Kind, issue.Count, issue.Suggested)
						fmt.Printf("    Fix: agency knowledge ontology migrate %s\n", issue.Action)
					}
				} else {
					fmt.Println("\nAll nodes match the ontology.")
				}
			} else {
				fmt.Println(string(data))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "migrate <from> <to>", Short: "Re-type nodes from one kind to another",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.KnowledgeOntologyMigrate(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	})

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Policy (grouped)
// ════════════════════════════════════════════════════════════════════════════

func policyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "policy", Short: "Agent policy inspection"}

	cmd.AddCommand(&cobra.Command{
		Use: "show <agent>", Short: "Show effective agent policy", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			policy, err := c.PolicyShow(args[0])
			if err != nil {
				return err
			}
			prettyPrint(policy)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "validate <agent>", Short: "Validate effective agent policy", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.PolicyValidate(args[0])
			if err != nil {
				if result == nil {
					return err
				}
			}
			prettyPrint(result)
			return nil
		},
	})

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Admin (grouped)
// ════════════════════════════════════════════════════════════════════════════

func adminCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "admin", Short: "Operator admin and safety checks"}

	cmd.AddCommand(&cobra.Command{
		Use: "doctor", Short: "Run security and runtime safety checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.AdminDoctor()
			if err != nil {
				return err
			}
			if checks, ok := result["checks"].([]interface{}); ok {
				// Group by agent
				currentAgent := ""
				for _, check := range checks {
					if m, ok := check.(map[string]interface{}); ok {
						name, _ := m["name"].(string)
						agent, _ := m["agent"].(string)
						status, _ := m["status"].(string)
						detail, _ := m["detail"].(string)
						if agent != currentAgent {
							if currentAgent != "" {
								fmt.Println()
							}
							fmt.Printf("  %s\n", bold.Render(agent))
							currentAgent = agent
						}
						icon := green.Render("✓")
						if status != "pass" {
							icon = red.Render("✗")
						}
						line := fmt.Sprintf("    %s %s", icon, name)
						if status != "pass" && detail != "" {
							line += dim.Render("  " + detail)
						}
						fmt.Println(line)
					}
				}
			} else {
				prettyPrint(result)
			}

			// Scope audit
			if scopes, ok := result["scopes"].([]interface{}); ok && len(scopes) > 0 {
				fmt.Println()
				fmt.Println(bold.Render("  Service credential scopes:"))
				for _, s := range scopes {
					if m, ok := s.(map[string]interface{}); ok {
						agent, _ := m["agent"].(string)
						service, _ := m["service"].(string)
						fmt.Printf("    %s (%s):\n", bold.Render(agent), dim.Render(service))
						if req, ok := m["required"].([]interface{}); ok && len(req) > 0 {
							strs := make([]string, len(req))
							for i, r := range req {
								strs[i], _ = r.(string)
							}
							fmt.Printf("      required: %s\n", strings.Join(strs, ", "))
						}
						if opt, ok := m["optional"].([]interface{}); ok && len(opt) > 0 {
							strs := make([]string, len(opt))
							for i, o := range opt {
								strs[i], _ = o.(string)
							}
							fmt.Printf("      optional: %s\n", dim.Render(strings.Join(strs, ", ")))
						}
					}
				}
			}
			if unscoped, ok := result["unscoped_agents"].([]interface{}); ok && len(unscoped) > 0 {
				strs := make([]string, len(unscoped))
				for i, u := range unscoped {
					strs[i], _ = u.(string)
				}
				fmt.Printf("    %s Unscoped agents: %s\n", yellow.Render("⚠"), strings.Join(strs, ", "))
			}

			return nil
		},
	})

	var usageSince, usageUntil, usageAgent string
	usageCmd := &cobra.Command{
		Use: "usage", Short: "LLM usage and cost metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.RoutingMetrics(usageAgent, usageSince, usageUntil)
			if err != nil {
				return err
			}
			// Print summary
			if totals, ok := result["totals"].(map[string]interface{}); ok {
				calls, _ := totals["requests"].(float64)
				inputTokens, _ := totals["input_tokens"].(float64)
				outputTokens, _ := totals["output_tokens"].(float64)
				cost, _ := totals["est_cost_usd"].(float64)
				avgLatency, _ := totals["avg_latency_ms"].(float64)
				errors, _ := totals["errors"].(float64)

				fmt.Printf("\n  %s\n\n", bold.Render("LLM Usage Summary"))
				fmt.Printf("  Calls:          %s\n", cyan.Render(fmt.Sprintf("%d", int(calls))))
				fmt.Printf("  Input tokens:   %s\n", cyan.Render(fmt.Sprintf("%d", int(inputTokens))))
				fmt.Printf("  Output tokens:  %s\n", cyan.Render(fmt.Sprintf("%d", int(outputTokens))))
				fmt.Printf("  Estimated cost: %s\n", cyan.Render(fmt.Sprintf("$%.4f", cost)))
				fmt.Printf("  Avg latency:    %s\n", cyan.Render(fmt.Sprintf("%dms", int(avgLatency))))
				if errors > 0 {
					fmt.Printf("  Errors:         %s\n", dim.Render(fmt.Sprintf("%d", int(errors))))
				}
			}
			// Print by-model breakdown
			if byModel, ok := result["by_model"].(map[string]interface{}); ok && len(byModel) > 0 {
				fmt.Printf("\n  %s\n", bold.Render("By Model"))
				for model, data := range byModel {
					if m, ok := data.(map[string]interface{}); ok {
						calls, _ := m["requests"].(float64)
						in, _ := m["input_tokens"].(float64)
						out, _ := m["output_tokens"].(float64)
						cost, _ := m["est_cost_usd"].(float64)
						fmt.Printf("  %-30s %4d calls  in:%7d  out:%7d  $%.4f\n",
							model, int(calls), int(in), int(out), cost)
					}
				}
			}
			// Print by-agent breakdown
			if byAgent, ok := result["by_agent"].(map[string]interface{}); ok && len(byAgent) > 1 {
				fmt.Printf("\n  %s\n", bold.Render("By Agent"))
				for agent, data := range byAgent {
					if m, ok := data.(map[string]interface{}); ok {
						calls, _ := m["requests"].(float64)
						cost, _ := m["est_cost_usd"].(float64)
						fmt.Printf("  %-20s %4d calls  $%.4f\n", agent, int(calls), cost)
					}
				}
			}
			fmt.Println()
			return nil
		},
	}
	usageCmd.Flags().StringVar(&usageAgent, "agent", "", "Filter by agent")
	usageCmd.Flags().StringVar(&usageSince, "since", "", "Start time (ISO 8601 or YYYY-MM-DD)")
	usageCmd.Flags().StringVar(&usageUntil, "until", "", "End time (ISO 8601 or YYYY-MM-DD)")
	cmd.AddCommand(usageCmd)

	// ── Routing optimizer commands ─────────────────────────────────────────
	routingCmd := &cobra.Command{
		Use: "routing", Short: "Provider routing visibility and experimental optimizer controls",
	}

	routingCmd.AddCommand(&cobra.Command{
		Use: "suggestions", Short: "List experimental routing suggestions",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.RoutingSuggestions()
			if err != nil {
				return err
			}
			var suggestions []map[string]interface{}
			if err := json.Unmarshal(data, &suggestions); err != nil {
				fmt.Println(string(data))
				return nil
			}
			if len(suggestions) == 0 {
				fmt.Println("  No routing suggestions.")
				return nil
			}
			fmt.Printf("\n  %s\n\n", bold.Render("Routing Suggestions"))
			for _, s := range suggestions {
				id, _ := s["id"].(string)
				status, _ := s["status"].(string)
				taskType, _ := s["task_type"].(string)
				current, _ := s["current_model"].(string)
				suggested, _ := s["suggested_model"].(string)
				reason, _ := s["reason"].(string)
				savingsPct, _ := s["savings_percent"].(float64)

				statusStyle := dim
				switch status {
				case "pending":
					statusStyle = yellow
				case "approved":
					statusStyle = green
				case "rejected":
					statusStyle = red
				}

				fmt.Printf("  %s  %s\n", cyan.Render(id[:8]), statusStyle.Render("["+status+"]"))
				fmt.Printf("    Task: %s\n", taskType)
				fmt.Printf("    %s → %s  (%.0f%% savings)\n", current, green.Render(suggested), savingsPct*100)
				fmt.Printf("    %s\n\n", dim.Render(reason))
			}
			return nil
		},
	})

	routingCmd.AddCommand(&cobra.Command{
		Use: "approve <suggestion-id>", Short: "Approve an experimental routing suggestion",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.RoutingApprove(args[0])
			if err != nil {
				return err
			}
			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				fmt.Println(string(data))
				return nil
			}
			fmt.Printf("  %s Suggestion %s approved.\n", green.Render("✓"), args[0][:8])
			if taskType, ok := result["task_type"].(string); ok {
				if model, ok := result["suggested_model"].(string); ok {
					fmt.Printf("  Override written: %s → %s\n", taskType, green.Render(model))
				}
			}
			return nil
		},
	})

	routingCmd.AddCommand(&cobra.Command{
		Use: "reject <suggestion-id>", Short: "Reject an experimental routing suggestion",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			_, err = c.RoutingReject(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("  %s Suggestion %s rejected.\n", green.Render("✓"), args[0][:8])
			return nil
		},
	})

	var statsTaskType string
	statsCmd := &cobra.Command{
		Use: "stats", Short: "Per-model routing statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := c.RoutingStats(statsTaskType)
			if err != nil {
				return err
			}
			var stats []map[string]interface{}
			if err := json.Unmarshal(data, &stats); err != nil {
				fmt.Println(string(data))
				return nil
			}
			if len(stats) == 0 {
				fmt.Println("  No routing statistics yet.")
				return nil
			}
			fmt.Printf("\n  %s\n\n", bold.Render("Routing Statistics"))
			fmt.Printf("  %-20s %-25s %6s %8s %10s %10s\n",
				"Task Type", "Model", "Calls", "Success", "Avg Lat", "Cost/1K")
			fmt.Printf("  %-20s %-25s %6s %8s %10s %10s\n",
				"─────────", "─────", "─────", "───────", "───────", "───────")
			for _, s := range stats {
				taskType, _ := s["task_type"].(string)
				model, _ := s["model"].(string)
				calls, _ := s["total_calls"].(float64)
				success, _ := s["success_rate"].(float64)
				latency, _ := s["avg_latency_ms"].(float64)
				costPer1K, _ := s["cost_per_1k"].(float64)
				fmt.Printf("  %-20s %-25s %6d %7.0f%% %8.0fms $%8.4f\n",
					taskType, model, int(calls), success*100, latency, costPer1K)
			}
			fmt.Println()
			return nil
		},
	}
	statsCmd.Flags().StringVar(&statsTaskType, "task-type", "", "Filter by task type")
	routingCmd.AddCommand(statsCmd)

	cmd.AddCommand(routingCmd)

	trustCmd := &cobra.Command{
		Use: "trust [action]", Short: "Experimental trust calibration", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			trustArgs := map[string]string{}
			if len(args) > 1 {
				trustArgs["agent"] = args[1]
			}
			if len(args) > 2 {
				trustArgs["level"] = args[2]
			}
			result, err := c.AdminTrust(args[0], trustArgs)
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	}
	cmd.AddCommand(trustCmd)

	auditCmd := &cobra.Command{
		Use: "audit", Short: "Show audit events for an agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			agent, _ := cmd.Flags().GetString("agent")
			if agent == "" && len(args) > 0 {
				agent = args[0]
			}
			if agent == "" {
				return fmt.Errorf("specify an agent with --agent or as argument")
			}
			events, err := c.AdminAudit(agent)
			if err != nil {
				return err
			}
			for _, e := range events {
				ts, _ := e["timestamp"].(string)
				event, _ := e["event"].(string)
				if len(ts) >= 19 {
					ts = ts[:19]
				}
				fmt.Printf("  %s  %s\n", dim.Render(ts), cyan.Render(event))
			}
			return nil
		},
	}
	auditCmd.Flags().String("agent", "", "Agent to audit")
	cmd.AddCommand(auditCmd)

	egressCmd := &cobra.Command{Use: "egress", Short: "Egress allowlist visibility"}

	egressCmd.AddCommand(&cobra.Command{
		Use: "domains", Short: "List allowed egress domains with provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			var result map[string]interface{}
			if err := c.GetJSON("/api/v1/hub/egress/domains", &result); err != nil {
				return err
			}
			domains, _ := result["domains"].([]interface{})
			if len(domains) == 0 {
				fmt.Println("  No egress domains tracked.")
				return nil
			}
			fmt.Printf("\n  %s\n\n", bold.Render("Egress Domains"))
			for _, d := range domains {
				dm, ok := d.(map[string]interface{})
				if !ok {
					continue
				}
				domain, _ := dm["domain"].(string)
				autoManaged, _ := dm["auto_managed"].(bool)
				sources, _ := dm["sources"].([]interface{})
				managed := ""
				if autoManaged {
					managed = dim.Render(" (auto-managed)")
				}
				fmt.Printf("  %s%s\n", cyan.Render(domain), managed)
				for _, s := range sources {
					if sm, ok := s.(map[string]interface{}); ok {
						sType, _ := sm["type"].(string)
						sName, _ := sm["name"].(string)
						fmt.Printf("    %s %s/%s\n", dim.Render("-"), sType, sName)
					}
				}
			}
			fmt.Println()
			return nil
		},
	})

	egressCmd.AddCommand(&cobra.Command{
		Use: "why <domain>", Short: "Show why a domain is allowed",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			var result map[string]interface{}
			if err := c.GetJSON("/api/v1/hub/egress/domains/"+args[0]+"/provenance", &result); err != nil {
				return fmt.Errorf("domain %q not tracked in provenance", args[0])
			}
			domain, _ := result["domain"].(string)
			autoManaged, _ := result["auto_managed"].(bool)
			sources, _ := result["sources"].([]interface{})
			fmt.Printf("\n  Domain: %s\n", bold.Render(domain))
			if autoManaged {
				fmt.Printf("  Status: %s\n", dim.Render("auto-managed (will be removed on component deactivation)"))
			} else {
				fmt.Printf("  Status: %s\n", dim.Render("operator-managed"))
			}
			fmt.Printf("\n  %s\n", bold.Render("Sources:"))
			for _, s := range sources {
				if sm, ok := s.(map[string]interface{}); ok {
					sType, _ := sm["type"].(string)
					sName, _ := sm["name"].(string)
					addedAt, _ := sm["added_at"].(string)
					if len(addedAt) > 19 {
						addedAt = addedAt[:19]
					}
					fmt.Printf("    %s %s/%s  %s\n", dim.Render("-"), sType, sName, dim.Render(addedAt))
				}
			}
			fmt.Println()
			return nil
		},
	})

	// Keep the generic fallback for other egress actions
	egressCmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := requireGateway()
		if err != nil {
			return err
		}
		action := "status"
		if len(args) > 0 {
			action = args[0]
		}
		result, err := c.AdminEgress(action)
		if err != nil {
			return err
		}
		prettyPrint(result)
		return nil
	}

	cmd.AddCommand(egressCmd)

	destroyCmd := &cobra.Command{
		Use: "destroy", Short: "Destroy all agents and infrastructure",
		RunE: func(cmd *cobra.Command, args []string) error {
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				fmt.Print("This will destroy all agents and infrastructure. Continue? [y/N] ")
				var answer string
				fmt.Scanln(&answer)
				if answer != "y" && answer != "Y" && answer != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}
			c, err := requireGateway()
			if err != nil {
				return err
			}
			fmt.Println(red.Render("Destroying all agents and infrastructure..."))
			if err := c.AdminDestroy(); err != nil {
				return err
			}
			fmt.Printf("%s All destroyed\n", green.Render("✓"))
			return nil
		},
	}
	destroyCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	cmd.AddCommand(destroyCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "rebuild <agent>", Short: "Regenerate all derived config files for an agent",
		Long: `Regenerate all derived config files for an agent without starting or
stopping it. Rebuilds: services-manifest.json, services.yaml, PLATFORM.md,
FRAMEWORK.md, AGENTS.md.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.AdminRebuild(args[0])
			if err != nil {
				return err
			}
			status, _ := result["status"].(string)
			if regen, ok := result["regenerated"].([]interface{}); ok {
				for _, f := range regen {
					fmt.Printf("  %s %v\n", green.Render("✓"), f)
				}
			}
			if errs, ok := result["errors"].([]interface{}); ok {
				for _, e := range errs {
					fmt.Printf("  %s %v\n", red.Render("✗"), e)
				}
			}
			if status == "failed" {
				return fmt.Errorf("rebuild failed")
			}
			fmt.Printf("%s Agent %s rebuilt\n", green.Render("✓"), args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "department [action]", Short: "Experimental department management", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			deptArgs := map[string]string{}
			if len(args) > 1 {
				deptArgs["name"] = args[1]
			}
			result, err := c.AdminDepartment(args[0], deptArgs)
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	})

	knowledgeAdminCmd := &cobra.Command{
		Use:   "knowledge [action]",
		Short: "Knowledge graph admin",
		Long: `Knowledge graph admin operations.

Actions:
  stats              Show graph statistics (nodes, edges, kinds)
  query <text>       Query the knowledge graph
  who-knows <topic>  Find agents with expertise on a topic
  changed-since <ts> Show changes since a timestamp

Also available as top-level: agency knowledge {query|who-knows|stats}`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			kArgs := map[string]string{}
			if len(args) > 1 {
				kArgs["target"] = args[1]
			}
			result, err := c.AdminKnowledge(args[0], kArgs)
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	}
	// agency admin knowledge ontology subcommands
	ontologyCmd := &cobra.Command{
		Use:   "ontology",
		Short: "Ontology emergence operations",
	}
	ontologyCmd.AddCommand(&cobra.Command{
		Use:   "candidates",
		Short: "List candidate ontology values observed but not yet promoted",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.AdminKnowledge("ontology_candidates", map[string]string{})
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	})
	ontologyCmd.AddCommand(&cobra.Command{
		Use:   "promote <value>",
		Short: "Promote a candidate value into the ontology",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.AdminKnowledge("ontology_promote", map[string]string{"value": args[0]})
			if err != nil {
				return err
			}
			fmt.Printf("Promoted %q into ontology\n", args[0])
			prettyPrint(result)
			return nil
		},
	})
	ontologyCmd.AddCommand(&cobra.Command{
		Use:   "reject <value>",
		Short: "Reject a candidate value from the ontology",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.AdminKnowledge("ontology_reject", map[string]string{"value": args[0]})
			if err != nil {
				return err
			}
			fmt.Printf("Rejected %q from ontology\n", args[0])
			prettyPrint(result)
			return nil
		},
	})
	// agency admin knowledge quarantine
	quarantineCmd := &cobra.Command{
		Use:   "quarantine",
		Short: "Quarantine knowledge contributed by an agent",
		Long:  `Quarantine all knowledge nodes contributed by an agent. ASK tenet 16: quarantine is immediate, silent, and complete.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			agent, _ := cmd.Flags().GetString("agent")
			if agent == "" {
				return fmt.Errorf("--agent is required")
			}
			since, _ := cmd.Flags().GetString("since")
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.KnowledgeQuarantine(agent, since)
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	}
	quarantineCmd.Flags().String("agent", "", "Agent name (required)")
	quarantineCmd.Flags().String("since", "", "Only quarantine nodes created since this timestamp")
	knowledgeAdminCmd.AddCommand(quarantineCmd)

	// agency admin knowledge quarantine-release
	quarantineReleaseCmd := &cobra.Command{
		Use:   "quarantine-release",
		Short: "Release quarantined knowledge nodes",
		Long:  `Release quarantined nodes by node ID or by agent name.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _ := cmd.Flags().GetString("node")
			agent, _ := cmd.Flags().GetString("agent")
			if nodeID == "" && agent == "" {
				return fmt.Errorf("--node or --agent is required")
			}
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.KnowledgeQuarantineRelease(nodeID, agent)
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	}
	quarantineReleaseCmd.Flags().String("node", "", "Node ID to release")
	quarantineReleaseCmd.Flags().String("agent", "", "Release all quarantined nodes for this agent")
	knowledgeAdminCmd.AddCommand(quarantineReleaseCmd)

	// agency admin knowledge quarantine-list
	quarantineListCmd := &cobra.Command{
		Use:   "quarantine-list",
		Short: "List quarantined knowledge nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			agent, _ := cmd.Flags().GetString("agent")
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.KnowledgeQuarantineList(agent)
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	}
	quarantineListCmd.Flags().String("agent", "", "Filter by agent name")
	knowledgeAdminCmd.AddCommand(quarantineListCmd)

	knowledgeAdminCmd.AddCommand(ontologyCmd)
	knowledgeAdminCmd.AddCommand(&cobra.Command{
		Use:   "curate",
		Short: "Manually trigger a curation cycle",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			fmt.Print("Running curation cycle...")
			result, err := c.AdminKnowledge("curate", map[string]string{})
			if err != nil {
				fmt.Println()
				return err
			}
			fmt.Printf("\r%s Curation cycle complete\n", green.Render("✓"))
			prettyPrint(result)
			return nil
		},
	})
	cmd.AddCommand(knowledgeAdminCmd)

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Context / Constraints (grouped)
// ════════════════════════════════════════════════════════════════════════════

func contextCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "context", Short: "Agent context and constraint operations"}

	// agency context push <agent> --constraints <file> --reason <reason> [--severity <LEVEL>]
	pushCmd := &cobra.Command{
		Use:   "push <agent>",
		Short: "Push constraint update to an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			constraintsFile, _ := cmd.Flags().GetString("constraints")
			reason, _ := cmd.Flags().GetString("reason")
			severity, _ := cmd.Flags().GetString("severity")

			if constraintsFile == "" {
				return fmt.Errorf("--constraints <file> is required")
			}
			if reason == "" {
				return fmt.Errorf("--reason <reason> is required")
			}

			data, err := os.ReadFile(constraintsFile)
			if err != nil {
				return fmt.Errorf("read constraints file: %w", err)
			}
			var constraints interface{}
			if err := yaml.Unmarshal(data, &constraints); err != nil {
				return fmt.Errorf("parse constraints file: %w", err)
			}

			c, err := requireGateway()
			if err != nil {
				return err
			}

			req := apiclient.ContextPushRequest{
				Constraints: constraints,
				Reason:      reason,
				Severity:    severity,
			}
			result, err := c.ContextPush(args[0], req)
			if err != nil {
				return err
			}

			changeID, _ := result["change_id"].(string)
			version, _ := result["version"].(float64)
			sev, _ := result["severity"].(string)
			status, _ := result["status"].(string)
			fmt.Printf("%s Constraint push queued for %s\n", green.Render("✓"), bold.Render(args[0]))
			if changeID != "" {
				fmt.Printf("  change_id: %s\n", cyan.Render(changeID))
			}
			if version > 0 {
				fmt.Printf("  version:   %d\n", int(version))
			}
			if sev != "" {
				fmt.Printf("  severity:  %s\n", sev)
			}
			if status != "" {
				fmt.Printf("  status:    %s\n", renderState(status))
			}
			return nil
		},
	}
	pushCmd.Flags().String("constraints", "", "Path to YAML constraints file (required)")
	pushCmd.Flags().String("reason", "", "Reason for the constraint change (required)")
	pushCmd.Flags().String("severity", "", "Severity level (e.g. INFO, WARN, CRITICAL)")
	cmd.AddCommand(pushCmd)

	// agency context status <agent>
	cmd.AddCommand(&cobra.Command{
		Use:   "status <agent>",
		Short: "Show current constraint status for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.ContextStatus(args[0])
			if err != nil {
				return err
			}
			prettyPrint(result)
			return nil
		},
	})

	return cmd
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func relativeTime(isoTime string) string {
	if isoTime == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, isoTime)
	if err != nil {
		// Try without timezone
		t, err = time.Parse("2006-01-02T15:04:05", isoTime)
		if err != nil {
			return isoTime
		}
		t = t.UTC()
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func renderStatePadded(s string, width int) string {
	padded := fmt.Sprintf("%-*s", width, s)
	switch s {
	case "running", "available", "healthy", "active", "pass", "routed", "relayed", "resolved":
		return green.Render(padded)
	case "stopped", "disabled", "missing", "inactive", "fail":
		return red.Render(padded)
	case "paused", "halted", "restricted", "pending", "unrouted", "received":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(padded)
	default:
		return dim.Render(padded)
	}
}

func renderState(s string) string {
	switch s {
	case "running", "available", "healthy", "active", "pass":
		return green.Render(s)
	case "stopped", "disabled", "missing", "inactive", "fail":
		return red.Render(s)
	case "paused", "halted", "restricted", "pending":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(s)
	default:
		return dim.Render(s)
	}
}

func prettyPrint(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

// printRequirementsTable displays a connector's requirement status.
func printRequirementsTable(reqs map[string]interface{}) {
	ready, _ := reqs["ready"].(bool)
	connector, _ := reqs["connector"].(string)
	version, _ := reqs["version"].(string)

	fmt.Printf("  %s v%s", bold.Render(connector), version)
	if ready {
		fmt.Printf("  %s\n", green.Render("READY"))
	} else {
		fmt.Printf("  %s\n", red.Render("NOT READY"))
	}
	fmt.Println()

	if creds, ok := reqs["credentials"].([]interface{}); ok && len(creds) > 0 {
		fmt.Printf("  %s\n", bold.Render("Credentials:"))
		for _, c := range creds {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := cm["name"].(string)
			configured, _ := cm["configured"].(bool)
			desc, _ := cm["description"].(string)
			icon := red.Render("✗")
			if configured {
				icon = green.Render("✓")
			}
			line := fmt.Sprintf("    %s %s", icon, name)
			if desc != "" {
				line += dim.Render("  " + desc)
			}
			fmt.Println(line)
		}
	}

	if auth, ok := reqs["auth"].(map[string]interface{}); ok {
		authType, _ := auth["type"].(string)
		authConfigured, _ := auth["configured"].(bool)
		icon := red.Render("✗")
		if authConfigured {
			icon = green.Render("✓")
		}
		fmt.Printf("\n  %s\n", bold.Render("Auth:"))
		fmt.Printf("    %s %s\n", icon, authType)
	}

	if domains, ok := reqs["egress_domains"].([]interface{}); ok && len(domains) > 0 {
		fmt.Printf("\n  %s\n", bold.Render("Egress Domains:"))
		for _, d := range domains {
			dm, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			domain, _ := dm["domain"].(string)
			allowed, _ := dm["allowed"].(bool)
			icon := red.Render("✗")
			if allowed {
				icon = green.Render("✓")
			}
			fmt.Printf("    %s %s\n", icon, domain)
		}
	}
}

// ════════════════════════════════════════════════════════════════════════════
// Missions (grouped)
// ════════════════════════════════════════════════════════════════════════════

func missionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "mission", Short: "Mission operations"}

	cmd.AddCommand(&cobra.Command{
		Use: "create <file>", Short: "Create a mission from a YAML file", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading file: %w", err)
			}
			result, err := c.MissionCreate(data)
			if err != nil {
				return err
			}
			name, _ := result["name"].(string)
			fmt.Printf("%s Mission %s created\n", green.Render("✓"), bold.Render(name))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List missions",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			missions, err := c.MissionList()
			if err != nil {
				return err
			}
			if len(missions) == 0 {
				fmt.Println(dim.Render("No missions"))
				return nil
			}
			fmt.Printf("  %s %s %s %s\n",
				bold.Render(fmt.Sprintf("%-20s", "NAME")),
				bold.Render(fmt.Sprintf("%-12s", "STATUS")),
				bold.Render(fmt.Sprintf("%-20s", "ASSIGNED TO")),
				bold.Render(fmt.Sprintf("%-10s", "TYPE")),
			)
			for _, m := range missions {
				name, _ := m["name"].(string)
				status, _ := m["status"].(string)
				assignedTo, _ := m["assigned_to"].(string)
				assignedType, _ := m["assigned_type"].(string)
				fmt.Printf("  %s %s %s %s\n",
					bold.Render(fmt.Sprintf("%-20s", name)),
					cyan.Render(fmt.Sprintf("%-12s", status)),
					fmt.Sprintf("%-20s", assignedTo),
					dim.Render(fmt.Sprintf("%-10s", assignedType)),
				)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "show <name>", Short: "Show mission details", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			info, err := c.MissionShow(args[0])
			if err != nil {
				return err
			}
			prettyPrint(info)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "health [name]", Short: "Check mission health", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				// All missions health
				var result map[string]interface{}
				if err := c.GetJSON("/api/v1/missions/health", &result); err != nil {
					return err
				}
				missions, _ := result["missions"].([]interface{})
				if len(missions) == 0 {
					fmt.Println(dim.Render("No active missions"))
					return nil
				}
				fmt.Printf("  %-20s %-12s %s\n", bold.Render("NAME"), bold.Render("HEALTH"), bold.Render("SUMMARY"))
				for _, m := range missions {
					mm, _ := m.(map[string]interface{})
					name, _ := mm["mission"].(string)
					status, _ := mm["status"].(string)
					summary, _ := mm["summary"].(string)
					statusColor := green
					if status == "unhealthy" {
						statusColor = red
					} else if status == "degraded" {
						statusColor = yellow
					}
					fmt.Printf("  %-20s %-12s %s\n", name, statusColor.Render(status), dim.Render(summary))
				}
				return nil
			}
			// Single mission health
			var result map[string]interface{}
			if err := c.GetJSON("/api/v1/missions/"+args[0]+"/health", &result); err != nil {
				return err
			}
			status, _ := result["status"].(string)
			summary, _ := result["summary"].(string)
			statusColor := green
			icon := "✓"
			if status == "unhealthy" {
				statusColor = red
				icon = "✗"
			} else if status == "degraded" {
				statusColor = yellow
				icon = "!"
			}
			fmt.Printf("%s %s  %s\n\n", statusColor.Render(icon), bold.Render(args[0]), statusColor.Render(strings.ToUpper(status)))
			checks, _ := result["checks"].([]interface{})
			for _, ch := range checks {
				cc, _ := ch.(map[string]interface{})
				name, _ := cc["name"].(string)
				st, _ := cc["status"].(string)
				detail, _ := cc["detail"].(string)
				fix, _ := cc["fix"].(string)
				checkIcon := green.Render("✓")
				if st == "fail" {
					checkIcon = red.Render("✗")
				} else if st == "warn" {
					checkIcon = yellow.Render("!")
				}
				fmt.Printf("  %s %-22s %s\n", checkIcon, name, detail)
				if fix != "" && st == "fail" {
					fmt.Printf("    %s %s\n", dim.Render("Fix:"), fix)
				}
			}
			if summary != "" {
				fmt.Printf("\n  %s\n", dim.Render(summary))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "update <name> <file>", Short: "Update mission from file (preserves assignment)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[1])
			if err != nil {
				return fmt.Errorf("reading file: %w", err)
			}
			result, err := c.MissionUpdate(args[0], data)
			if err != nil {
				return err
			}
			version, _ := result["version"].(float64)
			status, _ := result["status"].(string)
			assigned, _ := result["assigned_to"].(string)
			fmt.Printf("%s Mission %s updated (v%d, %s", green.Render("✓"), bold.Render(args[0]), int(version), status)
			if assigned != "" {
				fmt.Printf(", assigned to %s", assigned)
			}
			fmt.Println(")")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "delete <name>", Short: "Delete a mission", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.MissionDelete(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Mission %s deleted\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	assignCmd := &cobra.Command{
		Use: "assign <name> <target>", Short: "Assign a mission to an agent or team", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			targetType, _ := cmd.Flags().GetString("type")
			if err := c.MissionAssign(args[0], args[1], targetType); err != nil {
				return err
			}
			fmt.Printf("%s Mission %s assigned to %s\n", green.Render("✓"), bold.Render(args[0]), bold.Render(args[1]))
			return nil
		},
	}
	assignCmd.Flags().String("type", "agent", "Target type (agent or team)")
	cmd.AddCommand(assignCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "pause <name>", Short: "Pause a mission", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.MissionPause(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Mission %s paused\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "resume <name>", Short: "Resume a paused mission", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.MissionResume(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Mission %s resumed\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "complete <name>", Short: "Mark a mission as complete", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.MissionComplete(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Mission %s marked complete\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "history <name>", Short: "Show mission version history", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			entries, err := c.MissionHistory(args[0])
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println(dim.Render("No history"))
				return nil
			}
			for _, e := range entries {
				version, _ := e["version"].(string)
				ts, _ := e["timestamp"].(string)
				actor, _ := e["actor"].(string)
				note, _ := e["note"].(string)
				fmt.Printf("  %s %s %s %s\n",
					bold.Render(fmt.Sprintf("%-10s", version)),
					cyan.Render(fmt.Sprintf("%-24s", ts)),
					fmt.Sprintf("%-20s", actor),
					dim.Render(note),
				)
			}
			return nil
		},
	})

	return cmd
}

func eventCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "event", Short: "Event framework operations"}

	evtListCmd := &cobra.Command{
		Use: "list", Short: "List recent events",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			sourceType, _ := cmd.Flags().GetString("source-type")
			sourceName, _ := cmd.Flags().GetString("source-name")
			eventType, _ := cmd.Flags().GetString("event-type")
			limit, _ := cmd.Flags().GetInt("limit")
			evts, err := c.EventList(sourceType, sourceName, eventType, limit)
			if err != nil {
				return err
			}
			if len(evts) == 0 {
				fmt.Println(dim.Render("No events"))
				return nil
			}
			fmt.Printf("  %s %s %s %s\n",
				bold.Render(fmt.Sprintf("%-16s", "ID")),
				bold.Render(fmt.Sprintf("%-24s", "SOURCE")),
				bold.Render(fmt.Sprintf("%-20s", "TYPE")),
				bold.Render(fmt.Sprintf("%-24s", "TIMESTAMP")),
			)
			for _, e := range evts {
				id, _ := e["id"].(string)
				sType, _ := e["source_type"].(string)
				sName, _ := e["source_name"].(string)
				eType, _ := e["event_type"].(string)
				ts, _ := e["timestamp"].(string)
				source := sType + "/" + sName
				fmt.Printf("  %s %s %s %s\n",
					cyan.Render(fmt.Sprintf("%-16s", id)),
					fmt.Sprintf("%-24s", source),
					bold.Render(fmt.Sprintf("%-20s", eType)),
					dim.Render(fmt.Sprintf("%-24s", ts)),
				)
			}
			return nil
		},
	}
	evtListCmd.Flags().String("source-type", "", "Filter by source type")
	evtListCmd.Flags().String("source-name", "", "Filter by source name")
	evtListCmd.Flags().String("event-type", "", "Filter by event type")
	evtListCmd.Flags().Int("limit", 50, "Max events to show")

	evtShowCmd := &cobra.Command{
		Use: "show <id>", Short: "Show event detail", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			event, err := c.EventShow(args[0])
			if err != nil {
				return err
			}
			prettyPrint(event)
			return nil
		},
	}

	subsCmd := &cobra.Command{
		Use: "subscriptions", Short: "List active subscriptions",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			subs, err := c.SubscriptionList()
			if err != nil {
				return err
			}
			if len(subs) == 0 {
				fmt.Println(dim.Render("No subscriptions"))
				return nil
			}
			fmt.Printf("  %s %s %s %s %s\n",
				bold.Render(fmt.Sprintf("%-20s", "ID")),
				bold.Render(fmt.Sprintf("%-12s", "SOURCE")),
				bold.Render(fmt.Sprintf("%-16s", "EVENT TYPE")),
				bold.Render(fmt.Sprintf("%-12s", "DEST TYPE")),
				bold.Render(fmt.Sprintf("%-20s", "TARGET")),
			)
			for _, s := range subs {
				id, _ := s["id"].(string)
				sType, _ := s["source_type"].(string)
				eType, _ := s["event_type"].(string)
				dest, _ := s["destination"].(map[string]interface{})
				dType := ""
				target := ""
				if dest != nil {
					dType, _ = dest["type"].(string)
					target, _ = dest["target"].(string)
				}
				active, _ := s["active"].(bool)
				idStr := id
				if !active {
					idStr += " (paused)"
				}
				fmt.Printf("  %s %s %s %s %s\n",
					cyan.Render(fmt.Sprintf("%-20s", idStr)),
					fmt.Sprintf("%-12s", sType),
					fmt.Sprintf("%-16s", eType),
					dim.Render(fmt.Sprintf("%-12s", dType)),
					fmt.Sprintf("%-20s", target),
				)
			}
			return nil
		},
	}

	cmd.AddCommand(evtListCmd, evtShowCmd, subsCmd)
	return cmd
}

func webhookCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "webhook", Short: "Manage inbound webhooks"}

	whCreateCmd := &cobra.Command{
		Use: "create <name>", Short: "Register an inbound webhook", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			eventType, _ := cmd.Flags().GetString("event-type")
			result, err := c.WebhookCreate(args[0], eventType)
			if err != nil {
				return err
			}
			secret, _ := result["secret"].(string)
			url, _ := result["url"].(string)
			fmt.Printf("%s Webhook %s created\n", green.Render("✓"), bold.Render(args[0]))
			fmt.Printf("  URL:    %s\n", url)
			fmt.Printf("  Secret: %s\n", bold.Render(secret))
			fmt.Println()
			fmt.Println(dim.Render("  Save the secret — it will not be shown again in list output."))
			return nil
		},
	}
	whCreateCmd.Flags().String("event-type", "", "Event type for this webhook (required)")
	whCreateCmd.MarkFlagRequired("event-type") //nolint:errcheck

	whListCmd := &cobra.Command{
		Use: "list", Short: "List registered webhooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			webhooks, err := c.WebhookList()
			if err != nil {
				return err
			}
			if len(webhooks) == 0 {
				fmt.Println(dim.Render("No webhooks"))
				return nil
			}
			fmt.Printf("  %s %s %s\n",
				bold.Render(fmt.Sprintf("%-24s", "NAME")),
				bold.Render(fmt.Sprintf("%-24s", "EVENT TYPE")),
				bold.Render(fmt.Sprintf("%-40s", "URL")),
			)
			for _, wh := range webhooks {
				whName, _ := wh["name"].(string)
				whEvType, _ := wh["event_type"].(string)
				whURL, _ := wh["url"].(string)
				fmt.Printf("  %s %s %s\n",
					bold.Render(fmt.Sprintf("%-24s", whName)),
					fmt.Sprintf("%-24s", whEvType),
					dim.Render(fmt.Sprintf("%-40s", whURL)),
				)
			}
			return nil
		},
	}

	whShowCmd := &cobra.Command{
		Use: "show <name>", Short: "Show webhook detail", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			wh, err := c.WebhookShow(args[0])
			if err != nil {
				return err
			}
			prettyPrint(wh)
			return nil
		},
	}

	whDeleteCmd := &cobra.Command{
		Use: "delete <name>", Short: "Delete a webhook", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.WebhookDelete(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Webhook %s deleted\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}

	whRotateCmd := &cobra.Command{
		Use: "rotate-secret <name>", Short: "Rotate webhook secret", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.WebhookRotateSecret(args[0])
			if err != nil {
				return err
			}
			secret, _ := result["secret"].(string)
			fmt.Printf("%s Secret rotated for %s\n", green.Render("✓"), bold.Render(args[0]))
			fmt.Printf("  New secret: %s\n", bold.Render(secret))
			return nil
		},
	}

	cmd.AddCommand(whCreateCmd, whListCmd, whShowCmd, whDeleteCmd, whRotateCmd)
	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Meeseeks management
// ════════════════════════════════════════════════════════════════════════════

func meeseeksCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "meeseeks", Short: "Meeseeks ephemeral agent operations"}

	// list
	listCmd := &cobra.Command{
		Use: "list", Short: "List active Meeseeks",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			parent, _ := cmd.Flags().GetString("parent")
			items, err := c.MeeseeksList(parent)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println(dim.Render("No active Meeseeks"))
				return nil
			}
			fmt.Printf("  %s %s %s %s %s %s\n",
				bold.Render(fmt.Sprintf("%-14s", "ID")),
				bold.Render(fmt.Sprintf("%-20s", "PARENT")),
				bold.Render(fmt.Sprintf("%-30s", "TASK")),
				bold.Render(fmt.Sprintf("%-12s", "STATUS")),
				bold.Render(fmt.Sprintf("%-14s", "BUDGET USED")),
				bold.Render("AGE"),
			)
			fmt.Println(dim.Render("  " + strings.Repeat("─", 100)))
			for _, m := range items {
				id, _ := m["id"].(string)
				parent, _ := m["parent_agent"].(string)
				task, _ := m["task"].(string)
				status, _ := m["status"].(string)
				budgetUsed, _ := m["budget_used"].(float64)
				budget, _ := m["budget"].(float64)
				spawnedAt, _ := m["spawned_at"].(string)

				// Truncate task
				taskTrunc := task
				if len(taskTrunc) > 28 {
					taskTrunc = taskTrunc[:25] + "..."
				}
				budgetStr := fmt.Sprintf("$%.3f/$%.3f", budgetUsed, budget)

				age := ""
				if spawnedAt != "" {
					if t, err := time.Parse(time.RFC3339, spawnedAt); err == nil {
						d := time.Since(t).Round(time.Second)
						age = d.String()
					}
				}

				fmt.Printf("  %s %s %s %s %s %s\n",
					bold.Render(fmt.Sprintf("%-14s", id)),
					fmt.Sprintf("%-20s", parent),
					dim.Render(fmt.Sprintf("%-30s", taskTrunc)),
					renderStatePadded(status, 12),
					fmt.Sprintf("%-14s", budgetStr),
					dim.Render(age),
				)
			}
			return nil
		},
	}
	listCmd.Flags().String("parent", "", "Filter by parent agent")
	cmd.AddCommand(listCmd)

	// show
	cmd.AddCommand(&cobra.Command{
		Use: "show <id>", Short: "Show Meeseeks detail", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			info, err := c.MeeseeksShow(args[0])
			if err != nil {
				return err
			}
			prettyPrint(info)
			return nil
		},
	})

	// kill
	killCmd := &cobra.Command{
		Use: "kill [id]", Short: "Terminate a Meeseeks (or all for a parent with --parent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			parent, _ := cmd.Flags().GetString("parent")
			if parent != "" {
				result, err := c.MeeseeksKillByParent(parent)
				if err != nil {
					return err
				}
				killed, _ := result["killed"].([]interface{})
				fmt.Printf("%s Terminated %d Meeseeks for parent %s\n", green.Render("✓"), len(killed), bold.Render(parent))
				return nil
			}
			if len(args) == 0 {
				return fmt.Errorf("id required (or use --parent <agent>)")
			}
			if err := c.MeeseeksKill(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Meeseeks %s terminated\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	killCmd.Flags().String("parent", "", "Terminate all Meeseeks for this parent agent")
	cmd.AddCommand(killCmd)

	return cmd
}

func notificationsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "notify", Aliases: []string{"notifications", "notification"}, Short: "Manage operator notification destinations"}

	listCmd := &cobra.Command{
		Use: "list", Short: "List notification destinations",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			items, err := c.NotificationList()
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println(dim.Render("No notification destinations configured"))
				return nil
			}
			fmt.Printf("  %s %s %s %s\n",
				bold.Render(fmt.Sprintf("%-20s", "NAME")),
				bold.Render(fmt.Sprintf("%-10s", "TYPE")),
				bold.Render(fmt.Sprintf("%-40s", "URL")),
				bold.Render(fmt.Sprintf("%-40s", "EVENTS")),
			)
			for _, item := range items {
				name, _ := item["name"].(string)
				nType, _ := item["type"].(string)
				url, _ := item["url"].(string)
				evts := ""
				if evtList, ok := item["events"].([]interface{}); ok {
					parts := make([]string, len(evtList))
					for i, e := range evtList {
						parts[i], _ = e.(string)
					}
					evts = strings.Join(parts, ", ")
				}
				fmt.Printf("  %s %s %s %s\n",
					cyan.Render(fmt.Sprintf("%-20s", name)),
					fmt.Sprintf("%-10s", nType),
					fmt.Sprintf("%-40s", url),
					dim.Render(fmt.Sprintf("%-40s", evts)),
				)
			}
			return nil
		},
	}

	addCmd := &cobra.Command{
		Use: "add <name>", Short: "Add a notification destination", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			url, _ := cmd.Flags().GetString("url")
			nType, _ := cmd.Flags().GetString("type")
			eventsStr, _ := cmd.Flags().GetString("events")
			var evts []string
			if eventsStr != "" {
				evts = strings.Split(eventsStr, ",")
			}
			result, err := c.NotificationAdd(args[0], nType, url, evts, nil)
			if err != nil {
				return err
			}
			name, _ := result["name"].(string)
			fmt.Printf("%s Notification destination %s added\n", green.Render("✓"), bold.Render(name))
			return nil
		},
	}
	addCmd.Flags().String("url", "", "Notification URL (required)")
	addCmd.Flags().String("type", "", "Type: ntfy or webhook (auto-detected from URL if omitted)")
	addCmd.Flags().String("events", "", "Comma-separated event types (default: operator_alert,enforcer_exited,mission_health_alert)")
	addCmd.MarkFlagRequired("url")

	removeCmd := &cobra.Command{
		Use: "remove <name>", Short: "Remove a notification destination", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.NotificationRemove(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Notification destination %s removed\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}

	testCmd := &cobra.Command{
		Use: "test <name>", Short: "Send a test notification", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.NotificationTest(args[0])
			if err != nil {
				return err
			}
			eventID, _ := result["event_id"].(string)
			fmt.Printf("%s Test notification sent (event: %s)\n", green.Render("✓"), cyan.Render(eventID))
			return nil
		},
	}

	cmd.AddCommand(listCmd, addCmd, removeCmd, testCmd)
	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Audit (grouped)
// ════════════════════════════════════════════════════════════════════════════

func auditCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "audit", Short: "Audit log operations"}

	cmd.AddCommand(&cobra.Command{
		Use: "summarize", Short: "Summarize audit logs into per-mission metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			var result map[string]interface{}
			err = c.PostJSON("/api/v1/admin/audit/summarize", nil, &result)
			if err != nil {
				return err
			}
			metrics, ok := result["metrics"].([]interface{})
			if !ok || len(metrics) == 0 {
				fmt.Println("No audit data found.")
				return nil
			}
			for _, item := range metrics {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				mission, _ := m["mission"].(string)
				date, _ := m["date"].(string)
				activations, _ := m["activations"].(float64)
				inputTokens, _ := m["total_input_tokens"].(float64)
				outputTokens, _ := m["total_output_tokens"].(float64)
				cost, _ := m["estimated_cost_usd"].(float64)
				model, _ := m["model"].(string)
				fmt.Printf("  %s  %s  %s  acts=%d  in=%d  out=%d  $%.4f  model=%s\n",
					bold.Render(mission), dim.Render(date), "",
					int(activations), int(inputTokens), int(outputTokens), cost, model)
			}
			count, _ := result["count"].(float64)
			fmt.Printf("\n%s %d metric(s) returned\n", green.Render("✓"), int(count))
			return nil
		},
	})

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Credential management
// ════════════════════════════════════════════════════════════════════════════

func credentialCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "creds", Short: "Manage governed credentials"}

	// agency credential set
	setCmd := &cobra.Command{
		Use:   "set [name]",
		Short: "Create or update a credential",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			name, _ := cmd.Flags().GetString("name")
			value, _ := cmd.Flags().GetString("value")
			kind, _ := cmd.Flags().GetString("kind")
			scope, _ := cmd.Flags().GetString("scope")
			protocol, _ := cmd.Flags().GetString("protocol")
			service, _ := cmd.Flags().GetString("service")
			group, _ := cmd.Flags().GetString("group")
			externalScopes, _ := cmd.Flags().GetString("external-scopes")
			requires, _ := cmd.Flags().GetString("requires")
			expiresAt, _ := cmd.Flags().GetString("expires")

			nameArg := ""
			if len(args) > 0 {
				nameArg = args[0]
			}
			body, err := buildCredentialSetBody(credentialSetInput{
				NameArg:        nameArg,
				NameFlag:       name,
				Value:          value,
				Kind:           kind,
				Scope:          scope,
				Protocol:       protocol,
				Service:        service,
				Group:          group,
				ExternalScopes: externalScopes,
				Requires:       requires,
				ExpiresAt:      expiresAt,
			})
			if err != nil {
				return err
			}

			result, err := c.CredentialSet(body)
			if err != nil {
				return err
			}
			n, _ := result["name"].(string)
			fmt.Printf("%s Credential %s stored\n", green.Render("✓"), bold.Render(n))
			return nil
		},
	}
	setCmd.Flags().String("name", "", "Credential name (optional when provided as positional argument)")
	setCmd.Flags().String("value", "", "Secret value (required)")
	setCmd.Flags().String("kind", "provider", "Kind: provider, service, gateway, internal")
	setCmd.Flags().String("scope", "platform", "Scope: platform or agent:<name>")
	setCmd.Flags().String("protocol", "api-key", "Protocol: api-key, jwt-exchange, bearer, github-app, oauth2")
	setCmd.Flags().String("service", "", "Optional governed service name")
	setCmd.Flags().String("group", "", "Group name to inherit config from")
	setCmd.Flags().String("external-scopes", "", "Comma-separated external scopes")
	setCmd.Flags().String("requires", "", "Comma-separated dependency credential names")
	setCmd.Flags().String("expires", "", "Expiration in RFC3339 format")
	setCmd.MarkFlagRequired("value")

	// agency credential list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List credentials (values redacted)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			kind, _ := cmd.Flags().GetString("kind")
			scope, _ := cmd.Flags().GetString("scope")
			service, _ := cmd.Flags().GetString("service")
			group, _ := cmd.Flags().GetString("group")
			expiring, _ := cmd.Flags().GetString("expiring")

			params := make(map[string][]string)
			if kind != "" {
				params["kind"] = []string{kind}
			}
			if scope != "" {
				params["scope"] = []string{scope}
			}
			if service != "" {
				params["service"] = []string{service}
			}
			if group != "" {
				params["group"] = []string{group}
			}
			if expiring != "" {
				params["expiring"] = []string{expiring}
			}

			items, err := c.CredentialList(params)
			if err != nil {
				return err
			}

			if len(items) == 0 {
				fmt.Println(dim.Render("No credentials found"))
				return nil
			}

			fmt.Printf("  %s %s %s %s\n",
				bold.Render(fmt.Sprintf("%-30s", "NAME")),
				bold.Render(fmt.Sprintf("%-12s", "KIND")),
				bold.Render(fmt.Sprintf("%-20s", "SCOPE")),
				bold.Render(fmt.Sprintf("%-12s", "PROTOCOL")),
			)
			for _, item := range items {
				name, _ := item["name"].(string)
				meta, _ := item["metadata"].(map[string]interface{})
				if meta == nil {
					meta = map[string]interface{}{}
				}
				k, _ := meta["kind"].(string)
				s, _ := meta["scope"].(string)
				p, _ := meta["protocol"].(string)
				svc, _ := meta["service"].(string)
				line := fmt.Sprintf("  %s %s %s %s",
					cyan.Render(fmt.Sprintf("%-30s", name)),
					fmt.Sprintf("%-12s", k),
					fmt.Sprintf("%-20s", s),
					fmt.Sprintf("%-12s", p),
				)
				if svc != "" {
					line += " service=" + svc
				}
				fmt.Println(line)
			}
			return nil
		},
	}
	listCmd.Flags().String("kind", "", "Filter by kind")
	listCmd.Flags().String("scope", "", "Filter by scope")
	listCmd.Flags().String("service", "", "Filter by service")
	listCmd.Flags().String("group", "", "Filter by group")
	listCmd.Flags().String("expiring", "", "Show credentials expiring within duration (e.g. 7d)")

	// agency credential show NAME
	credShowCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show credential details (value redacted by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			showValue, _ := cmd.Flags().GetBool("show-value")
			result, err := c.CredentialShow(args[0], showValue)
			if err != nil {
				return err
			}
			name, _ := result["name"].(string)
			value, _ := result["value"].(string)
			meta, _ := result["metadata"].(map[string]interface{})
			if meta == nil {
				meta = map[string]interface{}{}
			}

			fmt.Printf("%s\n", bold.Render(name))
			fmt.Printf("  Value:     %s\n", value)
			for _, field := range []struct{ key, label string }{
				{"kind", "Kind"},
				{"scope", "Scope"},
				{"protocol", "Protocol"},
				{"service", "Service"},
				{"group", "Group"},
				{"expires_at", "Expires"},
				{"created_at", "Created"},
				{"rotated_at", "Rotated"},
				{"source", "Source"},
			} {
				if v, ok := meta[field.key].(string); ok && v != "" {
					fmt.Printf("  %-10s %s\n", field.label+":", v)
				}
			}
			return nil
		},
	}
	credShowCmd.Flags().Bool("show-value", false, "Reveal the actual secret value (logged)")

	// agency credential delete NAME
	credDeleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			if err := c.CredentialDelete(args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Credential %s deleted\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}

	// agency credential rotate NAME --value V
	rotateCmd := &cobra.Command{
		Use:   "rotate <name>",
		Short: "Rotate a credential's value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			value, _ := cmd.Flags().GetString("value")
			_, err = c.CredentialRotate(args[0], value)
			if err != nil {
				return err
			}
			fmt.Printf("%s Credential %s rotated\n", green.Render("✓"), bold.Render(args[0]))
			return nil
		},
	}
	rotateCmd.Flags().String("value", "", "New secret value (required)")
	rotateCmd.MarkFlagRequired("value")

	// agency credential test NAME
	testCmd := &cobra.Command{
		Use:   "test <name>",
		Short: "Test credential connectivity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			result, err := c.CredentialTest(args[0])
			if err != nil {
				return err
			}
			ok, _ := result["ok"].(bool)
			msg, _ := result["message"].(string)
			latency, _ := result["latency_ms"].(float64)
			if ok {
				fmt.Printf("%s %s: %s (%dms)\n", green.Render("✓"), bold.Render(args[0]), msg, int(latency))
			} else {
				fmt.Printf("%s %s: %s (%dms)\n", red.Render("✗"), bold.Render(args[0]), msg, int(latency))
			}
			return nil
		},
	}

	// agency credential group create
	groupCmd := &cobra.Command{Use: "group", Short: "Credential group operations"}
	groupCreateCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a credential group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			protocol, _ := cmd.Flags().GetString("protocol")
			tokenURL, _ := cmd.Flags().GetString("token-url")
			requires, _ := cmd.Flags().GetString("requires")

			body := map[string]interface{}{
				"name":     args[0],
				"protocol": protocol,
			}
			if tokenURL != "" {
				body["token_url"] = tokenURL
			}
			if requires != "" {
				body["requires"] = strings.Split(requires, ",")
			}

			result, err := c.CredentialGroupCreate(body)
			if err != nil {
				return err
			}
			n, _ := result["name"].(string)
			fmt.Printf("%s Group %s created\n", green.Render("✓"), bold.Render(n))
			return nil
		},
	}
	groupCreateCmd.Flags().String("protocol", "", "Authentication protocol (required)")
	groupCreateCmd.Flags().String("token-url", "", "Token exchange URL")
	groupCreateCmd.Flags().String("requires", "", "Comma-separated dependency names")
	groupCreateCmd.MarkFlagRequired("protocol")
	groupCmd.AddCommand(groupCreateCmd)

	cmd.AddCommand(setCmd, listCmd, credShowCmd, credDeleteCmd, rotateCmd, testCmd, groupCmd)
	return cmd
}

type credentialSetInput struct {
	NameArg        string
	NameFlag       string
	Value          string
	Kind           string
	Scope          string
	Protocol       string
	Service        string
	Group          string
	ExternalScopes string
	Requires       string
	ExpiresAt      string
}

func buildCredentialSetBody(input credentialSetInput) (map[string]interface{}, error) {
	name := input.NameFlag
	if input.NameArg != "" {
		if name != "" && name != input.NameArg {
			return nil, fmt.Errorf("credential name %q conflicts with --name %q", input.NameArg, name)
		}
		name = input.NameArg
	}
	if name == "" {
		return nil, fmt.Errorf("credential name is required: use agency creds set <name> --value <secret>")
	}
	if input.Value == "" {
		return nil, fmt.Errorf("credential value is required")
	}

	body := map[string]interface{}{
		"name":     name,
		"value":    input.Value,
		"kind":     input.Kind,
		"scope":    input.Scope,
		"protocol": input.Protocol,
	}
	if input.Service != "" {
		body["service"] = input.Service
	}
	if input.Group != "" {
		body["group"] = input.Group
	}
	if input.ExternalScopes != "" {
		body["external_scopes"] = splitCommaList(input.ExternalScopes)
	}
	if input.Requires != "" {
		body["requires"] = splitCommaList(input.Requires)
	}
	if input.ExpiresAt != "" {
		body["expires_at"] = input.ExpiresAt
	}
	return body, nil
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ════════════════════════════════════════════════════════════════════════════
// Cache management
// ════════════════════════════════════════════════════════════════════════════

func cacheCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cache", Short: "Semantic cache management"}

	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear cached task results for an agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			agent, _ := cmd.Flags().GetString("agent")
			if agent == "" {
				return fmt.Errorf("--agent is required")
			}
			c, err := requireGateway()
			if err != nil {
				return err
			}
			var result map[string]interface{}
			if err := c.DeleteJSON("/api/v1/agents/"+agent+"/cache", &result); err != nil {
				return fmt.Errorf("failed to clear cache: %w", err)
			}
			deleted, _ := result["deleted"].(float64)
			fmt.Printf("%s Cache cleared for %s (%d entries removed)\n",
				green.Render("✓"), bold.Render(agent), int(deleted))
			return nil
		},
	}
	clearCmd.Flags().String("agent", "", "Agent name (required)")
	clearCmd.MarkFlagRequired("agent")

	cmd.AddCommand(clearCmd)
	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// Registry (principal identity registry)
// ════════════════════════════════════════════════════════════════════════════

func registryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "registry", Short: "Principal identity registry"}

	// --- list ---
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List registered principals",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			ptype, _ := cmd.Flags().GetString("type")
			data, err := c.RegistryList(ptype)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	listCmd.Flags().String("type", "", "Filter by principal type (agent|operator|team|role|channel)")
	cmd.AddCommand(listCmd)

	// --- show ---
	showCmd := &cobra.Command{
		Use:   "show <name-or-uuid>",
		Short: "Show a principal's registry entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			ptype, _ := cmd.Flags().GetString("type")
			data, err := c.RegistryResolve(args[0], ptype)
			if err != nil {
				return err
			}
			fmt.Println(string(data))

			// Also display effective permissions if we can extract the UUID.
			var entry map[string]interface{}
			if err := json.Unmarshal(data, &entry); err == nil {
				if uuid, ok := entry["uuid"].(string); ok && uuid != "" {
					effData, err := c.RegistryEffective(uuid)
					if err == nil {
						fmt.Println("effective_permissions:", string(effData))
					}
				}
			}
			return nil
		},
	}
	showCmd.Flags().String("type", "agent", "Principal type (used when resolving by name)")
	cmd.AddCommand(showCmd)

	// --- update ---
	updateCmd := &cobra.Command{
		Use:   "update <name-or-uuid>",
		Short: "Update a principal's registry entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			parent, _ := cmd.Flags().GetString("parent")
			status, _ := cmd.Flags().GetString("status")
			if parent == "" && status == "" {
				return fmt.Errorf("at least one of --parent or --status is required")
			}
			// Resolve name to UUID
			ptype, _ := cmd.Flags().GetString("type")
			resolved, err := c.RegistryResolve(args[0], ptype)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", args[0], err)
			}
			var entry map[string]interface{}
			if err := json.Unmarshal(resolved, &entry); err != nil {
				return fmt.Errorf("parse resolve response: %w", err)
			}
			uuid, _ := entry["uuid"].(string)
			if uuid == "" {
				return fmt.Errorf("could not determine UUID for %s", args[0])
			}
			fields := map[string]interface{}{}
			if parent != "" {
				fields["parent_uuid"] = parent
			}
			if status != "" {
				fields["status"] = status
			}
			data, err := c.RegistryUpdate(uuid, fields)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	updateCmd.Flags().String("parent", "", "Parent principal UUID")
	updateCmd.Flags().String("status", "", "Principal status (active|suspended|revoked)")
	updateCmd.Flags().String("type", "agent", "Principal type (used when resolving by name)")
	cmd.AddCommand(updateCmd)

	// --- delete ---
	deleteCmd := &cobra.Command{
		Use:   "delete <name-or-uuid>",
		Short: "Delete a principal from the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := requireGateway()
			if err != nil {
				return err
			}
			// Resolve name to UUID
			ptype, _ := cmd.Flags().GetString("type")
			resolved, err := c.RegistryResolve(args[0], ptype)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", args[0], err)
			}
			var entry map[string]interface{}
			if err := json.Unmarshal(resolved, &entry); err != nil {
				return fmt.Errorf("parse resolve response: %w", err)
			}
			uuid, _ := entry["uuid"].(string)
			if uuid == "" {
				return fmt.Errorf("could not determine UUID for %s", args[0])
			}
			data, err := c.RegistryDelete(uuid)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	deleteCmd.Flags().String("type", "agent", "Principal type (used when resolving by name)")
	cmd.AddCommand(deleteCmd)

	return cmd
}
