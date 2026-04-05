package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

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

func runQuickstart(opts quickstartOptions) error {
	fmt.Println()
	fmt.Println(qsBold.Render("Agency Quickstart"))
	fmt.Println(qsDim.Render("Setting up your agent platform"))
	fmt.Println()

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

	fmt.Println()
	fmt.Println(qsGreen.Render("Quickstart complete!"))
	return nil
}
