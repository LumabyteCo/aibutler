package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// --- task.remove ---

type taskRemoveTool struct{ db *sql.DB }

func (t *taskRemoveTool) Name() string        { return "task.remove" }
func (t *taskRemoveTool) Description() string  { return "Remove a task by ID" }
func (t *taskRemoveTool) Capability() string   { return "data.tasks.write" }
func (t *taskRemoveTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`
}

func (t *taskRemoveTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("task.remove: invalid input: %w", err)
	}

	result, err := t.db.ExecContext(ctx, `DELETE FROM user_tasks WHERE id = ?`, args.ID)
	if err != nil {
		return "", fmt.Errorf("task.remove: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return "Task not found", nil
	}
	return fmt.Sprintf("Task %d removed", args.ID), nil
}

// --- task.clear ---

type taskClearTool struct{ db *sql.DB }

func (t *taskClearTool) Name() string        { return "task.clear" }
func (t *taskClearTool) Description() string  { return "Clear all completed tasks from a list" }
func (t *taskClearTool) Capability() string   { return "data.tasks.write" }
func (t *taskClearTool) Schema() string {
	return `{"type":"object","properties":{"list":{"type":"string","description":"List name (default: all lists)"}}}`
}

func (t *taskClearTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		List string `json:"list"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}

	query := `DELETE FROM user_tasks WHERE status = 'completed'`
	var params []interface{}
	if args.List != "" {
		query += " AND list_name = ?"
		params = append(params, args.List)
	}

	result, err := t.db.ExecContext(ctx, query, params...)
	if err != nil {
		return "", fmt.Errorf("task.clear: %w", err)
	}
	n, _ := result.RowsAffected()
	return fmt.Sprintf("Cleared %d completed tasks", n), nil
}

// --- task.prioritize ---

type taskPrioritizeTool struct{ db *sql.DB }

func (t *taskPrioritizeTool) Name() string        { return "task.prioritize" }
func (t *taskPrioritizeTool) Description() string  { return "Set priority of a task (0=low, 1=normal, 2=high, 3=urgent)" }
func (t *taskPrioritizeTool) Capability() string   { return "data.tasks.write" }
func (t *taskPrioritizeTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer"},"priority":{"type":"integer","minimum":0,"maximum":3}},"required":["id","priority"]}`
}

func (t *taskPrioritizeTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID       int `json:"id"`
		Priority int `json:"priority"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("task.prioritize: invalid input: %w", err)
	}

	result, err := t.db.ExecContext(ctx,
		`UPDATE user_tasks SET priority = ? WHERE id = ?`,
		args.Priority, args.ID)
	if err != nil {
		return "", fmt.Errorf("task.prioritize: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return "Task not found", nil
	}
	labels := []string{"low", "normal", "high", "urgent"}
	label := "custom"
	if args.Priority >= 0 && args.Priority < len(labels) {
		label = labels[args.Priority]
	}
	return fmt.Sprintf("Task %d priority set to %s (%d)", args.ID, label, args.Priority), nil
}

// --- expense.budget_check ---

type expenseBudgetCheckTool struct{ db *sql.DB }

func (t *expenseBudgetCheckTool) Name() string        { return "expense.budget_check" }
func (t *expenseBudgetCheckTool) Description() string  { return "Check spending against budget for a category" }
func (t *expenseBudgetCheckTool) Capability() string   { return "data.finance.read" }
func (t *expenseBudgetCheckTool) Schema() string {
	return `{"type":"object","properties":{"category":{"type":"string"},"period":{"type":"string","description":"YYYY-MM for monthly"}},"required":["category"]}`
}

func (t *expenseBudgetCheckTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Category string `json:"category"`
		Period   string `json:"period"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("expense.budget_check: invalid input: %w", err)
	}
	if args.Category == "" {
		return "", fmt.Errorf("expense.budget_check: category is required")
	}

	// Get budget for this category.
	var budgetAmt float64
	err := t.db.QueryRowContext(ctx,
		`SELECT amount FROM user_budgets WHERE category = ? AND period = 'monthly'`,
		args.Category).Scan(&budgetAmt)
	if err == sql.ErrNoRows {
		return fmt.Sprintf("No budget set for category %q", args.Category), nil
	}
	if err != nil {
		return "", fmt.Errorf("expense.budget_check: %w", err)
	}

	// Get spending for this period.
	if args.Period == "" {
		args.Period = "%" // all time
	}
	var spent sql.NullFloat64
	err = t.db.QueryRowContext(ctx,
		`SELECT SUM(amount) FROM user_expenses WHERE category = ? AND date LIKE ?`,
		args.Category, args.Period+"%").Scan(&spent)
	if err != nil {
		return "", fmt.Errorf("expense.budget_check: %w", err)
	}

	type budgetCheck struct {
		Category  string  `json:"category"`
		Budget    float64 `json:"budget"`
		Spent     float64 `json:"spent"`
		Remaining float64 `json:"remaining"`
		Percent   float64 `json:"percent_used"`
	}

	check := budgetCheck{
		Category:  args.Category,
		Budget:    budgetAmt,
		Spent:     spent.Float64,
		Remaining: budgetAmt - spent.Float64,
		Percent:   0,
	}
	if budgetAmt > 0 {
		check.Percent = (spent.Float64 / budgetAmt) * 100
	}

	out, _ := json.Marshal(check)
	return string(out), nil
}
