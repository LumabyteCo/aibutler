// Package skillsynth turns successfully completed multi-step work into
// reusable, human-reviewable skill proposals.
//
// The pipeline is proposal-first end to end: a synthesized skill is written
// to a staging directory the skill loader never reads, recorded as a pending
// proposal, and becomes active only through an explicit human approval —
// there is no configuration under which a self-authored skill activates
// itself. Approval moves the file into the live skills directory and writes
// a ledger entry (mirrored to the audit trail) carrying the content hash and
// the approving human.
//
// Capability discipline: a synthesized skill's declared capabilities are
// intersected with the capability set of the run it was learned from — a
// skill can describe how to use access the agent already had, never request
// more. Skills are prompt guidance only; every tool call still passes the
// dispatcher's capability checks regardless of what any skill says.
//
// Synthesis triggers only on runs with a grounded success signal (a
// verification tool that ran and did not error) — minting procedures from
// self-assessed success would codify lucky guesses; the measurement harness
// exists precisely because self-assessment is unreliable.
package skillsynth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/changelog"
	"github.com/LumabyteCo/aibutler/internal/prompt"
)

// Config tunes the synthesis gate.
type Config struct {
	Enabled      bool
	MinToolCalls int      // runs with fewer tool calls are routine, not procedures (default 5)
	VerifyTools  []string // tools whose non-error output counts as grounded success
	SkillsDir    string   // live skills directory; proposals stage under <dir>/proposed
	MaxPending   int      // cap on staged proposals (default 25)
}

// DefaultVerifyTools are the built-in grounded-success signals.
var DefaultVerifyTools = []string{"code.test", "code.lint", "code.run"}

// Synthesizer drafts skill proposals from completed runs.
type Synthesizer struct {
	cfg    Config
	model  agent.ModelAdapter
	db     *sql.DB
	ledger *changelog.Ledger
	// allowedCaps is the capability-resource snapshot of the context skills
	// are synthesized in; declared capabilities are clamped to it.
	allowedCaps map[string]bool
	// capOf maps a tool name to its capability resource, so the artifact
	// declares only what the run's tools actually required.
	capOf func(toolName string) string
}

// SetToolCapabilityResolver wires the tool→capability lookup used to derive
// a skill's declared capabilities from the tools its source run used.
func (s *Synthesizer) SetToolCapabilityResolver(fn func(toolName string) string) {
	s.capOf = fn
}

// New creates a synthesizer. model may be nil (synthesis disabled until a
// provider resolves). allowedCaps lists the capability resources of the
// running context.
func New(cfg Config, model agent.ModelAdapter, db *sql.DB, ledger *changelog.Ledger, allowedCaps []string) *Synthesizer {
	if cfg.MinToolCalls <= 0 {
		cfg.MinToolCalls = 5
	}
	if len(cfg.VerifyTools) == 0 {
		cfg.VerifyTools = DefaultVerifyTools
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = 25
	}
	capsSet := make(map[string]bool, len(allowedCaps))
	for _, c := range allowedCaps {
		capsSet[c] = true
	}
	return &Synthesizer{cfg: cfg, model: model, db: db, ledger: ledger, allowedCaps: capsSet}
}

// RunInfo is what synthesis needs to know about a completed run.
type RunInfo struct {
	SessionID   string
	UserMessage string
	Status      string // agent run state; only "completed" runs synthesize
	ToolOutputs []agent.ToolOutput
}

// ShouldSynthesize applies the gate: enabled, run actually completed,
// enough tool calls, and a grounded success signal. Exported so the gate
// itself is testable.
func (s *Synthesizer) ShouldSynthesize(info RunInfo) bool {
	if !s.cfg.Enabled || s.model == nil {
		return false
	}
	if info.Status != "completed" {
		// The post-run hook fires for failed and cancelled runs too — a
		// verify tool that passed early in a run that later fell apart is
		// not a success worth learning from.
		return false
	}
	if len(info.ToolOutputs) < s.cfg.MinToolCalls {
		return false
	}
	return s.hasGroundedSuccess(info.ToolOutputs)
}

