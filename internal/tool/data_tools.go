package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// --- task.add ---

type taskAddTool struct{ db *sql.DB }

func (t *taskAddTool) Name() string        { return "task.add" }
func (t *taskAddTool) Description() string  { return "Add a new task to a list" }
func (t *taskAddTool) Capability() string   { return "data.tasks.write" }
func (t *taskAddTool) Schema() string {
	return `{"type":"object","properties":{"content":{"type":"string"},"list":{"type":"string"},"priority":{"type":"integer"}},"required":["content"]}`
}

func (t *taskAddTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Content  string `json:"content"`
		List     string `json:"list"`
		Priority int    `json:"priority"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("task.add: invalid input: %w", err)
	}
	if args.Content == "" {
		return "", fmt.Errorf("task.add: content is required")
	}
	if args.List == "" {
		args.List = "default"
	}

	result, err := t.db.ExecContext(ctx,
		`INSERT INTO user_tasks (list_name, content, status, priority) VALUES (?, ?, 'pending', ?)`,
		args.List, args.Content, args.Priority)
	if err != nil {
		return "", fmt.Errorf("task.add: %w", err)
	}
	id, _ := result.LastInsertId()
	return fmt.Sprintf("Task added (id: %d)", id), nil
}

// --- task.list ---

type taskListTool struct{ db *sql.DB }

func (t *taskListTool) Name() string        { return "task.list" }
func (t *taskListTool) Description() string  { return "List tasks, optionally filtered by list name and status" }
func (t *taskListTool) Capability() string   { return "data.tasks.read" }
func (t *taskListTool) Schema() string {
	return `{"type":"object","properties":{"list":{"type":"string"},"status":{"type":"string"}}}`
}

func (t *taskListTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		List   string `json:"list"`
		Status string `json:"status"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}

	query := "SELECT id, list_name, content, status, priority FROM user_tasks WHERE 1=1"
	var params []interface{}
	if args.List != "" {
		query += " AND list_name = ?"
		params = append(params, args.List)
	}
	if args.Status != "" {
		query += " AND status = ?"
		params = append(params, args.Status)
	}
	query += " ORDER BY priority DESC, created_at DESC LIMIT 50"

	rows, err := t.db.QueryContext(ctx, query, params...)
	if err != nil {
		return "", fmt.Errorf("task.list: %w", err)
	}
	defer rows.Close()

	type task struct {
		ID       int    `json:"id"`
		List     string `json:"list"`
		Content  string `json:"content"`
		Status   string `json:"status"`
		Priority int    `json:"priority"`
	}

	var tasks []task
	for rows.Next() {
		var tk task
		if err := rows.Scan(&tk.ID, &tk.List, &tk.Content, &tk.Status, &tk.Priority); err != nil {
			return "", fmt.Errorf("task.list: scan: %w", err)
		}
		tasks = append(tasks, tk)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("task.list: rows: %w", err)
	}

	out, _ := json.Marshal(tasks)
	return string(out), nil
}

// --- task.complete ---

type taskCompleteTool struct{ db *sql.DB }

