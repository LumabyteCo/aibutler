// Package transaction implements a multi-phase transaction engine with spending limits.
// Transactions follow a prepare -> confirm -> execute lifecycle to prevent accidental spending.
package transaction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Phase represents a transaction lifecycle phase.
type Phase string

const (
	PhasePrepare Phase = "prepare"  // search, compare — no money
	PhaseConfirm Phase = "confirm"  // user reviews details
	PhaseExecute Phase = "execute"  // actual booking/order
	PhaseCancel  Phase = "cancel"
)

// Status constants.
const (
	StatusPending   = "pending"
	StatusConfirmed = "confirmed"
	StatusExecuted  = "executed"
	StatusCancelled = "cancelled"
)

// Errors.
var (
	ErrConfirmationRequired = errors.New("transaction must be confirmed before execution")
	ErrNotFound             = errors.New("transaction not found")
	ErrAlreadyExecuted      = errors.New("transaction already executed")
	ErrAlreadyCancelled     = errors.New("transaction already cancelled")
	ErrLimitExceeded        = errors.New("spending limit exceeded")
)

// Transaction represents a transactional action (booking, order, etc.).
type Transaction struct {
	ID          string     `json:"id"`
	Service     string     `json:"service"`     // "opentable", "uber", "doordash"
	Category    string     `json:"category"`    // "restaurant", "rideshare", "delivery"
	Phase       Phase      `json:"phase"`
	Details     string     `json:"details"`     // JSON details (items, price, etc.)
	Amount      float64    `json:"amount"`      // total cost
	Status      string     `json:"status"`      // "pending", "confirmed", "executed", "cancelled"
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	ExecutedAt  *time.Time `json:"executed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// SpendingLimits controls per-transaction and daily spending caps.
type SpendingLimits struct {
	PerTransaction float64 // max per single transaction (0 = unlimited)
	DailyTotal     float64 // max daily spend (0 = unlimited)
}

// Engine manages the transaction lifecycle.
type Engine struct {
	db     *sql.DB
	limits SpendingLimits
}

// New creates a new transaction Engine.
func New(db *sql.DB, limits SpendingLimits) *Engine {
	return &Engine{db: db, limits: limits}
}

// Prepare creates a new transaction in the prepare phase.
func (e *Engine) Prepare(ctx context.Context, service, category, details string) (*Transaction, error) {
	id := fmt.Sprintf("tx_%d", time.Now().UnixNano())

	var amount float64
	var parsed struct {
		Amount float64 `json:"amount"`
	}
	if err := json.Unmarshal([]byte(details), &parsed); err == nil {
		amount = parsed.Amount
	}
	if amount < 0 {
		return nil, fmt.Errorf("transaction.prepare: negative amount %.2f not allowed", amount)
	}

	now := time.Now()
	_, err := e.db.ExecContext(ctx,
		`INSERT INTO transactions (id, service, category, phase, details, amount, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, service, category, string(PhasePrepare), details, amount, StatusPending, now)
	if err != nil {
		return nil, fmt.Errorf("transaction.prepare: %w", err)
	}

	e.audit(ctx, id, "prepare", details)

	return &Transaction{
		ID:        id,
		Service:   service,
		Category:  category,
		Phase:     PhasePrepare,
		Details:   details,
		Amount:    amount,
		Status:    StatusPending,
		CreatedAt: now,
	}, nil
}

// Confirm moves a transaction from prepare to confirmed status.
func (e *Engine) Confirm(ctx context.Context, txID string) (*Transaction, error) {
	tx, err := e.get(ctx, txID)
	if err != nil {
		return nil, err
	}

	if tx.Status == StatusCancelled {
		return nil, ErrAlreadyCancelled
	}
	if tx.Status == StatusExecuted {
		return nil, ErrAlreadyExecuted
	}

	now := time.Now()
	_, err = e.db.ExecContext(ctx,
		`UPDATE transactions SET phase = ?, status = ?, confirmed_at = ? WHERE id = ?`,
		string(PhaseConfirm), StatusConfirmed, now, txID)
	if err != nil {
		return nil, fmt.Errorf("transaction.confirm: %w", err)
	}

	tx.Phase = PhaseConfirm
	tx.Status = StatusConfirmed
	tx.ConfirmedAt = &now

	e.audit(ctx, txID, "confirm", "")

	return tx, nil
}

// Execute finalizes a confirmed transaction. Returns ErrConfirmationRequired if not confirmed.
func (e *Engine) Execute(ctx context.Context, txID string) (*Transaction, error) {
	tx, err := e.get(ctx, txID)
	if err != nil {
		return nil, err
	}

	if tx.Status == StatusCancelled {
		return nil, ErrAlreadyCancelled
	}
	if tx.Status == StatusExecuted {
		return nil, ErrAlreadyExecuted
	}
	if tx.Status != StatusConfirmed {
		return nil, ErrConfirmationRequired
	}

	// Check spending limits before executing.
	if err := e.CheckLimits(ctx, tx.Amount); err != nil {
		return nil, err
	}

	now := time.Now()
	_, err = e.db.ExecContext(ctx,
		`UPDATE transactions SET phase = ?, status = ?, executed_at = ? WHERE id = ?`,
		string(PhaseExecute), StatusExecuted, now, txID)
	if err != nil {
		return nil, fmt.Errorf("transaction.execute: %w", err)
	}

	tx.Phase = PhaseExecute
	tx.Status = StatusExecuted
	tx.ExecutedAt = &now

	e.audit(ctx, txID, "execute", fmt.Sprintf("amount=%.2f", tx.Amount))

	return tx, nil
}

