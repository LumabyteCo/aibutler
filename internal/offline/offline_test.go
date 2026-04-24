package offline_test

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/offline"
)

func TestGuardDisabledAllowsAll(t *testing.T) {
	g := offline.NewGuard(false)
	if err := g.CheckURL("https://api.openai.com/v1/chat"); err != nil {
		t.Errorf("disabled guard blocked: %v", err)
	}
}

func TestGuardEnabledBlocksRemote(t *testing.T) {
	g := offline.NewGuard(true)
	if err := g.CheckURL("https://api.openai.com/v1/chat"); err == nil {
		t.Error("expected block for remote URL in offline mode")
	}
}

func TestGuardEnabledAllowsLocalhost(t *testing.T) {
	g := offline.NewGuard(true)

	urls := []string{
		"http://localhost:3377/api",
		"http://127.0.0.1:8080/health",
		"http://[::1]:3000/test",
	}
	for _, u := range urls {
		if err := g.CheckURL(u); err != nil {
			t.Errorf("offline guard blocked localhost URL %q: %v", u, err)
		}
	}
}

func TestGuardIsEnabled(t *testing.T) {
	g := offline.NewGuard(true)
	if !g.IsEnabled() {
		t.Error("expected enabled")
	}

	g2 := offline.NewGuard(false)
	if g2.IsEnabled() {
		t.Error("expected disabled")
	}
}
