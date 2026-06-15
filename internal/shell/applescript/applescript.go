// Package applescript provides a macOS AppleScript / JXA executor.
//
// AppleScript is the native scripting language for macOS apps that expose a
// scripting dictionary (Mail, Calendar, Music, Finder, Safari, and many
// third-party apps). JXA (JavaScript for Automation) is the JavaScript-based
// alternative that uses the same scripting bridges.
//
// This package mirrors the security model of internal/shell/powershell:
//
//   - Allowlist-by-verb — empty allowlist denies everything.
//   - Bounded execution timeout.
//   - Capped output size.
//   - Capability gating via tool.applescript.exec at the dispatcher layer.
//
// AppleScript first-word grammar covers most real scripts: tell, set, get,
// display, do, run, return, on, of, repeat, if. Allowlisting these keywords
// is coarse but matches the existing PowerShell executor's posture.
//
// Whole-script validation (hardened in v0.4.2): because osascript runs the
// ENTIRE submitted script, the allowlist check inspects the whole script,
// not just the first statement. Specifically it (a) normalizes all
// statement separators (CR, CRLF, U+2028/U+2029) to LF; (b) requires EVERY
// `tell application/process "X"` target in the script to be allowlisted,
// not merely the first; and (c) denies `do shell script` / `do script`
// (arbitrary shell / arbitrary AppleScript) unless an explicit
// "do shell script" allowlist entry opts in. This closes multi-statement
// and do-shell-script allowlist bypasses.
//
// For `tell`-style scripts the allowlist also supports a target-application
// pattern: an entry like `tell:Mail` grants ONLY scripts of the form
// `tell application "Mail" to ...`. Wildcards work — `tell:Music*` matches
// Music and Music Pro, `tell:*` matches any target. Bare `tell` keeps the
// original broad behaviour (any target). This makes finer-grained safe
// defaults (e.g. allow only `tell:System Events` and `tell:Notification
// Center` while denying `tell:Mail`) possible without schema churn.
package applescript

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/action"
)

const (
	maxOutputBytes = 4096
	defaultTimeout = 30 * time.Second

	// LanguageAppleScript runs the script as AppleScript (default).
	LanguageAppleScript = "AppleScript"
	// LanguageJavaScript runs the script as JXA via osascript -l JavaScript.
	LanguageJavaScript = "JavaScript"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Executor runs AppleScript / JXA commands with allowlist enforcement.
type Executor struct {
	allowlist []string
	timeout   time.Duration
	recorder  action.Recorder // optional — nil disables recording
}

// NewExecutor creates an AppleScript executor with the given first-word allowlist.
// An empty allowlist permits no commands.
func NewExecutor(allowlist []string) *Executor {
	return &Executor{
		allowlist: allowlist,
		timeout:   defaultTimeout,
	}
}

// SetTimeout overrides the default command timeout.
func (e *Executor) SetTimeout(d time.Duration) { e.timeout = d }

// SetRecorder attaches an action recorder. Pass nil to disable.
// Each Execute call produces one Action row when a recorder is set.
func (e *Executor) SetRecorder(r action.Recorder) { e.recorder = r }

// Execute runs an AppleScript (default) or JXA (when language=JavaScript)
// script after validating the first word against the allowlist.
//
// SECURITY: An empty allowlist means no commands are permitted. Mirrors the
// powershell executor's posture — do NOT wrap the allowlist check in
// `if len(allowlist) > 0`, which would silently allow ALL commands.
//
// When a recorder is attached (SetRecorder), every call emits one Action
// row with the script's target, allowlist outcome, duration, and a
// truncated result summary.
func (e *Executor) Execute(ctx context.Context, script, language string) (string, error) {
	start := time.Now()
	result, err := e.execute(ctx, script, language)
	e.recordAction(ctx, script, language, result, err, time.Since(start))
	return result, err
}

func (e *Executor) execute(ctx context.Context, script, language string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf(
			"shell.applescript: macOS-only — you're on %s. "+
				"Try shell.dbus for native Linux automation, shell.powershell on Windows, "+
				"or shell.script for cross-OS routing.", runtime.GOOS)
	}
	if !e.inAllowlist(script) {
		return "", fmt.Errorf("shell.applescript: command not in allowlist: %q", firstWord(script))
	}

	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	args := []string{}
	switch language {
	case "", LanguageAppleScript:
		// default — no -l flag
	case LanguageJavaScript:
		args = append(args, "-l", "JavaScript")
	default:
		return "", fmt.Errorf("shell.applescript: unsupported language %q (use %q or %q)", language, LanguageAppleScript, LanguageJavaScript)
	}
	args = append(args, "-e", script)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(execCtx, "osascript", args...) //nolint:gosec
	cmd.Stdout = &limitWriter{w: &stdout, remaining: maxOutputBytes}
	cmd.Stderr = &limitWriter{w: &stderr, remaining: maxOutputBytes}

	if err := cmd.Run(); err != nil {
		if execCtx.Err() != nil {
			return "", fmt.Errorf("shell.applescript: timeout after %s", e.timeout)
		}
		out := stdout.String()
		if errOut := stderr.String(); errOut != "" {
			out += "\n--- stderr ---\n" + errOut
		}
		return out, fmt.Errorf("shell.applescript: %w", err)
	}

	out := stdout.String()
	if errOut := stderr.String(); errOut != "" {
		out += "\n--- stderr ---\n" + errOut
	}
	return out, nil
}