func (t *taskCompleteTool) Name() string        { return "task.complete" }
func (t *taskCompleteTool) Description() string  { return "Mark a task as completed by ID" }
func (t *taskCompleteTool) Capability() string   { return "data.tasks.write" }
func (t *taskCompleteTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`
}

func (t *taskCompleteTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("task.complete: invalid input: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := t.db.ExecContext(ctx,
		`UPDATE user_tasks SET status = 'completed', completed_at = ? WHERE id = ?`,
		now, args.ID)
	if err != nil {
		return "", fmt.Errorf("task.complete: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return "Task not found", nil
	}
	return "Task completed", nil
}

// --- expense.log ---

type expenseLogTool struct{ db *sql.DB }

func (t *expenseLogTool) Name() string        { return "expense.log" }
func (t *expenseLogTool) Description() string  { return "Log an expense" }
func (t *expenseLogTool) Capability() string   { return "data.finance.write" }
func (t *expenseLogTool) Schema() string {
	return `{"type":"object","properties":{"amount":{"type":"number"},"category":{"type":"string"},"description":{"type":"string"},"currency":{"type":"string"}},"required":["amount","category"]}`
}

func (t *expenseLogTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Amount      float64 `json:"amount"`
		Category    string  `json:"category"`
		Description string  `json:"description"`
		Currency    string  `json:"currency"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("expense.log: invalid input: %w", err)
	}
	if args.Currency == "" {
		args.Currency = "USD"
	}

	result, err := t.db.ExecContext(ctx,
		`INSERT INTO user_expenses (amount, currency, category, description) VALUES (?, ?, ?, ?)`,
		args.Amount, args.Currency, args.Category, args.Description)
	if err != nil {
		return "", fmt.Errorf("expense.log: %w", err)
	}
	id, _ := result.LastInsertId()
	return fmt.Sprintf("Expense logged (id: %d): %.2f %s in %s", id, args.Amount, args.Currency, args.Category), nil
}

// --- expense.summary ---

type expenseSummaryTool struct{ db *sql.DB }

func (t *expenseSummaryTool) Name() string        { return "expense.summary" }
func (t *expenseSummaryTool) Description() string  { return "Get expense summary by category" }
func (t *expenseSummaryTool) Capability() string   { return "data.finance.read" }
func (t *expenseSummaryTool) Schema() string {
	return `{"type":"object","properties":{"period":{"type":"string","description":"month (YYYY-MM) or date range"}}}`
}

func (t *expenseSummaryTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Period string `json:"period"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}

	query := `SELECT category, SUM(amount) as total, currency, COUNT(*) as count FROM user_expenses`
	var params []interface{}
	if args.Period != "" {
		query += " WHERE date LIKE ?"
		params = append(params, args.Period+"%")
	}
	query += " GROUP BY category, currency ORDER BY total DESC"

	rows, err := t.db.QueryContext(ctx, query, params...)
	if err != nil {
		return "", fmt.Errorf("expense.summary: %w", err)
	}
	defer rows.Close()

	type summary struct {
		Category string  `json:"category"`
		Total    float64 `json:"total"`
		Currency string  `json:"currency"`
		Count    int     `json:"count"`
	}

	var results []summary
	for rows.Next() {
		var s summary
		if err := rows.Scan(&s.Category, &s.Total, &s.Currency, &s.Count); err != nil {
			return "", fmt.Errorf("expense.summary: scan: %w", err)
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("expense.summary: rows: %w", err)
	}

	out, _ := json.Marshal(results)
	return string(out), nil
}

// --- contact.add ---

type contactAddTool struct{ db *sql.DB }

func (t *contactAddTool) Name() string        { return "contact.add" }
func (t *contactAddTool) Description() string  { return "Add a new contact" }
func (t *contactAddTool) Capability() string   { return "data.contacts.write" }
func (t *contactAddTool) Schema() string {
	return `{"type":"object","properties":{"name":{"type":"string"},"email":{"type":"string"},"phone":{"type":"string"},"relationship":{"type":"string"}},"required":["name"]}`
}

func (t *contactAddTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Name         string `json:"name"`
		Email        string `json:"email"`
		Phone        string `json:"phone"`
		Relationship string `json:"relationship"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("contact.add: invalid input: %w", err)
	}
	if args.Name == "" {
		return "", fmt.Errorf("contact.add: name is required")
	}

	result, err := t.db.ExecContext(ctx,
		`INSERT INTO user_contacts (name, email, phone, relationship) VALUES (?, ?, ?, ?)`,
		args.Name, args.Email, args.Phone, args.Relationship)
	if err != nil {
		return "", fmt.Errorf("contact.add: %w", err)
	}
	id, _ := result.LastInsertId()
	return fmt.Sprintf("Contact added: %s (id: %d)", args.Name, id), nil
}

