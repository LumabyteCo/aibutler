package channel_test

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/i18n"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/stopphrase"
	"github.com/LumabyteCo/aibutler/testutil"
)

// failAgent is an AgentFactory whose runs fail through result.Error with an
// empty Output — the exact shape the agent loop produces on a provider 401
// (failWith sets Error, returns nil error).
type failAgent struct{ err string }

func (f *failAgent) Run(_ context.Context, _, _, _ string) (*agent.Result, error) {
	return &agent.Result{Status: agent.StateFailed, Error: f.err}, nil
}

// newTestRouter builds a Router wired for unit tests.
func newTestRouter(t *testing.T, fac channel.AgentFactory) (*RouterHarness, *fakeChannel) {
	t.Helper()
	database := testutil.TestDB(t)
	db := database.Conn()

	cfg := config.Default()
	cfg.Settings.Language = "en"

	bundle := i18n.New("en")
	stop := stopphrase.NewMatcher(bundle)
	typing := channel.NewTypingManager(5*time.Second, 30*time.Second)
	sessions := session.NewManager(db, cfg)

	ch := newFake("webchat")
	reg := channel.NewRegistry()
	reg.Register(ch)

	router := channel.NewRouter(channel.RouterConfig{
		Sessions: sessions,
		Stop:     stop,
		Typing:   typing,
		Channels: reg,
		Config:   cfg,
		I18n:     bundle,
		DB:       db,
		Agent:    fac,
	})

	env := channel.Envelope{
		ID:        "msg-1",
		Channel:   "webchat",
		AccountID: "user-1",
		Type:      channel.TypeText,
		Text:      "hello",
		Timestamp: time.Now(),
	}
	return &RouterHarness{t: t, r: router, env: env}, ch
}

type RouterHarness struct {
	t   *testing.T
	r   *channel.Router
	env channel.Envelope
}

func (h *RouterHarness) Dispatch() {
	h.t.Helper()
	h.r.HandleMessage(h.t.Context(), h.env)
}

// TestRouterSurfacesModelError guards the v0.2.1 fix (B4): a provider failure
// (401/410/etc.) reported via result.Error with empty Output must produce a
// VISIBLE, actionable message — not the silent empty assistant bubble users
// saw when their API key was rejected or the model was retired.
func TestRouterSurfacesModelError(t *testing.T) {
	cases := []struct {
		name    string
		rawErr  string
		wantIn  string
	}{
		{"401 unauthorized", "model error: openai: all 3 attempts failed: openai: status 401: unauthorized", "401"},
		{"410 retired model", "model error: openai: all 3 attempts failed: openai: status 410: {\"error\":{\"message\":\"glm-5.1 was retired at 2026-09-25\"}}", "retired"},
		{"404 unknown model", "model error: openai: status 404: model not found", "404"},
		{"generic failure", "model error: connection refused", "failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, ch := newTestRouter(t, &failAgent{err: tc.rawErr})
			h.Dispatch()

			sent := ch.Sent()
			if len(sent) == 0 {
				t.Fatal("router sent nothing — user sees an empty reply (bug B4 regression)")
			}
			if sent[0].Text == "" {
				t.Fatal("router sent an empty message — user sees an empty bubble (bug B4 regression)")
			}
			if !containsFold(sent[0].Text, tc.wantIn) {
				t.Errorf("message %q should mention %q (actionable hint)", sent[0].Text, tc.wantIn)
			}
		})
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}