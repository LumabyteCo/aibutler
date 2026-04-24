package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
)

// CmdCost handles the "cost" command and its subcommands.
func CmdCost(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler cost <status|history|breakdown|strategy|budget> [args]")
	}
	switch args[0] {
	case "status":
		return cmdCostStatus(app, w)
	case "history":
		return cmdCostHistory(app, w)
	case "breakdown":
		return cmdCostBreakdown(app, w)
	case "strategy":
		if len(args) < 2 {
			fmt.Fprintf(w, "Current strategy: %s\n", app.Config.Settings.Cost.Strategy)
			return nil
		}
		return cmdCostStrategy(app, args[1], w)
	case "budget":
		if len(args) < 2 {
			fmt.Fprintf(w, "Monthly budget: $%.2f\n", app.Config.Settings.Cost.MonthlyBudget)
			return nil
		}
		return cmdCostBudget(app, args[1], w)
	default:
		return fmt.Errorf("unknown cost subcommand: %s", args[0])
	}
}

func cmdCostStatus(app *App, w io.Writer) error {
	ctx := context.Background()
	usage, err := app.Tracker.MonthlyUsage(ctx)
	if err != nil {
		return fmt.Errorf("get monthly usage: %w", err)
	}
	budget := app.Config.Settings.Cost.MonthlyBudget
	remaining := budget - usage
	if remaining < 0 {
		remaining = 0
	}

	fmt.Fprintln(w, "=== Cost Status ===")
	fmt.Fprintf(w, "  This Month:    $%.4f\n", usage)
	fmt.Fprintf(w, "  Budget:        $%.2f\n", budget)
	fmt.Fprintf(w, "  Remaining:     $%.4f\n", remaining)
	fmt.Fprintf(w, "  Strategy:      %s\n", app.Config.Settings.Cost.Strategy)

	alert, err := app.Tracker.CheckBudget(ctx)
	if err == nil && alert != nil {
		fmt.Fprintf(w, "  Alert:         %s (%.0f%% used)\n", alert.Action, alert.Percentage)
	}
	return nil
}

func cmdCostHistory(app *App, w io.Writer) error {
	ctx := context.Background()
	usage, err := app.Tracker.MonthlyUsage(ctx)
	if err != nil {
		return fmt.Errorf("get monthly usage: %w", err)
	}
	fmt.Fprintln(w, "=== Cost History ===")
	fmt.Fprintf(w, "  Current Month: $%.4f\n", usage)
	return nil
}

func cmdCostBreakdown(app *App, w io.Writer) error {
	ctx := context.Background()
	breakdown, err := app.Tracker.MonthlyBreakdown(ctx)
	if err != nil {
		return fmt.Errorf("get breakdown: %w", err)
	}
	if len(breakdown) == 0 {
		fmt.Fprintln(w, "No usage this month.")
		return nil
	}

	fmt.Fprintf(w, "%-30s %10s %12s %12s %10s\n", "MODEL", "CALLS", "INPUT", "OUTPUT", "COST")
	fmt.Fprintf(w, "%-30s %10s %12s %12s %10s\n", "-----", "-----", "-----", "------", "----")
	for _, m := range breakdown {
		fmt.Fprintf(w, "%-30s %10d %12d %12d $%.4f\n",
			m.Model, m.Calls, m.InputTokens, m.OutputTokens, m.CostUSD)
	}
	return nil
}

func cmdCostStrategy(app *App, strategy string, w io.Writer) error {
	switch strategy {
	case "frugal", "balanced", "quality":
		app.Config.Settings.Cost.Strategy = strategy
		fmt.Fprintf(w, "Cost strategy set to: %s\n", strategy)
		return nil
	default:
		return fmt.Errorf("invalid strategy: %s (valid: frugal, balanced, quality)", strategy)
	}
}

func cmdCostBudget(app *App, amount string, w io.Writer) error {
	budget, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		return fmt.Errorf("invalid budget amount: %s", amount)
	}
	if budget < 0 {
		return fmt.Errorf("budget cannot be negative")
	}
	app.Config.Settings.Cost.MonthlyBudget = budget
	fmt.Fprintf(w, "Monthly budget set to: $%.2f\n", budget)
	return nil
}