// Cancel cancels a transaction.
func (e *Engine) Cancel(ctx context.Context, txID string) error {
	tx, err := e.get(ctx, txID)
	if err != nil {
		return err
	}

	if tx.Status == StatusExecuted {
		return ErrAlreadyExecuted
	}
	if tx.Status == StatusCancelled {
		return nil // idempotent
	}

	_, err = e.db.ExecContext(ctx,
		`UPDATE transactions SET phase = ?, status = ? WHERE id = ?`,
		string(PhaseCancel), StatusCancelled, txID)
	if err != nil {
		return fmt.Errorf("transaction.cancel: %w", err)
	}

	e.audit(ctx, txID, "cancel", "")
	return nil
}

// CheckLimits verifies the amount doesn't exceed spending limits.
func (e *Engine) CheckLimits(ctx context.Context, amount float64) error {
	if amount < 0 {
		return fmt.Errorf("%w: negative amount %.2f", ErrLimitExceeded, amount)
	}
	if e.limits.PerTransaction > 0 && amount > e.limits.PerTransaction {
		return fmt.Errorf("%w: amount %.2f exceeds per-transaction limit %.2f",
			ErrLimitExceeded, amount, e.limits.PerTransaction)
	}

	if e.limits.DailyTotal > 0 {
		daily, err := e.DailySpend(ctx)
		if err != nil {
			return fmt.Errorf("check daily spend: %w", err)
		}
		if daily+amount > e.limits.DailyTotal {
			return fmt.Errorf("%w: daily spend %.2f + %.2f exceeds daily limit %.2f",
				ErrLimitExceeded, daily, amount, e.limits.DailyTotal)
		}
	}

	return nil
}

// DailySpend returns the total amount spent today.
func (e *Engine) DailySpend(ctx context.Context) (float64, error) {
	var total float64
	err := e.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM transactions
		 WHERE status = 'executed' AND date(executed_at) = date('now')`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("daily spend query: %w", err)
	}
	return total, nil
}

// Get retrieves a transaction by ID.
func (e *Engine) Get(ctx context.Context, txID string) (*Transaction, error) {
	return e.get(ctx, txID)
}

func (e *Engine) get(ctx context.Context, txID string) (*Transaction, error) {
	var tx Transaction
	var confirmedAt, executedAt sql.NullTime
	err := e.db.QueryRowContext(ctx,
		`SELECT id, service, category, phase, details, amount, status, confirmed_at, executed_at, created_at
		 FROM transactions WHERE id = ?`, txID).Scan(
		&tx.ID, &tx.Service, &tx.Category, &tx.Phase, &tx.Details,
		&tx.Amount, &tx.Status, &confirmedAt, &executedAt, &tx.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("transaction.get: %w", err)
	}
	if confirmedAt.Valid {
		tx.ConfirmedAt = &confirmedAt.Time
	}
	if executedAt.Valid {
		tx.ExecutedAt = &executedAt.Time
	}
	return &tx, nil
}

func (e *Engine) audit(ctx context.Context, txID, action, details string) {
	_, _ = e.db.ExecContext(ctx,
		`INSERT INTO transaction_audit (transaction_id, action, details) VALUES (?, ?, ?)`,
		txID, action, details)
}

// toolRegistry is the narrow interface used by registration functions.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// RegisterTransactionTools registers transaction management tools.
func RegisterTransactionTools(registry toolRegistry, engine *Engine) {
	registry.Register(
		"transaction.prepare",
		"Prepare a new transaction (search/compare phase, no money spent).",
		`{"type":"object","properties":{"service":{"type":"string"},"category":{"type":"string"},"details":{"type":"string"}},"required":["service","category","details"]}`,
		"tool.transaction.write",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Service  string `json:"service"`
				Category string `json:"category"`
				Details  string `json:"details"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			tx, err := engine.Prepare(ctx, args.Service, args.Category, args.Details)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(tx)
			return string(out), nil
		},
	)

	registry.Register(
		"transaction.confirm",
		"Confirm a prepared transaction (user has reviewed details).",
		`{"type":"object","properties":{"transaction_id":{"type":"string"}},"required":["transaction_id"]}`,
		"tool.transaction.write",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				TransactionID string `json:"transaction_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			tx, err := engine.Confirm(ctx, args.TransactionID)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(tx)
			return string(out), nil
		},
	)

	registry.Register(
		"transaction.execute",
		"Execute a confirmed transaction (actually performs the booking/order).",
		`{"type":"object","properties":{"transaction_id":{"type":"string"}},"required":["transaction_id"]}`,
		"tool.transaction.write",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				TransactionID string `json:"transaction_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			tx, err := engine.Execute(ctx, args.TransactionID)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(tx)
			return string(out), nil
		},
	)

	registry.Register(
		"transaction.cancel",
		"Cancel a transaction.",
		`{"type":"object","properties":{"transaction_id":{"type":"string"}},"required":["transaction_id"]}`,
		"tool.transaction.write",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				TransactionID string `json:"transaction_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			err := engine.Cancel(ctx, args.TransactionID)
			if err != nil {
				return "", err
			}
			return `{"status":"cancelled"}`, nil
		},
	)

	registry.Register(
		"transaction.status",
		"Get the current status of a transaction.",
		`{"type":"object","properties":{"transaction_id":{"type":"string"}},"required":["transaction_id"]}`,
		"tool.transaction.read",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				TransactionID string `json:"transaction_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			tx, err := engine.Get(ctx, args.TransactionID)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(tx)
			return string(out), nil
		},
	)
}