// hasGroundedSuccess reports whether the LAST verification-tool invocation
// actually ran and did not error. The last one decides: a passing check
// followed by more mutations and a failing check is not verified work.
// Outputs prefixed "error:" are loop-reported tool failures; outputs
// prefixed "blocked" are policy blocks or repeat-breaker advisories — the
// tool never executed, so nothing was verified.
func (s *Synthesizer) hasGroundedSuccess(outputs []agent.ToolOutput) bool {
	verdict := false
	seen := false
	for _, out := range outputs {
		for _, vt := range s.cfg.VerifyTools {
			if out.ToolName != vt {
				continue
			}
			seen = true
			verdict = !strings.HasPrefix(out.Output, "error:") &&
				!strings.HasPrefix(out.Output, "blocked")
		}
	}
	return seen && verdict
}

// Synthesize drafts a skill from the run and stages it as a pending
// proposal. It never touches the live skills directory.
func (s *Synthesizer) Synthesize(ctx context.Context, info RunInfo) (int64, error) {
	if !s.ShouldSynthesize(info) {
		return 0, fmt.Errorf("skillsynth: run does not meet the synthesis gate")
	}

	var pending int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_proposals WHERE kind = 'skill' AND status = 'pending'`).Scan(&pending); err != nil {
		return 0, fmt.Errorf("skillsynth: %w", err)
	}
	if pending >= s.cfg.MaxPending {
		return 0, fmt.Errorf("skillsynth: %d proposals already pending — review them before synthesizing more", pending)
	}

	draft, err := s.draft(ctx, info)
	if err != nil {
		return 0, err
	}

	stagingDir := filepath.Join(s.cfg.SkillsDir, "proposed")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return 0, fmt.Errorf("skillsynth: staging dir: %w", err)
	}
	path := filepath.Join(stagingDir, draft.name+".md")
	// One staged file per name: overwriting would orphan an earlier pending
	// proposal (its hash would no longer match its file).
	if _, err := os.Stat(path); err == nil {
		return 0, fmt.Errorf("skillsynth: a proposal named %q is already staged — review it first", draft.name)
	}
	if err := os.WriteFile(path, []byte(draft.content), 0o600); err != nil {
		return 0, fmt.Errorf("skillsynth: stage: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{
		"path": path,
		"name": draft.name,
		"hash": draft.hash,
	})
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_proposals (kind, title, payload) VALUES ('skill', ?, ?)`,
		"New skill: "+draft.name, string(payload))
	if err != nil {
		return 0, fmt.Errorf("skillsynth: record proposal: %w", err)
	}
	return res.LastInsertId()
}

type draftedSkill struct {
	name    string
	content string
	hash    string
}

