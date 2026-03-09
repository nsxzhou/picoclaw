package mcpfeishudoc

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/pkg/mcp/feishudoc"
)

func NewMCPFeishuDocCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp-feishu-doc",
		Short: "Run built-in Feishu Docs MCP sidecar",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newServeCommand())

	return cmd
}

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve Feishu Docs MCP tools over stdio",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return serveCmd()
		},
	}

	return cmd
}

func serveCmd() error {
	cfg, err := internal.LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	server, err := feishudoc.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("create feishu docs MCP server: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run feishu docs MCP server: %w", err)
	}
	return nil
}