// recordAction emits one Action row for the just-completed call when a
// recorder is attached.
func (e *Executor) recordAction(ctx context.Context, script, language, result string, err error, dur time.Duration) {
	if e.recorder == nil {
		return
	}
	target := extractTellTarget(script)
	verb := firstWord(script)
	summary := verb
	if target != "" {
		summary += ":" + target
	}
	if language == LanguageJavaScript {
		summary = "[jxa] " + summary
	}
	payloadJSON, _ := json.Marshal(struct {
		Script   string `json:"script"`
		Language string `json:"language,omitempty"`
	}{Script: script, Language: language})

	status := "success"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
		if strings.Contains(errStr, "not in allowlist") {
			status = "denied"
		}
	}

	resSnippet := result
	if len(resSnippet) > 200 {
		resSnippet = resSnippet[:200]
	}

	_ = e.recorder.Record(ctx, action.Action{
		Type:           "applescript.exec",
		Target:         target,
		PayloadSummary: summary,
		PayloadFull:    string(payloadJSON),
		DurationMS:     dur.Milliseconds(),
		Status:         status,
		ResultSummary:  resSnippet,
		Error:          errStr,
	})
}

func (e *Executor) inAllowlist(script string) bool {
	// Normalize statement separators FIRST. AppleScript treats CR (\r),
	// CRLF, and the Unicode line/paragraph separators as statement
	// terminators just like LF; without normalizing, a `\r`-separated
	// second statement hides from any newline-based reasoning below.
	script = normalizeSeparators(script)

	cmd := firstWord(script)
	if cmd == "" {
		return false
	}

	// `do shell script` / `do script` run an arbitrary shell command or
	// arbitrary AppleScript text — they escape the app-scoped allowlist
	// model entirely. They may appear ANYWHERE in the script (inside a
	// tell block, after an allowed leading statement, etc.), so scan the
	// whole script. Permit them ONLY if an explicit allowlist entry opts
	// in (e.g. "do shell script"). Otherwise deny outright.
	isDoShell := scriptContainsDoShell(script)
	if isDoShell && !e.allowsDoShell() {
		return false
	}

	// For tell-style scripts, every tell target in the script must be
	// allowlisted — not just the first. osascript runs the entire script,
	// so an allowed leading `tell` must not smuggle a second tell to a
	// denied target. Scripts with zero parseable tell targets fall
	// through to the first-word matcher (covers un-quoted targets, which
	// only a bare-verb entry can permit).
	if strings.EqualFold(cmd, "tell") {
		targets := extractAllTellTargets(script)
		if len(targets) > 0 {
			for _, tgt := range targets {
				if !e.targetAllowed(cmd, tgt) {
					return false
				}
			}
			return true
		}
		// No parseable target — only a bare-verb "tell" entry can allow it.
		return e.targetAllowed(cmd, "")
	}

	// A top-level do-shell script reaches here with cmd=="do". It already
	// cleared the explicit opt-in gate above, so the opt-in entry IS the
	// grant — the verb matcher below would miss it (firstWord is "do",
	// the entry is "do shell script").
	if isDoShell {
		return true
	}

	// Non-tell first word. The allowlist is keyed by verb; match it.
	for _, allowed := range e.allowlist {
		if matchAllowlistEntry(allowed, cmd, "") {
			return true
		}
	}
	return false
}

