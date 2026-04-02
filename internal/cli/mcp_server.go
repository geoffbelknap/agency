package cli

import (
	"fmt"
	"os"

	"github.com/geoffbelknap/agency/internal/mcp"
	"github.com/spf13/cobra"
)

func mcpServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-server",
		Short: "Start the Agency MCP server (stdio)",
		Long:  "Starts the Agency MCP server using stdio JSON-RPC transport.\nThis is used by AI coding assistants (Claude Code, Copilot, Cursor, etc.) to interact with the Agency platform.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := NewClient("http://127.0.0.1:8200")
			proxy := mcp.NewProxy("http://127.0.0.1:8200", client.Token)
			server := mcp.NewProxyServer(proxy)
			if err := server.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
				return err
			}
			return nil
		},
	}
}
