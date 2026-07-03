package eval

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// RunReport aggregates one suite run.
type RunReport struct {
	RunID       int64        `json:"run_id"`
	SuiteHash   string       `json:"suite_hash"`
	Mode        string       `json:"mode"`
	Model       string       `json:"model"`
	TasksTotal  int          `json:"tasks_total"`
	TasksPassed int          `json:"tasks_passed"`
	Results     []TaskResult `json:"results"`
}

// SuccessRate is the fraction of tasks that passed all repeats.
func (r RunReport) SuccessRate() float64 {
	if r.TasksTotal == 0 {
		return 0
	}
	return float64(r.TasksPassed) / float64(r.TasksTotal)
}

// RunSuite executes every task in the suite, judging each repeat and
// recording per-task results and the run row. A task passes only if ALL its
// repeats pass — single-pass success overstates reliability for flaky
// behavior, so consistency is part of the definition.
func RunSuite(ctx context.Context, db *sql.DB, s Suite, r *Runner, mode, model string) (RunReport, error) {
	// The anti-vacuous-pass rules apply to EVERY entry path — programmatic
	// suites get the same validation YAML loading applies.
	for _, t := range s.Tasks {
		if err := ValidateTask(t); err != nil {
			return RunReport{}, fmt.Errorf("eval: invalid suite: %w", err)
		}
	}

	res, err := db.ExecContext(ctx,
		`INSERT INTO eval_runs (suite_hash, model, mode, tasks_total) VALUES (?, ?, ?, ?)`,
		s.Hash, model, mode, len(s.Tasks))
	if err != nil {
		return RunReport{}, fmt.Errorf("eval: record run: %w", err)
	}
	runID, _ := res.LastInsertId()

	report := RunReport{RunID: runID, SuiteHash: s.Hash, Mode: mode, Model: model, TasksTotal: len(s.Tasks)}
	for _, t := range s.Tasks {
		repeats := t.Repeat
		if repeats <= 0 {
			repeats = 1
		}
		agg := TaskResult{TaskID: t.ID, Success: true}
		for i := 0; i < repeats; i++ {
			workspace, err := os.MkdirTemp("", "aibutler-eval-*")
			if err != nil {
				return report, fmt.Errorf("eval: workspace: %w", err)
			}
			trace, runErr := r.RunTask(ctx, t, workspace)
			var one TaskResult
			if runErr != nil {
				one = TaskResult{TaskID: t.ID, Failures: []string{runErr.Error()}}
			} else {
				one = Judge(t, trace, workspace)
			}
			os.RemoveAll(workspace)

			agg.ToolCalls += one.ToolCalls
			agg.ToolErrors += one.ToolErrors
			agg.Retries += one.Retries
			agg.TokensIn += one.TokensIn
			agg.TokensOut += one.TokensOut
			agg.CostUSD += one.CostUSD
			agg.WallMS += one.WallMS
			if !one.Success {
				agg.Success = false
				for _, f := range one.Failures {
					agg.Failures = append(agg.Failures, fmt.Sprintf("repeat %d/%d: %s", i+1, repeats, f))
				}
			}
		}

		if agg.Success {
			report.TasksPassed++
		}
		report.Results = append(report.Results, agg)

		failure := strings.Join(agg.Failures, "; ")
		if len(failure) > 2000 {
			// Truncate on a rune boundary — failures embed arbitrary content.
			cut := 2000
			for cut > 0 && !utf8.RuneStart(failure[cut]) {
				cut--
			}
			failure = failure[:cut] + "…"
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO eval_results
			 (run_id, task_id, success, tool_calls, tool_errors, retries, tokens_in, tokens_out, cost_usd, wall_ms, failure_reason)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
			runID, t.ID, boolToInt(agg.Success), agg.ToolCalls, agg.ToolErrors, agg.Retries,
			agg.TokensIn, agg.TokensOut, agg.CostUSD, agg.WallMS, failure); err != nil {
			return report, fmt.Errorf("eval: record result: %w", err)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		`UPDATE eval_runs SET completed_at = ?, tasks_passed = ? WHERE id = ?`,
		now, report.TasksPassed, runID); err != nil {
		return report, fmt.Errorf("eval: finish run: %w", err)
	}
	return report, nil
}

// Delta compares two runs of the SAME suite hash — the promotion gate for
// any change to skills or heuristics: non-negative success delta and no
// budget regression, or it stays proposed.
type Delta struct {
	SuccessRate float64 `json:"success_rate_delta"`
	ToolCalls   int     `json:"tool_calls_delta"`
	ToolErrors  int     `json:"tool_errors_delta"`
	Tokens      int     `json:"tokens_delta"`
	Comparable  bool    `json:"comparable"` // false when suite hashes differ
}

// CompareRuns computes candidate minus baseline.
func CompareRuns(ctx context.Context, db *sql.DB, baselineID, candidateID int64) (Delta, error) {
	load := func(id int64) (hash string, total, passed, calls, errs, tokens int, err error) {
		err = db.QueryRowContext(ctx,
			`SELECT r.suite_hash, r.tasks_total, r.tasks_passed,
			        COALESCE(SUM(t.tool_calls), 0), COALESCE(SUM(t.tool_errors), 0),
			        COALESCE(SUM(t.tokens_in + t.tokens_out), 0)
			 FROM eval_runs r LEFT JOIN eval_results t ON t.run_id = r.id
			 WHERE r.id = ? GROUP BY r.id`, id).
			Scan(&hash, &total, &passed, &calls, &errs, &tokens)
		return
	}
	bHash, bTotal, bPassed, bCalls, bErrs, bTokens, err := load(baselineID)
	if err != nil {
		return Delta{}, fmt.Errorf("eval: baseline run %d: %w", baselineID, err)
	}
	cHash, cTotal, cPassed, cCalls, cErrs, cTokens, err := load(candidateID)
	if err != nil {
		return Delta{}, fmt.Errorf("eval: candidate run %d: %w", candidateID, err)
	}
	d := Delta{Comparable: bHash == cHash}
	if bTotal > 0 && cTotal > 0 {
		d.SuccessRate = float64(cPassed)/float64(cTotal) - float64(bPassed)/float64(bTotal)
	}
	d.ToolCalls = cCalls - bCalls
	d.ToolErrors = cErrs - bErrs
	d.Tokens = cTokens - bTokens
	return d, nil
}

// ListRuns returns recent runs, newest first.
type RunSummary struct {
	ID          int64  `json:"id"`
	SuiteHash   string `json:"suite_hash"`
	Mode        string `json:"mode"`
	Model       string `json:"model"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	TasksTotal  int    `json:"tasks_total"`
	TasksPassed int    `json:"tasks_passed"`
}

func ListRuns(ctx context.Context, db *sql.DB, limit int) ([]RunSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, suite_hash, mode, COALESCE(model, ''), started_at, COALESCE(completed_at, ''), tasks_total, tasks_passed
		 FROM eval_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("eval: list runs: %w", err)
	}
	defer rows.Close()
	var out []RunSummary
	for rows.Next() {
		var s RunSummary
		if err := rows.Scan(&s.ID, &s.SuiteHash, &s.Mode, &s.Model, &s.StartedAt, &s.CompletedAt, &s.TasksTotal, &s.TasksPassed); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