// targetAllowed reports whether some allowlist entry permits the given
// verb + target.
func (e *Executor) targetAllowed(cmd, target string) bool {
	for _, allowed := range e.allowlist {
		if matchAllowlistEntry(allowed, cmd, target) {
			return true
		}
	}
	return false
}

// allowsDoShell reports whether the allowlist explicitly opts in to
// `do shell script` / `do script` via a dedicated entry. The entry is
// matched case-insensitively against "do shell script" or "do script".
func (e *Executor) allowsDoShell() bool {
	for _, allowed := range e.allowlist {
		a := strings.TrimSpace(strings.ToLower(allowed))
		if a == "do shell script" || a == "do script" {
			return true
		}
	}
	return false
}

// normalizeSeparators converts every AppleScript statement-separator
// variant to a plain LF so downstream parsing sees a single line-ending
// convention. Covers CRLF, bare CR, and the Unicode line (U+2028) and
// paragraph (U+2029) separators.
func normalizeSeparators(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\u2028", "\n") // Unicode line separator
	s = strings.ReplaceAll(s, "\u2029", "\n") // Unicode paragraph separator
	return s
}

// doShellRegex matches a `do shell script` or `do script` command
// anywhere in the (separator-normalized) script. The leading boundary
// is start-of-line or whitespace so it won't false-match an identifier
// like `mydo shell script`.
var doShellRegex = regexp.MustCompile(`(?im)(^|\s)do\s+(shell\s+)?script\b`)

// scriptContainsDoShell reports whether the script invokes do[ shell]
// script anywhere.
func scriptContainsDoShell(script string) bool {
	return doShellRegex.MatchString(script)
}

// matchAllowlistEntry checks whether `entry` permits the script's first
// word `cmd` and (for tell-style scripts) target `target`.
//
// Entry syntax:
//
//   - "tell"             — bare verb; permits any target (or no target)
//   - "tell:Mail"        — exact target match (case-insensitive)
//   - "tell:Music*"      — prefix wildcard on target
//   - "tell:*"           — any target (equivalent to bare "tell")
//   - "display"          — non-tell verbs ignore the target component
func matchAllowlistEntry(entry, cmd, target string) bool {
	parts := strings.SplitN(entry, ":", 2)
	if !strings.EqualFold(parts[0], cmd) {
		return false
	}
	if len(parts) == 1 {
		// Bare verb — matches regardless of target.
		return true
	}
	return matchTargetPattern(parts[1], target)
}

// matchTargetPattern compares an allowlist target pattern against the
// extracted script target. Empty pattern denies, "*" allows anything,
// "Foo*" allows prefix matches, exact strings match case-insensitively.
func matchTargetPattern(pattern, target string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(strings.ToLower(target), strings.ToLower(prefix))
	}
	return strings.EqualFold(pattern, target)
}

