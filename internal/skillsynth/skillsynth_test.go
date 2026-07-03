package skillsynth_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/changelog"
	"github.com/LumabyteCo/aibutler/internal/db"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/skillsynth"
	"github.com/LumabyteCo/aibutler/testutil"
)

// draftModel returns a well-formed skill draft.
type draftModel struct{}

func (draftModel) Complete(_ context.Context, _ []agent.Message) (agent.Response, error) {
	return agent.Response{Content: `NAME: weekly-report-flow
SUMMARY: assemble weekly status report
TRIGGERS: weekly report, status summary, team update
PROCEDURE:
1. Gather the week's completed items.
2. Run the report generator.
3. Verify the output builds cleanly.`}, nil
}

func groundedRun() skillsynth.RunInfo {
	return skillsynth.RunInfo{
		SessionID:   "s1",
		UserMessage: "put together the weekly report",
		Status:      "completed",
		ToolOutputs: []agent.ToolOutput{
			{ToolName: "file.read", Output: "notes"},
			{ToolName: "file.write", Output: "written"},
			{ToolName: "file.edit", Output: "edited"},
			{ToolName: "shell.exec", Output: "ran"},
			{ToolName: "code.test", Output: "ok: all tests passed"},
		},
	}
}

func newSynth(t *testing.T, dir string) (*skillsynth.Synthesizer, *db.DB) {
	t.Helper()
	db := testutil.TestDB(t)
	ledger := changelog.New(db.Conn(), nil)
	s := skillsynth.New(skillsynth.Config{
		Enabled:   true,
		SkillsDir: dir,
	}, draftModel{}, db.Conn(), ledger, []string{"memory.read", "memory.write", "tool.file.read"})
	s.SetToolCapabilityResolver(func(tool string) string {
		return map[string]string{
			"file.read":  "tool.file.read",
			"file.write": "tool.file.write", // NOT in the allowed set — must be clamped out
			"code.test":  "tool.code.test",  // NOT in the allowed set either
		}[tool]
	})
	return s, db
}

func TestGateRequiresGroundedSuccess(t *testing.T) {
	s, _ := newSynth(t, t.TempDir())

	// Enough calls but no verification tool → no synthesis.
	info := groundedRun()
	info.ToolOutputs[4] = agent.ToolOutput{ToolName: "file.read", Output: "x"}
	if s.ShouldSynthesize(info) {
		t.Fatal("no grounded success signal — must not synthesize")
	}
	// Verification tool errored → no synthesis.
	info.ToolOutputs[4] = agent.ToolOutput{ToolName: "code.test", Output: "error: 2 tests failed"}
	if s.ShouldSynthesize(info) {
		t.Fatal("errored verification — must not synthesize")
	}
	// Too few calls → no synthesis even with success.
	short := skillsynth.RunInfo{ToolOutputs: []agent.ToolOutput{{ToolName: "code.test", Output: "ok"}}}
	if s.ShouldSynthesize(short) {
		t.Fatal("routine short run — must not synthesize")
	}
	// A blocked verification (policy/repeat-breaker advisory) never ran —
	// nothing was verified.
	blocked := groundedRun()
	blocked.ToolOutputs[4] = agent.ToolOutput{ToolName: "code.test", Output: "blocked by autonomy policy: code.test requires confirmation"}
	if s.ShouldSynthesize(blocked) {
		t.Fatal("blocked verification — must not synthesize")
	}
	// A pass followed by a later failing check is not verified work: the
	// LAST verification decides.
	flaky := groundedRun()
	flaky.ToolOutputs = append(flaky.ToolOutputs, agent.ToolOutput{ToolName: "code.test", Output: "error: 1 test failed"})
	if s.ShouldSynthesize(flaky) {
		t.Fatal("last verification failed — must not synthesize")
	}
	// Non-completed runs never synthesize, even with a passing check.
	failedRun := groundedRun()
	failedRun.Status = "failed"
	if s.ShouldSynthesize(failedRun) {
		t.Fatal("failed run — must not synthesize")
	}
	// The real thing passes the gate.
	if !s.ShouldSynthesize(groundedRun()) {
		t.Fatal("grounded multi-step success must pass the gate")
	}
}

