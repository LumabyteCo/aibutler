package prompt_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/testutil"
)

// TestComposerCurrentTurnPINSurvives guards the B10 fix's critical invariant:
// the CURRENT turn's user message must reach the model intact (the model
// needs the PIN to build the tool call), while PREVIOUS turns' PINs are
// redacted from history.
//
// This replicates the production flow exactly: router stores the message,
// then the Factory composes with the same sessionID + task.
func TestComposerCurrentTurnPINSurvives(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	cfg := config.Default()
	cfg.Settings.PersonaName = "Butler"
	mgr := session.NewManager(conn, cfg)

	sessID := "sess-b10-composer"
	// Seed the session (FK target).
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id, scope) VALUES (?, 'webchat', 'u1', 'default')`,
		sessID); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// 1. A PREVIOUS turn that contained a PIN — must be redacted.
	if err := mgr.AddMessage(ctx, sessID, agent.Message{
		Role: "user", Content: "Unlock the door. Safety PIN: 1111",
	}); err != nil {
		t.Fatalf("add old msg: %v", err)
	}
	if err := mgr.AddMessage(ctx, sessID, agent.Message{
		Role: "assistant", Content: "Older turn done.",
	}); err != nil {
		t.Fatalf("add old reply: %v", err)
	}

	// 2. Router stores the CURRENT message (production order).
	current := "Unlock the Front Door. I confirm. Safety PIN: 2468"
	if err := mgr.AddMessage(ctx, sessID, agent.Message{
		Role: "user", Content: current,
	}); err != nil {
		t.Fatalf("add current: %v", err)
	}

	// 3. Compose — what the model receives.
	c := prompt.NewComposer(cfg, mgr, nil, nil)
	p, err := c.Compose(ctx, sessID, current, "webchat")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	var joined string
	for _, m := range p.History {
		joined += m.Content + "\n"
	}

	// Current turn's PIN must SURVIVE (it's this turn's own message).
	if !contains(joined, "2468") {
		t.Errorf("current-turn PIN was redacted — the model can't build the tool call. History:\n%s", joined)
	}
	// Previous turn's PIN must be REDACTED.
	if contains(joined, "1111") {
		t.Errorf("previous-turn PIN leaked into history:\n%s", joined)
	}
	if !contains(joined, "[REDACTED]") {
		t.Errorf("expected [REDACTED] for the old PIN in history:\n%s", joined)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}