// tellTargetRegex captures the application or process name in
//
//	tell application "<NAME>" ...
//	tell app "<NAME>" ...
//	tell process "<NAME>" ...
//
// AppleScript is case-insensitive on these keywords. Quoted target name is
// the most common form; un-quoted identifiers (rare) are not parsed —
// callers using those should use a bare-verb allowlist entry.
//
// The (?m) flag makes `^` match at every line start, so FindAll picks up
// a `tell` at the head of ANY statement, not just the first — closing
// the multi-tell bypass where a denied second target rides on an allowed
// first one.
var tellTargetRegex = regexp.MustCompile(`(?im)^\s*tell\s+(?:application|app|process)\s+"([^"]+)"`)

// extractTellTarget returns the FIRST target application/process name
// from a tell-style script, or "" if the script doesn't match the
// expected `tell application "Name"` shape. Used by the audit recorder
// for a human-readable summary; allowlist enforcement uses
// extractAllTellTargets so every target is checked.
func extractTellTarget(script string) string {
	m := tellTargetRegex.FindStringSubmatch(normalizeSeparators(script))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// extractAllTellTargets returns every tell target in the script, in
// order. The caller passes an already-separator-normalized script.
func extractAllTellTargets(script string) []string {
	ms := tellTargetRegex.FindAllStringSubmatch(script, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if len(m) >= 2 {
			out = append(out, m[1])
		}
	}
	return out
}

// firstWord extracts the first word (keyword) from an AppleScript / JXA script.
// AppleScript scripts typically begin with: tell, set, get, display, do, run,
// on, of, repeat, if, return.
func firstWord(script string) string {
	script = strings.TrimSpace(script)
	for i, r := range script {
		if r == ' ' || r == '\t' || r == '\n' || r == '(' {
			return script[:i]
		}
	}
	return script
}

// limitWriter wraps a bytes.Buffer with a size limit.
type limitWriter struct {
	w         *bytes.Buffer
	remaining int
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		return len(p), nil
	}
	if len(p) > lw.remaining {
		p = p[:lw.remaining]
	}
	n, err := lw.w.Write(p)
	lw.remaining -= n
	return n, err
}

// DefaultAllowlist returns a small, curated set of AppleScript first-words
// that cover the common cases of "talk to a Mac app, display a notification,
// or speak some text" — useful as an opt-in starter for users who don't want
// to assemble an allowlist from scratch.
//
// These are intentionally permissive enough to be useful: `tell` is the
// universal entry point for app interaction, `display` covers dialogs and
// notifications, `say` covers TTS, and `get` covers read operations. A
// future target-application allowlist could narrow `tell` to specific apps
// (e.g. tell:Music, tell:Notification Center) — until then, callers who
// opt into defaults accept the broader surface.
//
// Defaults are NOT applied automatically. Callers must merge them into the
// allowlist explicitly (typically gated by a config flag) so secure-by-default
// (empty allowlist denies everything) still holds for fresh installs.
func DefaultAllowlist() []string {
	return []string{
		"tell",    // tell application "X" to ... — primary verb
		"display", // display notification / display dialog — UI output
		"say",     // say "..." — text-to-speech output
		"get",     // get count of messages, get name of, ... — read
	}
}

// RegisterAppleScriptTool registers the shell.applescript tool.
func RegisterAppleScriptTool(registry toolRegistry, executor *Executor) {
	registry.Register(
		"shell.applescript",
		"Execute an AppleScript or JXA script on macOS via osascript. Only allowed first-words (per allowlist) may run. Use language=JavaScript for JXA.",
		`{"type":"object","properties":{"script":{"type":"string","description":"AppleScript or JXA source (e.g. 'tell application \"Mail\" to get count of messages in inbox')"},"language":{"type":"string","enum":["AppleScript","JavaScript"],"description":"Script language. Defaults to AppleScript.","default":"AppleScript"}},"required":["script"]}`,
		"tool.applescript.exec",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Script   string `json:"script"`
				Language string `json:"language"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("shell.applescript: invalid input: %w", err)
			}
			if args.Script == "" {
				return "", fmt.Errorf("shell.applescript: script is required")
			}
			return executor.Execute(ctx, args.Script, args.Language)
		},
	)
}