// --- contact.search ---

type contactSearchTool struct{ db *sql.DB }

func (t *contactSearchTool) Name() string        { return "contact.search" }
func (t *contactSearchTool) Description() string  { return "Search contacts by name" }
func (t *contactSearchTool) Capability() string   { return "data.contacts.read" }
func (t *contactSearchTool) Schema() string {
	return `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`
}

func (t *contactSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("contact.search: invalid input: %w", err)
	}

	rows, err := t.db.QueryContext(ctx,
		`SELECT id, name, email, phone, relationship FROM user_contacts WHERE name LIKE ? LIMIT 20`,
		"%"+args.Query+"%")
	if err != nil {
		return "", fmt.Errorf("contact.search: %w", err)
	}
	defer rows.Close()

	type contact struct {
		ID           int     `json:"id"`
		Name         string  `json:"name"`
		Email        *string `json:"email,omitempty"`
		Phone        *string `json:"phone,omitempty"`
		Relationship *string `json:"relationship,omitempty"`
	}

	var contacts []contact
	for rows.Next() {
		var c contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Relationship); err != nil {
			return "", fmt.Errorf("contact.search: scan: %w", err)
		}
		contacts = append(contacts, c)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("contact.search: rows: %w", err)
	}

	out, _ := json.Marshal(contacts)
	return string(out), nil
}

// --- journal.write ---

type journalWriteTool struct{ db *sql.DB }

func (t *journalWriteTool) Name() string        { return "journal.write" }
func (t *journalWriteTool) Description() string  { return "Write a journal entry" }
func (t *journalWriteTool) Capability() string   { return "data.journal.write" }
func (t *journalWriteTool) Schema() string {
	return `{"type":"object","properties":{"content":{"type":"string"},"mood":{"type":"string"},"type":{"type":"string"}},"required":["content"]}`
}

