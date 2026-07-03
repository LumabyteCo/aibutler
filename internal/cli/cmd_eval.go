package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/eval"
)

// CmdEval handles the `aibutler eval` subcommands — the internal benchmark
// harness that turns "did that change help?" into measured numbers.
func CmdEval(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(w, "Usage: aibutler eval <subcommand>")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Subcommands:")
		fmt.Fprintln(w, "  run [--suite <dir>] [--live]   Run the benchmark suite (built-in suite by default;")
		fmt.Fprintln(w, "                                 unit mode by default — deterministic, no model calls)")
		fmt.Fprintln(w, "  list                           List recent runs")
		fmt.Fprintln(w, "  compare <baseline> <candidate> Compare two runs of the same suite")
		return nil
	}

	ctx := context.Background()
	switch args[0] {
	case "run":
		suite, err := eval.DefaultSuite()
		live := false
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--suite":
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
					return fmt.Errorf("--suite requires a directory")
				}
				i++
				suite, err = eval.LoadSuiteDir(args[i])
				if err != nil {
					return err
				}
			case "--live":
				live = true
			default:
				return fmt.Errorf("unknown flag %q (want --suite <dir> or --live)", args[i])
			}
		}
		if err != nil {
			return err
		}

		runner := &eval.Runner{}
		mode := "unit"
		modelName := "scripted"
		if live {
			var adapter agent.ModelAdapter
			adapter, modelName = resolveModelAdapter(app)
			if adapter == nil {
				return fmt.Errorf("eval: no model provider configured for live mode")
			}
			runner.Model = adapter
			mode = "live"
		}

		fmt.Fprintf(w, "Running suite %q (%d tasks, hash %s…) in %s mode\n",
			suite.Name, len(suite.Tasks), suite.Hash[:12], mode)
		report, err := eval.RunSuite(ctx, app.DB.Conn(), suite, runner, mode, modelName)
		if err != nil {
			return err
		}
		printReport(w, report)
		return nil

	case "list":
		runs, err := eval.ListRuns(ctx, app.DB.Conn(), 20)
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			fmt.Fprintln(w, "No eval runs recorded yet. Start with: aibutler eval run")
			return nil
		}
		fmt.Fprintf(w, "%-5s %-6s %-14s %-12s %-8s %s\n", "ID", "MODE", "SUITE", "MODEL", "PASSED", "STARTED")
		for _, r := range runs {
			hash := r.SuiteHash
			if len(hash) > 12 {
				hash = hash[:12] + "…"
			}
			started := r.StartedAt
			if r.CompletedAt == "" {
				started += " (incomplete)"
			}
			fmt.Fprintf(w, "%-5d %-6s %-14s %-12s %d/%-6d %s\n",
				r.ID, r.Mode, hash, r.Model, r.TasksPassed, r.TasksTotal, started)
		}
		return nil

	case "compare":
		if len(args) < 3 {
			return fmt.Errorf("usage: aibutler eval compare <baseline-id> <candidate-id>")
		}
		baseID, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("bad baseline id %q", args[1])
		}
		candID, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("bad candidate id %q", args[2])
		}
		d, err := eval.CompareRuns(ctx, app.DB.Conn(), baseID, candID)
		if err != nil {
			return err
		}
		if !d.Comparable {
			fmt.Fprintln(w, "⚠ Runs used DIFFERENT suite content (hash mismatch) — deltas below are not meaningful.")
		}
		fmt.Fprintf(w, "Success rate: %+.1f%%\n", d.SuccessRate*100)
		fmt.Fprintf(w, "Tool calls:   %+d\n", d.ToolCalls)
		fmt.Fprintf(w, "Tool errors:  %+d\n", d.ToolErrors)
		fmt.Fprintf(w, "Tokens:       %+d\n", d.Tokens)
		return nil

	default:
		return fmt.Errorf("unknown eval subcommand: %s", args[0])
	}
}

func printReport(w io.Writer, report eval.RunReport) {
	for _, res := range report.Results {
		status := "PASS"
		if !res.Success {
			status = "FAIL"
		}
		fmt.Fprintf(w, "  [%s] %-24s calls=%d errors=%d retries=%d %dms\n",
			status, res.TaskID, res.ToolCalls, res.ToolErrors, res.Retries, res.WallMS)
		for _, f := range res.Failures {
			fmt.Fprintf(w, "         └─ %s\n", f)
		}
	}
	fmt.Fprintf(w, "Run %d: %d/%d passed (%.0f%%)\n",
		report.RunID, report.TasksPassed, report.TasksTotal, report.SuccessRate()*100)
}
