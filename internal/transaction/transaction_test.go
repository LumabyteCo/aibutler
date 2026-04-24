package transaction_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/transaction"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newTestEngine(t *testing.T, limits transaction.SpendingLimits) *transaction.Engine {
	t.Helper()
	db := testutil.TestDB(t)
	return transaction.New(db.Conn(), limits)
}

func TestPrepare(t *testing.T) {
	engine := newTestEngine(t, transaction.SpendingLimits{})
	ctx := context.Background()

	tx, err := engine.Prepare(ctx, "opentable", "restaurant", `{"restaurant":"Sushi Place","amount":85.50}`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if tx.Service != "opentable" {
		t.Errorf("service = %q, want opentable", tx.Service)
	}
	if tx.Category != "restaurant" {
		t.Errorf("category = %q, want restaurant", tx.Category)
	}
	if tx.Status != "pending" {
		t.Errorf("status = %q, want pending", tx.Status)
	}
	if tx.Amount != 85.50 {
		t.Errorf("amount = %f, want 85.50", tx.Amount)
	}
}

func TestConfirm(t *testing.T) {
	engine := newTestEngine(t, transaction.SpendingLimits{})
	ctx := context.Background()

	tx, _ := engine.Prepare(ctx, "uber", "rideshare", `{"amount":25.00}`)
	confirmed, err := engine.Confirm(ctx, tx.ID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.Status != "confirmed" {
		t.Errorf("status = %q, want confirmed", confirmed.Status)
	}
	if confirmed.ConfirmedAt == nil {
		t.Error("expected confirmed_at to be set")
	}
}

func TestExecute(t *testing.T) {
	engine := newTestEngine(t, transaction.SpendingLimits{})
	ctx := context.Background()

	tx, _ := engine.Prepare(ctx, "doordash", "delivery", `{"amount":42.00}`)
	engine.Confirm(ctx, tx.ID)

	executed, err := engine.Execute(ctx, tx.ID)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if executed.Status != "executed" {
		t.Errorf("status = %q, want executed", executed.Status)
	}
	if executed.ExecutedAt == nil {
		t.Error("expected executed_at to be set")
	}
}

func TestCancel(t *testing.T) {
	engine := newTestEngine(t, transaction.SpendingLimits{})
	ctx := context.Background()

	tx, _ := engine.Prepare(ctx, "uber", "rideshare", `{"amount":15.00}`)
	if err := engine.Cancel(ctx, tx.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	got, err := engine.Get(ctx, tx.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
}

func TestExecuteWithoutConfirmReturnsError(t *testing.T) {
	engine := newTestEngine(t, transaction.SpendingLimits{})
	ctx := context.Background()

	tx, _ := engine.Prepare(ctx, "opentable", "restaurant", `{"amount":50.00}`)

	_, err := engine.Execute(ctx, tx.ID)
	if !errors.Is(err, transaction.ErrConfirmationRequired) {
		t.Errorf("expected ErrConfirmationRequired, got: %v", err)
	}
}

func TestPrepareRejectsNegativeAmount(t *testing.T) {
	engine := newTestEngine(t, transaction.SpendingLimits{})
	ctx := context.Background()

	_, err := engine.Prepare(ctx, "uber", "rideshare", `{"amount":-50.00}`)
	if err == nil {
		t.Fatal("expected error for negative amount, got nil")
	}
}

func TestCheckLimitsRejectsNegativeAmount(t *testing.T) {
	engine := newTestEngine(t, transaction.SpendingLimits{PerTransaction: 100.00, DailyTotal: 500.00})
	ctx := context.Background()

	err := engine.CheckLimits(ctx, -10.00)
	if !errors.Is(err, transaction.ErrLimitExceeded) {
		t.Errorf("expected ErrLimitExceeded for negative amount, got: %v", err)
	}
}

func TestSpendingLimitPerTransaction(t *testing.T) {
	engine := newTestEngine(t, transaction.SpendingLimits{PerTransaction: 100.00})
	ctx := context.Background()

	tx, _ := engine.Prepare(ctx, "uber", "rideshare", `{"amount":150.00}`)
	engine.Confirm(ctx, tx.ID)

	_, err := engine.Execute(ctx, tx.ID)
	if !errors.Is(err, transaction.ErrLimitExceeded) {
		t.Errorf("expected ErrLimitExceeded, got: %v", err)
	}
}