// Staging refuses to clobber an existing pending proposal's file.
func TestSynthesizeRefusesStageCollision(t *testing.T) {
	dir := t.TempDir()
	s, _ := newSynth(t, dir)
	ctx := context.Background()
	if _, err := s.Synthesize(ctx, groundedRun()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Synthesize(ctx, groundedRun()); err == nil ||
		!strings.Contains(err.Error(), "already staged") {
		t.Fatalf("second staging of the same name must be refused, got %v", err)
	}
}

func TestSynthesizeStagesProposalOutsideLiveDir(t *testing.T) {
	dir := t.TempDir()
	s, db := newSynth(t, dir)
	ctx := context.Background()

	id, err := s.Synthesize(ctx, groundedRun())
	if err != nil || id == 0 {
		t.Fatalf("synthesize: id=%d err=%v", id, err)
	}

	// Staged under proposed/, invisible to the skill loader.
	staged := filepath.Join(dir, "proposed", "weekly-report-flow.md")
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staged file missing: %v", err)
	}
	skills, err := prompt.LoadSkillsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Fatalf("loader must not see staged proposals, saw %d", len(skills))
	}

	// Even loaded directly, a proposed skill is disabled (defense-in-depth).
	sk, err := prompt.LoadSkill(staged)
	if err != nil {
		t.Fatal(err)
	}
	if sk.Enabled || sk.Status != "proposed" || sk.CreatedBy != "agent" || sk.Version != 1 {
		t.Fatalf("staged skill state wrong: %+v", sk)
	}
	// Capability declaration = used-tool capabilities ∩ originating context:
	// file.read maps to tool.file.read (allowed → declared); file.write and
	// code.test map to resources outside the context (clamped out).
	if len(sk.Capabilities) != 1 || sk.Capabilities[0] != "tool.file.read" {
		t.Fatalf("capabilities = %v, want exactly [tool.file.read]", sk.Capabilities)
	}

	// Proposal row pending.
	var status string
	if err := db.Conn().QueryRowContext(ctx,
		`SELECT status FROM agent_proposals WHERE id = ?`, id).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("proposal status = %q (err %v), want pending", status, err)
	}
}

func TestApproveActivatesAndLogs(t *testing.T) {
	dir := t.TempDir()
	s, db := newSynth(t, dir)
	ctx := context.Background()

	id, err := s.Synthesize(ctx, groundedRun())
	if err != nil {
		t.Fatal(err)
	}
	livePath, err := s.Approve(ctx, id, "user", 0)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Now loadable, enabled, active.
	skills, err := prompt.LoadSkillsDir(dir)
	if err != nil || len(skills) != 1 {
		t.Fatalf("expected 1 live skill, got %d (err %v)", len(skills), err)
	}
	if !skills[0].Enabled || skills[0].Status != "active" {
		t.Fatalf("approved skill not active: %+v", skills[0])
	}
	if livePath != filepath.Join(dir, "weekly-report-flow.md") {
		t.Errorf("unexpected live path %s", livePath)
	}
	// Staged copy gone; proposal closed; ledger entry present.
	if _, err := os.Stat(filepath.Join(dir, "proposed", "weekly-report-flow.md")); !os.IsNotExist(err) {
		t.Error("staged copy should be removed after approval")
	}
	var status, decidedBy string
	if err := db.Conn().QueryRowContext(ctx,
		`SELECT status, COALESCE(decided_by,'') FROM agent_proposals WHERE id = ?`, id).Scan(&status, &decidedBy); err != nil ||
		status != "approved" || decidedBy != "user" {
		t.Fatalf("proposal not closed correctly: %s/%s (err %v)", status, decidedBy, err)
	}
	var n int
	if err := db.Conn().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_changes WHERE kind = 'skill_created' AND subject_id = 'skill:weekly-report-flow'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("ledger entries = %d (err %v), want 1", n, err)
	}
}

func TestApproveDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	s, _ := newSynth(t, dir)
	ctx := context.Background()

	id, err := s.Synthesize(ctx, groundedRun())
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, "proposed", "weekly-report-flow.md")
	content, _ := os.ReadFile(staged)
	if err := os.WriteFile(staged, append(content, []byte("\n<!-- edited after staging -->\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(ctx, id, "user", 0); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("tampered staged file must fail approval, got %v", err)
	}
}

func TestApproveRefusesNameCollision(t *testing.T) {
	dir := t.TempDir()
	s, _ := newSynth(t, dir)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "weekly-report-flow.md"),
		[]byte("---\nname: weekly-report-flow\nenabled: true\n---\nexisting"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := s.Synthesize(ctx, groundedRun())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(ctx, id, "user", 0); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("name collision must fail approval, got %v", err)
	}
}

func TestRejectClosesAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	s, db := newSynth(t, dir)
	ctx := context.Background()

	id, err := s.Synthesize(ctx, groundedRun())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reject(ctx, id, "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "proposed", "weekly-report-flow.md")); !os.IsNotExist(err) {
		t.Error("rejected staged file should be removed")
	}
	var status string
	_ = db.Conn().QueryRowContext(ctx, `SELECT status FROM agent_proposals WHERE id = ?`, id).Scan(&status)
	if status != "rejected" {
		t.Fatalf("status = %q, want rejected", status)
	}
	// Deciding twice fails cleanly.
	if err := s.Reject(ctx, id, "user"); err == nil {
		t.Fatal("double-decide must error")
	}
}

func TestPendingCapBlocksRunaway(t *testing.T) {
	dir := t.TempDir()
	db := testutil.TestDB(t)
	s := skillsynth.New(skillsynth.Config{
		Enabled: true, SkillsDir: dir, MaxPending: 1,
	}, draftModel{}, db.Conn(), changelog.New(db.Conn(), nil), nil)
	ctx := context.Background()

	if _, err := s.Synthesize(ctx, groundedRun()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Synthesize(ctx, groundedRun()); err == nil ||
		!strings.Contains(err.Error(), "pending") {
		t.Fatalf("pending cap must block further synthesis, got %v", err)
	}
}