// draft asks the model for a procedure writeup, then builds the artifact
// deterministically: frontmatter fields are constructed here (not parsed
// from model output), so the capability clamp and status can't be prompted
// away.
func (s *Synthesizer) draft(ctx context.Context, info RunInfo) (draftedSkill, error) {
	var steps []string
	toolsUsed := map[string]bool{}
	for i, out := range info.ToolOutputs {
		o := out.Output
		if len(o) > 200 {
			o = o[:200] + "…"
		}
		steps = append(steps, fmt.Sprintf("%d. %s → %s", i+1, out.ToolName, o))
		toolsUsed[out.ToolName] = true
	}

	prompt := fmt.Sprintf(`A task was just completed successfully. Write a short reusable procedure so the same class of task goes faster next time.

Task: %s

Steps that worked:
%s

Reply with EXACTLY this structure (no extra sections):
NAME: <kebab-case-name, max 4 words>
SUMMARY: <5 words>
TRIGGERS: <3-6 comma-separated keywords a similar request would contain>
PROCEDURE:
<numbered steps in imperative voice, generalized from this run — strip run-specific values, keep the method>`,
		info.UserMessage, strings.Join(steps, "\n"))

	resp, err := s.model.Complete(ctx, []agent.Message{{Role: "user", Content: prompt}})
	if err != nil {
		return draftedSkill{}, fmt.Errorf("skillsynth: draft: %w", err)
	}

	name := extractField(resp.Content, "NAME")
	summary := extractField(resp.Content, "SUMMARY")
	triggers := extractField(resp.Content, "TRIGGERS")
	procedure := extractSection(resp.Content, "PROCEDURE:")
	name = sanitizeName(name)
	if name == "" || procedure == "" {
		return draftedSkill{}, fmt.Errorf("skillsynth: draft did not follow the required structure")
	}

	var tools, caps []string
	for t := range toolsUsed {
		tools = append(tools, t)
	}
	// Declared capabilities = what the run's tools actually required,
	// intersected with what the originating context held. The artifact
	// states its real needs to the reviewer — never the whole context, and
	// never anything the run didn't have.
	capSet := map[string]bool{}
	if s.capOf != nil {
		for t := range toolsUsed {
			c := s.capOf(t)
			if c == "" {
				continue
			}
			if len(s.allowedCaps) > 0 && !s.allowedCaps[c] {
				continue
			}
			capSet[c] = true
		}
	}
	for c := range capSet {
		caps = append(caps, c)
	}
	sort.Strings(caps)
	sort.Strings(tools)

	var trigList []string
	for _, t := range strings.Split(triggers, ",") {
		if t = strings.TrimSpace(t); t != "" {
			trigList = append(trigList, t)
		}
	}

	fm := map[string]interface{}{
		"name":           name,
		"description":    "Self-authored procedure, pending review",
		"summary":        summary,
		"triggers":       trigList,
		"tools":          tools,
		"capabilities":   caps,
		"enabled":        true,
		"version":        1,
		"created_by":     "agent",
		"source_session": info.SessionID,
		"status":         "proposed",
	}
	fmBytes, err := marshalFrontmatter(fm)
	if err != nil {
		return draftedSkill{}, err
	}
	content := "---\n" + string(fmBytes) + "---\n\n# " + name + "\n\n" + procedure + "\n"
	sum := sha256.Sum256([]byte(content))
	return draftedSkill{name: name, content: content, hash: hex.EncodeToString(sum[:])}, nil
}

// Approve activates a staged skill. The staged content is integrity-checked
// against the hash recorded at proposal time (edits after review began are
// refused), the proposal row is claimed first with a status-guarded update
// (so concurrent approve/reject can't disagree about the outcome), and only
// then does the file move into the live skills directory with status
// active. A ledger entry records the activation with the approving human.
// evalRunID links measured evidence when a comparison run exists; 0 records
// the honest "not yet measured" state.
func (s *Synthesizer) Approve(ctx context.Context, proposalID int64, approvedBy string, evalRunID int64) (string, error) {
	name, stagedPath, hash, err := s.loadPending(ctx, proposalID)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(stagedPath)
	if err != nil {
		return "", fmt.Errorf("skillsynth: staged file: %w", err)
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != hash {
		return "", fmt.Errorf("skillsynth: staged skill %q changed since it was proposed — re-review the file", name)
	}

	// Anchored on the frontmatter line, immune to the phrase appearing in
	// the body or a quoted value.
	activated := strings.Replace(string(content), "\nstatus: proposed\n", "\nstatus: active\n", 1)
	livePath := filepath.Join(s.cfg.SkillsDir, name+".md")
	if _, err := os.Stat(livePath); err == nil {
		return "", fmt.Errorf("skillsynth: a skill named %q already exists — reject this proposal or rename", name)
	}
	// User skills override embedded defaults by name — an approval must not
	// silently shadow built-in guidance.
	if defaults, derr := defaultSkillNames(); derr == nil && defaults[name] {
		return "", fmt.Errorf("skillsynth: %q is a built-in skill name — reject this proposal or rename", name)
	}

	// Claim the proposal before touching files: the guarded update loses
	// cleanly to a concurrent approve/reject instead of leaving a live
	// skill recorded as rejected.
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_proposals SET status = 'approved', decided_at = ?, decided_by = ?
		 WHERE id = ? AND status = 'pending'`,
		now, approvedBy, proposalID)
	if err != nil {
		return "", fmt.Errorf("skillsynth: close proposal: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", fmt.Errorf("skillsynth: proposal %d was decided concurrently", proposalID)
	}

	if err := os.WriteFile(livePath, []byte(activated), 0o600); err != nil {
		// Best-effort reopen so the proposal isn't stranded approved with
		// no live skill.
		_, _ = s.db.ExecContext(ctx,
			`UPDATE agent_proposals SET status = 'pending', decided_at = NULL, decided_by = NULL WHERE id = ?`,
			proposalID)
		return "", fmt.Errorf("skillsynth: activate: %w", err)
	}
	_ = os.Remove(stagedPath)

	if s.ledger != nil {
		activatedSum := sha256.Sum256([]byte(activated))
		if _, lerr := s.ledger.Record(ctx, changelog.Entry{
			Kind:       changelog.KindSkillCreated,
			SubjectID:  "skill:" + name,
			AfterHash:  hex.EncodeToString(activatedSum[:]),
			EvalRunID:  evalRunID,
			ApprovedBy: approvedBy,
		}); lerr != nil {
			// The skill IS active; a missing ledger row must be loud.
			log.Printf("skillsynth: WARNING — skill %q activated but the change-ledger write failed: %v", name, lerr)
		}
	}
	return livePath, nil
}

// Reject closes a proposal (status-guarded, so it can't race an approve)
// and removes the staged file.
func (s *Synthesizer) Reject(ctx context.Context, proposalID int64, decidedBy string) error {
	_, stagedPath, _, err := s.loadPending(ctx, proposalID)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_proposals SET status = 'rejected', decided_at = ?, decided_by = ?
		 WHERE id = ? AND status = 'pending'`,
		now, decidedBy, proposalID)
	if err != nil {
		return fmt.Errorf("skillsynth: close proposal: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("skillsynth: proposal %d was decided concurrently", proposalID)
	}
	_ = os.Remove(stagedPath)
	return nil
}

// defaultSkillNames returns the embedded built-in skill names.
func defaultSkillNames() (map[string]bool, error) {
	defaults, err := prompt.LoadDefaultSkills()
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(defaults))
	for _, d := range defaults {
		names[d.Name] = true
	}
	return names, nil
}

