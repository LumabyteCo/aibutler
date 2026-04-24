package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// CmdMCP handles the `aibutler mcp` subcommands.
func CmdMCP(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(w, "Usage: aibutler mcp <subcommand>")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Subcommands:")
		fmt.Fprintln(w, "  serve    Start MCP server on stdin/stdout")
		fmt.Fprintln(w, "  tools    List tools exposed via MCP server")
		return nil
	}

	switch args[0] {
	case "serve":
		return cmdMCPServe(app, w)
	case "tools":
		return cmdMCPTools(app, w)
	default:
		return fmt.Errorf("unknown mcp subcommand: %s", args[0])
	}
}

func cmdMCPServe(app *App, w io.Writer) error {
	if app.MCPServer == nil {
		return fmt.Errorf("MCP server not initialized")
	}

	// Redirect log output to stderr so it doesn't interfere with JSON-RPC on stdout.
	log.SetOutput(os.Stderr)

	fmt.Fprintln(os.Stderr, "aibutler: MCP server starting on stdio...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "aibutler: MCP server shutting down...")
		cancel()
	}()

	return app.MCPServer.Serve(ctx, os.Stdin, os.Stdout)
}

func cmdMCPTools(app *App, w io.Writer) error {
	if app.MCPServer == nil {
		return fmt.Errorf("MCP server not initialized")
	}

	tools := app.MCPServer.Tools()
	if len(tools) == 0 {
		fmt.Fprintln(w, "No tools exposed via MCP server.")
		return nil
	}

	fmt.Fprintf(w, "MCP server tools (%d):\n", len(tools))
	for _, t := range tools {
		fmt.Fprintf(w, "  %-30s %s\n", t.Name, t.Description)
	}
	return nil
}
