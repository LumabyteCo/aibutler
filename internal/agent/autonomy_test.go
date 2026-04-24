package agent

import (
	"testing"
	"time"
)

func TestAutonomyL1AlwaysAutoApproves(t *testing.T) {
	ac := AutonomyConfig{Level: AutonomyL1}
	if !ac.ShouldAutoApprove("shell.exec") {
		t.Error("L1 should auto-approve all tools")
	}
	if !ac.ShouldAutoApprove("agent.delegate") {
		t.Error("L1 should auto-approve all tools")
	}
}

func TestAutonomyL2AskList(t *testing.T) {
	ac := AutonomyConfig{
		Level:      AutonomyL2,
		AskActions: []string{"shell.exec", "file.edit"},
	}

	// Shell.exec is in ask list — should NOT auto-approve.
	if ac.ShouldAutoApprove("shell.exec") {
		t.Error("shell.exec should require confirmation at L2")
	}
	// Web.search is NOT in ask list — should auto-approve.
	if !ac.ShouldAutoApprove("web.search") {
		t.Error("web.search should be auto-approved at L2")
	}
}

func TestAutonomyL2AutoList(t *testing.T) {
	ac := AutonomyConfig{
		Level:       AutonomyL2,
		AutoActions: []string{"web.search", "memory.search"},
	}

	// Web.search is in auto list — should auto-approve.
	if !ac.ShouldAutoApprove("web.search") {
		t.Error("web.search should be auto-approved at L2")
	}
	// Shell.exec is NOT in auto list — should NOT auto-approve.
	if ac.ShouldAutoApprove("shell.exec") {
		t.Error("shell.exec should not be auto-approved when not in auto list")
	}
}

func TestAutonomyL2NoLists(t *testing.T) {
	ac := AutonomyConfig{Level: AutonomyL2}
	// No lists configured — auto-approve everything.
	if !ac.ShouldAutoApprove("shell.exec") {
		t.Error("should auto-approve when no lists configured")
	}
}

func TestAutonomyL3TimeBounded(t *testing.T) {
	// L3 within time bound should auto-approve non-safety actions.
	ac := AutonomyConfig{
		Level: AutonomyL3,
		L3: L3Config{
			TimeBound:     30 * time.Minute,
			SafetyActions: []string{"finance.transfer"},
			StartedAt:     time.Now(),
		},
	}

	if !ac.ShouldAutoApprove("shell.exec") {
		t.Error("L3 should auto-approve shell.exec within time bound")
	}
	if !ac.ShouldAutoApprove("web.search") {
		t.Error("L3 should auto-approve web.search within time bound")
	}

	// Expired time bound: should NOT auto-approve.
	ac.L3.StartedAt = time.Now().Add(-1 * time.Hour)
	if ac.ShouldAutoApprove("shell.exec") {
		t.Error("L3 should NOT auto-approve after time bound expired")
	}
}

func TestAutonomyL3FinancialAlwaysConfirms(t *testing.T) {
	ac := AutonomyConfig{
		Level: AutonomyL3,
		L3: L3Config{
			TimeBound:     30 * time.Minute,
			SafetyActions: []string{"finance.transfer", "finance.payment", "account.delete"},
			StartedAt:     time.Now(),
		},
	}

	// Safety-critical actions should ALWAYS require confirmation at L3.
	if ac.ShouldAutoApprove("finance.transfer") {
		t.Error("finance.transfer should always require confirmation at L3")
	}
	if ac.ShouldAutoApprove("finance.payment") {
		t.Error("finance.payment should always require confirmation at L3")
	}
	if ac.ShouldAutoApprove("account.delete") {
		t.Error("account.delete should always require confirmation at L3")
	}

	// Non-safety actions should be auto-approved.
	if !ac.ShouldAutoApprove("memory.search") {
		t.Error("memory.search should be auto-approved at L3")
	}
}

func TestAutonomyL3RequiresCapability(t *testing.T) {
	// Verify that L3 defaults have safety actions configured.
	cfg := DefaultL3Config()
	if len(cfg.SafetyActions) == 0 {
		t.Error("default L3 config should have safety actions")
	}
	if cfg.TimeBound != 30*time.Minute {
		t.Errorf("default L3 time bound = %v, want 30m", cfg.TimeBound)
	}

	// Verify the default safety actions include financial.
	found := false
	for _, a := range cfg.SafetyActions {
		if a == "finance.transfer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("default L3 safety actions should include finance.transfer")
	}
}