// PendingProposal is one staged skill awaiting review.
type PendingProposal struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Created string `json:"created_at"`
	Body    string `json:"body,omitempty"`
}

// Pending lists staged skill proposals, oldest first.
func (s *Synthesizer) Pending(ctx context.Context) ([]PendingProposal, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, COALESCE(payload, ''), created_at FROM agent_proposals
		 WHERE kind = 'skill' AND status = 'pending' ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("skillsynth: pending: %w", err)
	}
	defer rows.Close()
	var out []PendingProposal
	for rows.Next() {
		var p PendingProposal
		var payload string
		if err := rows.Scan(&p.ID, &p.Title, &payload, &p.Created); err != nil {
			return nil, err
		}
		var meta struct{ Path, Name string }
		_ = json.Unmarshal([]byte(payload), &meta)
		p.Path, p.Name = meta.Path, meta.Name
		if body, err := os.ReadFile(meta.Path); err == nil {
			p.Body = string(body)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Synthesizer) loadPending(ctx context.Context, id int64) (name, path, hash string, err error) {
	var payload, status string
	err = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(payload, ''), status FROM agent_proposals WHERE id = ? AND kind = 'skill'`, id).
		Scan(&payload, &status)
	if err == sql.ErrNoRows {
		return "", "", "", fmt.Errorf("skillsynth: proposal %d not found", id)
	}
	if err != nil {
		return "", "", "", fmt.Errorf("skillsynth: %w", err)
	}
	if status != "pending" {
		return "", "", "", fmt.Errorf("skillsynth: proposal %d already %s", id, status)
	}
	var meta struct{ Path, Name, Hash string }
	if err := json.Unmarshal([]byte(payload), &meta); err != nil || meta.Path == "" {
		return "", "", "", fmt.Errorf("skillsynth: proposal %d has a malformed payload", id)
	}
	return meta.Name, meta.Path, meta.Hash, nil
}

var nameRe = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = nameRe.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

func extractField(text, field string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, field+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		}
	}
	return ""
}

func extractSection(text, header string) string {
	idx := strings.Index(text, header)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(text[idx+len(header):])
}

func marshalFrontmatter(fm map[string]interface{}) ([]byte, error) {
	b, err := yamlMarshal(fm)
	if err != nil {
		return nil, fmt.Errorf("skillsynth: frontmatter: %w", err)
	}
	return b, nil
}