func (t *journalWriteTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Content string `json:"content"`
		Mood    string `json:"mood"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("journal.write: invalid input: %w", err)
	}
	if args.Type == "" {
		args.Type = "journal"
	}

	result, err := t.db.ExecContext(ctx,
		`INSERT INTO user_journal (type, content, mood) VALUES (?, ?, ?)`,
		args.Type, args.Content, args.Mood)
	if err != nil {
		return "", fmt.Errorf("journal.write: %w", err)
	}
	id, _ := result.LastInsertId()
	return fmt.Sprintf("Journal entry saved (id: %d)", id), nil
}

// --- health.log ---

// HealthEncryptor encrypts/decrypts health data values.
type HealthEncryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type healthLogTool struct {
	db  *sql.DB
	enc HealthEncryptor // optional; nil = store plaintext
}

func (t *healthLogTool) Name() string        { return "health.log" }
func (t *healthLogTool) Description() string  { return "Log a health metric" }
func (t *healthLogTool) Capability() string   { return "data.health.write" }
func (t *healthLogTool) Schema() string {
	return `{"type":"object","properties":{"metric":{"type":"string"},"value":{"type":"string"},"unit":{"type":"string"},"notes":{"type":"string"}},"required":["metric","value"]}`
}

func (t *healthLogTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Metric string `json:"metric"`
		Value  string `json:"value"`
		Unit   string `json:"unit"`
		Notes  string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("health.log: invalid input: %w", err)
	}

	valueBytes := []byte(args.Value)
	if t.enc != nil {
		encrypted, err := t.enc.Encrypt(valueBytes)
		if err != nil {
			return "", fmt.Errorf("health.log: encrypt: %w", err)
		}
		valueBytes = encrypted
	}

	result, err := t.db.ExecContext(ctx,
		`INSERT INTO user_health (metric, value, unit, notes) VALUES (?, ?, ?, ?)`,
		args.Metric, valueBytes, args.Unit, args.Notes)
	if err != nil {
		return "", fmt.Errorf("health.log: %w", err)
	}
	id, _ := result.LastInsertId()
	return fmt.Sprintf("Health metric logged: %s = %s %s (id: %d)", args.Metric, args.Value, args.Unit, id), nil
}

// --- health.read ---

type healthReadTool struct {
	db  *sql.DB
	enc HealthEncryptor
}

func (t *healthReadTool) Name() string        { return "health.read" }
func (t *healthReadTool) Description() string { return "Read health metrics with optional decryption" }
func (t *healthReadTool) Capability() string  { return "data.health.read" }
func (t *healthReadTool) Schema() string {
	return `{"type":"object","properties":{"metric":{"type":"string","description":"Filter by metric name"},"limit":{"type":"integer","description":"Max results (default 20)"}}}`
}

func (t *healthReadTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Metric string `json:"metric"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("health.read: %w", err)
	}
	if args.Limit <= 0 {
		args.Limit = 20
	}

	query := `SELECT id, metric, value, unit, notes, created_at FROM user_health`
	var qArgs []interface{}
	if args.Metric != "" {
		query += ` WHERE metric = ?`
		qArgs = append(qArgs, args.Metric)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	qArgs = append(qArgs, args.Limit)

	rows, err := t.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return "", fmt.Errorf("health.read: %w", err)
	}
	defer rows.Close()

	type entry struct {
		ID        int64  `json:"id"`
		Metric    string `json:"metric"`
		Value     string `json:"value"`
		Unit      string `json:"unit"`
		Notes     string `json:"notes"`
		CreatedAt string `json:"created_at"`
	}
	var entries []entry
	for rows.Next() {
		var e entry
		var valueBytes []byte
		if err := rows.Scan(&e.ID, &e.Metric, &valueBytes, &e.Unit, &e.Notes, &e.CreatedAt); err != nil {
			return "", fmt.Errorf("health.read: scan: %w", err)
		}
		// Decrypt if encryptor is available.
		if t.enc != nil {
			decrypted, err := t.enc.Decrypt(valueBytes)
			if err == nil {
				e.Value = string(decrypted)
			} else {
				e.Value = string(valueBytes) // Fallback to raw if decryption fails.
			}
		} else {
			e.Value = string(valueBytes)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("health.read: rows: %w", err)
	}
	if len(entries) == 0 {
		return "No health metrics found.", nil
	}
	data, _ := json.MarshalIndent(entries, "", "  ")
	return string(data), nil
}

// --- cost.status ---

type costStatusTool struct{ db *sql.DB }

func (t *costStatusTool) Name() string        { return "cost.status" }
func (t *costStatusTool) Description() string  { return "Check current month's token usage and cost" }
func (t *costStatusTool) Capability() string   { return "" } // Always available
func (t *costStatusTool) Schema() string {
	return `{"type":"object","properties":{}}`
}

func (t *costStatusTool) Execute(ctx context.Context, _ string) (string, error) {
	now := time.Now().UTC()
	monthStart := fmt.Sprintf("%d-%02d-01", now.Year(), now.Month())

	var totalCost sql.NullFloat64
	var totalInput, totalOutput sql.NullInt64
	err := t.db.QueryRowContext(ctx,
		`SELECT SUM(cost_usd), SUM(input_tokens), SUM(output_tokens) FROM token_usage WHERE timestamp >= ?`,
		monthStart).Scan(&totalCost, &totalInput, &totalOutput)
	if err != nil {
		return "", fmt.Errorf("cost.status: %w", err)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Month: %s", now.Format("January 2006")))
	parts = append(parts, fmt.Sprintf("Cost: $%.4f", totalCost.Float64))
	parts = append(parts, fmt.Sprintf("Input tokens: %d", totalInput.Int64))
	parts = append(parts, fmt.Sprintf("Output tokens: %d", totalOutput.Int64))

	return strings.Join(parts, "\n"), nil
}
