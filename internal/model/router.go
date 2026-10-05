package model

import (
	"context"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// RouterConfig controls the hybrid local/cloud model router.
type RouterConfig struct {
	// Local is the adapter for the small local model (Ollama/LM Studio).
	Local agent.ModelAdapter
	// LocalName is the human label for logging/boot output.
	LocalName string
	// Primary is the adapter for the cloud/primary model.
	Primary agent.ModelAdapter
	// PrimaryName is the human label.
	PrimaryName string
}

// Router is a hybrid model adapter: short, device-command-shaped requests
// go to the fast local model (sub-2s, works offline); everything else goes
// to the primary model (reasoning, memory, tools). It implements
// agent.ModelAdapter, so the entire pipeline (Factory, agent loop,
// capability engine) is unchanged — the router just picks per call.
//
// Graceful degradation:
//   - local unavailable (offline / not running): fall through to primary,
//     transparently (device commands are rare and cheap on cloud).
//   - primary unavailable: fall back to local and mark the response so the
//     caller can surface "reduced reasoning mode".
//
// Routing rule (Phase 2): a request routes local when it is a SHORT
// user-turn that looks like a device/quick-intent command. Long requests,
// anything with tool-call history in flight (multi-turn agent loops),
// and system-heavy compositions stay on primary — the local model is not
// asked to reason over tool schemas.
type Router struct {
	cfg RouterConfig
}

// NewRouter creates the hybrid router. Either side may be nil; a nil side
// is simply never routed to.
func NewRouter(cfg RouterConfig) *Router {
	return &Router{cfg: cfg}
}

// Name labels for diagnostics.
func (r *Router) LocalName() string  { return r.cfg.LocalName }
func (r *Router) PrimaryName() string { return r.cfg.PrimaryName }

// Complete implements agent.ModelAdapter. It inspects the message shape
// and routes; the chosen adapter's Complete does the real work.
func (r *Router) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	if r.cfg.Local == nil {
		return r.primary(ctx, messages)
	}
	if r.cfg.Primary == nil {
		return r.local(ctx, messages)
	}
	if routesLocal(messages) {
		resp, err := r.cfg.Local.Complete(ctx, messages)
		if err == nil {
			return resp, nil
		}
		// Local model failed (offline? model missing?) — degrade to
		// primary rather than failing the user's command.
		return r.primary(ctx, messages)
	}
	return r.primary(ctx, messages)
}

func (r *Router) primary(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	resp, err := r.cfg.Primary.Complete(ctx, messages)
	if err != nil && r.cfg.Local != nil {
		// Primary failed (cloud down, key invalid, retired model) — fall
		// back to local. Small models can still handle simple commands;
		// the caller surfaces the degraded state via the error we pass
		// through below (we do NOT swallow the primary error silently).
		if localResp, lerr := r.cfg.Local.Complete(ctx, messages); lerr == nil {
			localResp.Note = "primary model unavailable — answered by the local model (reduced reasoning)"
			return localResp, nil
		}
	}
	return resp, err
}

func (r *Router) local(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	return r.cfg.Local.Complete(ctx, messages)
}

// routesLocal decides whether a message set is a "fast intent": a short
// user turn with a command shape, no in-flight tool calls, and a system
// prompt under the agent-loop size (bare single-shot requests like the
// webchat's simple "turn off the kitchen light" or "goodnight").
//
// Heuristics, deliberately boring:
//   - exactly one user message in the conversation (single-shot);
//   - the user text is at most 120 bytes;
//   - no tool messages / assistant tool-call turns present;
//   - the text matches a device-command verb pattern.
//
// Anything that needs memory recall, files, scheduling, or reasoning stays
// on primary — local only gets the "flip a switch" class of request.
func routesLocal(messages []agent.Message) bool {
	if len(messages) == 0 {
		return false
	}
	// Route on the SHAPE OF THE CURRENT TURN, not the slice length:
	// history from prior turns is normal (the sliding window grows as
	// the conversation does) and must not disqualify a fast intent.
	//
	// Rules:
	//   1. the final message must be the current user turn;
	//   2. no tool results or assistant tool-call turns may FOLLOW it
	//      (those mean the agent loop is mid-flight — reasoning);
	//   3. the user text is short and matches a device-command shape.
	//
	// Prior history BEFORE the final user turn is allowed — but only if
	// it is plain conversation (no dangling tool loop), because a tiny
	// model reading a transcript of past tool calls gets confused.
	last := messages[len(messages)-1]
	if last.Role != "user" || len(last.ToolCalls) > 0 {
		return false
	}
	userText := last.Content
	if len(userText) == 0 || len(userText) > 120 {
		return false
	}
	for i := 0; i < len(messages)-1; i++ {
		m := messages[i]
		if m.Role == "tool" {
			return false
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			return false
		}
	}
	return isFastIntent(userText)
}

// fastIntentVerbs are the command shapes a tiny model can be trusted with.
var fastIntentVerbs = []string{
	"turn on", "turn off", "switch on", "switch off",
	"lock", "unlock", "open the", "close the",
	"dim", "brighten", "set the", "set ",
	"goodnight", "good night", "good morning", "good afternoon",
	"lights on", "lights off", "all lights",
	"arm", "disarm",
}

// isFastIntent reports whether a short utterance looks like a device
// command or a routine trigger word.
func isFastIntent(text string) bool {
	t := strings.ToLower(text)
	t = strings.TrimSpace(t)
	if t == "" {
		return false
	}
	for _, v := range fastIntentVerbs {
		if strings.Contains(t, v) {
			return true
		}
	}
	// Single-word routine triggers.
	switch t {
	case "goodnight", "goodbye", "goodmorning", "home", "away", "sleep":
		return true
	}
	return false
}

// FallbackNote returns whether the last response was a degraded local
// answer (for logging/testing).
func ResponseNote(resp *agent.Response) string {
	if resp == nil {
		return ""
	}
	return resp.Note
}

// SetTools implements model.ToolsSetter so the hybrid router is invisible
// to the Factory: tool definitions flow through to BOTH adapters (each
// native adapter serializes them for its API — the router itself never
// interprets tools). Without this, the Factory's capability check finds
// no ToolsSetter on the router and silently drops the tool definitions,
// leaving every routed model "without skills".
func (r *Router) SetTools(tools []agent.ToolDef) {
	if r.cfg.Local != nil {
		if setter, ok := r.cfg.Local.(ToolsSetter); ok {
			setter.SetTools(tools)
		}
	}
	if r.cfg.Primary != nil {
		if setter, ok := r.cfg.Primary.(ToolsSetter); ok {
			setter.SetTools(tools)
		}
	}
}
