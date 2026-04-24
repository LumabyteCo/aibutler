package prompt

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LumabyteCo/aibutler/internal/config"
)

// UsageEntry records token usage for one model call.
type UsageEntry struct {
	SessionID    string
	Model        string
	Provider     string
	InputTokens  int
	OutputTokens int
	CachedTokens int
	CostUSD      float64
	SkillsLoaded []string
	Tier2Tokens  int
}

// ModelUsage is a breakdown of usage by model.
type ModelUsage struct {
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Calls        int     `json:"calls"`
}

// BudgetAlert is returned when spending crosses a threshold.
type BudgetAlert struct {
	Percentage float64 `json:"percentage"` // 50, 75, 90, 100
	Message    string  `json:"message"`
	Action     string  `json:"action"` // info, warn, pause
}

// Tracker records token usage and checks budgets.
type Tracker struct {
	db  *sql.DB
	cfg *config.Config
}

// NewTracker creates a cost tracker.
func NewTracker(db *sql.DB, cfg *config.Config) *Tracker {
	return &Tracker{db: db, cfg: cfg}
}

// Record inserts a token usage entry.
func (t *Tracker) Record(ctx context.Context, entry UsageEntry) error {
	now := time.Now().UTC().Format(time.RFC3339)
	skillsJSON, _ := json.Marshal(entry.SkillsLoaded)

	_, err := t.db.ExecContext(ctx,
		`INSERT INTO token_usage (timestamp, session_id, model, provider, input_tokens, output_tokens, cached_tokens, cost_usd, skills_loaded, tier2_tokens)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now, entry.SessionID, entry.Model, entry.Provider,
		entry.InputTokens, entry.OutputTokens, entry.CachedTokens,
		entry.CostUSD, string(skillsJSON), entry.Tier2Tokens)
	if err != nil {
		return fmt.Errorf("cost.record: %w", err)
	}
	return nil
}

// MonthlyUsage returns total USD spent this month.
func (t *Tracker) MonthlyUsage(ctx context.Context) (float64, error) {
	monthStart := monthStartStr()
	var total sql.NullFloat64
	err := t.db.QueryRowContext(ctx,
		`SELECT SUM(cost_usd) FROM token_usage WHERE timestamp >= ?`,
		monthStart).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("cost.monthly_usage: %w", err)
	}
	return total.Float64, nil
}

// MonthlyBreakdown returns usage broken down by model.
func (t *Tracker) MonthlyBreakdown(ctx context.Context) ([]ModelUsage, error) {
	monthStart := monthStartStr()
	rows, err := t.db.QueryContext(ctx,
		`SELECT model, SUM(input_tokens), SUM(output_tokens), SUM(cost_usd), COUNT(*)
		 FROM token_usage WHERE timestamp >= ?
		 GROUP BY model ORDER BY SUM(cost_usd) DESC`,
		monthStart)
	if err != nil {
		return nil, fmt.Errorf("cost.monthly_breakdown: %w", err)
	}
	defer rows.Close()

	var results []ModelUsage
	for rows.Next() {
		var m ModelUsage
		if err := rows.Scan(&m.Model, &m.InputTokens, &m.OutputTokens, &m.CostUSD, &m.Calls); err != nil {
			return nil, fmt.Errorf("cost.monthly_breakdown: scan: %w", err)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

// CheckBudget checks if spending has crossed any alert threshold.
// Returns nil if under all thresholds.
func (t *Tracker) CheckBudget(ctx context.Context) (*BudgetAlert, error) {
	budget := t.cfg.Settings.Cost.MonthlyBudget
	if budget <= 0 {
		return nil, nil // No budget set.
	}

	spent, err := t.MonthlyUsage(ctx)
	if err != nil {
		return nil, err
	}

	pct := (spent / budget) * 100
	alerts := t.cfg.Configurations.Cost.Alerts
	if len(alerts) == 0 {
		alerts = []int{50, 75, 90, 100}
	}

	// Find the highest threshold crossed.
	var highestCrossed int
	for _, threshold := range alerts {
		if pct >= float64(threshold) && threshold > highestCrossed {
			highestCrossed = threshold
		}
	}

	if highestCrossed == 0 {
		return nil, nil
	}

	action := "info"
	msg := fmt.Sprintf("You've used $%.2f of your $%.2f budget (%.0f%%).", spent, budget, pct)

	switch {
	case highestCrossed >= 100:
		action = actionForBudgetReached(t.cfg.Configurations.Cost.OnBudgetReached)
		msg += " Budget reached."
	case highestCrossed >= 90:
		action = "warn"
		msg += " Almost at budget limit."
	case highestCrossed >= 75:
		action = "warn"
		msg += " Consider switching to frugal mode."
	}

	return &BudgetAlert{
		Percentage: float64(highestCrossed),
		Message:    msg,
		Action:     action,
	}, nil
}

func actionForBudgetReached(onReached string) string {
	switch onReached {
	case "pause":
		return "pause"
	default:
		return "warn"
	}
}

// ShouldPause checks if the global monthly budget is exhausted and action is "pause".
// This implements the agent.BudgetChecker interface.
func (t *Tracker) ShouldPause(ctx context.Context) bool {
	alert, err := t.CheckBudget(ctx)
	if err != nil || alert == nil {
		return false
	}
	return alert.Action == "pause"
}

// BudgetStatus returns a human-readable alert message and action if a threshold is crossed.
// Returns empty strings if under budget. Satisfies channel.BudgetTracker.
func (t *Tracker) BudgetStatus(ctx context.Context) (string, string) {
	alert, err := t.CheckBudget(ctx)
	if err != nil || alert == nil {
		return "", ""
	}
	return alert.Message, alert.Action
}

func monthStartStr() string {
	now := time.Now().UTC()
	return fmt.Sprintf("%d-%02d-01T00:00:00Z", now.Year(), now.Month())
}
