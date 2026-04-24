package cli

import (
	"context"
	"fmt"
	"io"
)

// CmdAgent handles the "agent" command and its subcommands.
func CmdAgent(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler agent <list|status|history> [id]")
	}
	switch args[0] {
	case "list":
		return cmdAgentList(app, w)
	case "status":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler agent status <id>")
		}
		return cmdAgentStatus(app, args[1], w)
	case "history":
		return cmdAgentHistory(app, w)
	default:
		return fmt.Errorf("unknown agent subcommand: %s", args[0])
	}
}

func cmdAgentList(app *App, w io.Writer) error {
	ctx := context.Background()
	rows, err := app.DB.Conn().QueryContext(ctx,
		`SELECT id, type, state, task, model, created_at FROM agents
		 WHERE state IN ('spawned','running','waiting')
		 ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	fmt.Fprintf(w, "%-36s %-12s %-10s %-30s %-20s %s\n", "ID", "TYPE", "STATE", "TASK", "MODEL", "CREATED")
	fmt.Fprintf(w, "%-36s %-12s %-10s %-30s %-20s %s\n", "--", "----", "-----", "----", "-----", "-------")

	count := 0
	for rows.Next() {
		var id, typ, state, task, model, created string
		if err := rows.Scan(&id, &typ, &state, &task, &model, &created); err != nil {
			continue
		}
		if len(task) > 30 {
			task = task[:27] + "..."
		}
		fmt.Fprintf(w, "%-36s %-12s %-10s %-30s %-20s %s\n", id, typ, state, task, model, created)
		count++
	}
	if count == 0 {
		fmt.Fprintln(w, "No active agents.")
	}
	return nil
}

func cmdAgentStatus(app *App, id string, w io.Writer) error {
	ctx := context.Background()
	row := app.DB.Conn().QueryRowContext(ctx,
		`SELECT id, type, state, task, model, tokens_used, cost_usd, tool_calls,
		        duration_ms, created_at, error
		 FROM agents WHERE id = ?`, id)

	var agentID, typ, state, task, model, created string
	var tokensUsed, toolCalls, durationMs int
	var costUSD float64
	var errStr *string

	if err := row.Scan(&agentID, &typ, &state, &task, &model, &tokensUsed,
		&costUSD, &toolCalls, &durationMs, &created, &errStr); err != nil {
		return fmt.Errorf("agent %q not found", id)
	}

	fmt.Fprintln(w, "=== Agent Status ===")
	fmt.Fprintf(w, "  ID:           %s\n", agentID)
	fmt.Fprintf(w, "  Type:         %s\n", typ)
	fmt.Fprintf(w, "  State:        %s\n", state)
	fmt.Fprintf(w, "  Task:         %s\n", task)
	fmt.Fprintf(w, "  Model:        %s\n", model)
	fmt.Fprintf(w, "  Tokens:       %d\n", tokensUsed)
	fmt.Fprintf(w, "  Cost:         $%.4f\n", costUSD)
	fmt.Fprintf(w, "  Tool Calls:   %d\n", toolCalls)
	fmt.Fprintf(w, "  Duration:     %dms\n", durationMs)
	fmt.Fprintf(w, "  Created:      %s\n", created)
	if errStr != nil && *errStr != "" {
		fmt.Fprintf(w, "  Error:        %s\n", *errStr)
	}
	return nil
}

func cmdAgentHistory(app *App, w io.Writer) error {
	ctx := context.Background()
	rows, err := app.DB.Conn().QueryContext(ctx,
		`SELECT id, type, state, task, model, cost_usd, created_at
		 FROM agents ORDER BY created_at DESC LIMIT 25`)
	if err != nil {
		return fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	fmt.Fprintf(w, "%-36s %-12s %-10s %-25s %-15s %10s %s\n", "ID", "TYPE", "STATE", "TASK", "MODEL", "COST", "CREATED")
	fmt.Fprintf(w, "%-36s %-12s %-10s %-25s %-15s %10s %s\n", "--", "----", "-----", "----", "-----", "----", "-------")

	count := 0
	for rows.Next() {
		var id, typ, state, task, model, created string
		var cost float64
		if err := rows.Scan(&id, &typ, &state, &task, &model, &cost, &created); err != nil {
			continue
		}
		if len(task) > 25 {
			task = task[:22] + "..."
		}
		fmt.Fprintf(w, "%-36s %-12s %-10s %-25s %-15s $%.4f %s\n", id, typ, state, task, model, cost, created)
		count++
	}
	if count == 0 {
		fmt.Fprintln(w, "No agent history.")
	}
	return nil
}
