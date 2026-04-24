package instruction_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/instruction"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newStore(t *testing.T) *instruction.Store {
	t.Helper()
	db := testutil.TestDB(t)
	return instruction.NewStore(db.Conn())
}

func TestStoreSave(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id, err := store.Save(ctx, "always reply in Arabic", "rule", 80, "global", "", "explicit", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
}

func TestStoreSaveDuplicate(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	_, err := store.Save(ctx, "always reply in Arabic", "rule", 50, "global", "", "explicit", "")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	_, err = store.Save(ctx, "always reply in Arabic", "rule", 50, "global", "", "explicit", "")
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestStoreGet(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id, _ := store.Save(ctx, "be concise", "style", 60, "global", "", "explicit", "")

	inst, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if inst == nil {
		t.Fatal("expected non-nil instruction")
	}
	if inst.Content != "be concise" {
		t.Errorf("content = %q, want %q", inst.Content, "be concise")
	}
	if inst.Category != "style" {
		t.Errorf("category = %q, want %q", inst.Category, "style")
	}
	if inst.Priority != 60 {
		t.Errorf("priority = %d, want 60", inst.Priority)
	}
	if !inst.Active {
		t.Error("expected active = true")
	}
}

func TestStoreGetNotFound(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	inst, err := store.Get(ctx, 9999)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if inst != nil {
		t.Fatal("expected nil for non-existent id")
	}
}

func TestStoreList(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "rule one", "rule", 50, "global", "", "explicit", "")
	store.Save(ctx, "style one", "style", 30, "global", "", "explicit", "")
	store.Save(ctx, "rule two", "rule", 80, "global", "", "explicit", "")

	list, err := store.List(ctx, instruction.ListQuery{ActiveOnly: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d, want 3", len(list))
	}
	// Priority DESC order: 80, 50, 30.
	if list[0].Priority != 80 {
		t.Errorf("first priority = %d, want 80", list[0].Priority)
	}
}

func TestStoreListByCategory(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "rule one", "rule", 50, "global", "", "explicit", "")
	store.Save(ctx, "style one", "style", 50, "global", "", "explicit", "")

	list, err := store.List(ctx, instruction.ListQuery{Category: "rule", ActiveOnly: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d, want 1", len(list))
	}
	if list[0].Category != "rule" {
		t.Errorf("category = %q, want rule", list[0].Category)
	}
}

func TestStoreListActiveOnly(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "active rule", "rule", 50, "global", "", "explicit", "")
	id2, _ := store.Save(ctx, "inactive rule", "rule", 50, "global", "", "explicit", "")

	// Deactivate one.
	f := false
	store.Update(ctx, id2, "", 0, "", &f)

	list, err := store.List(ctx, instruction.ListQuery{ActiveOnly: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d, want 1", len(list))
	}
}

func TestStoreUpdate(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id, _ := store.Save(ctx, "old content", "rule", 50, "global", "", "explicit", "")
	err := store.Update(ctx, id, "new content", 80, "style", nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	inst, _ := store.Get(ctx, id)
	if inst.Content != "new content" {
		t.Errorf("content = %q, want %q", inst.Content, "new content")
	}
	if inst.Priority != 80 {
		t.Errorf("priority = %d, want 80", inst.Priority)
	}
	if inst.Category != "style" {
		t.Errorf("category = %q, want style", inst.Category)
	}
}

func TestStoreRemove(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id, _ := store.Save(ctx, "to remove", "rule", 50, "global", "", "explicit", "")
	err := store.Remove(ctx, id)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}

	inst, _ := store.Get(ctx, id)
	if inst != nil {
		t.Fatal("expected nil after removal")
	}
}

func TestStoreActiveForPrompt(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "global rule", "rule", 50, "global", "", "explicit", "")

	list, err := store.ActiveForPrompt(ctx, "webchat", "sess-1")
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d, want 1", len(list))
	}
	if list[0].Content != "global rule" {
		t.Errorf("content = %q, want %q", list[0].Content, "global rule")
	}
}

func TestStoreActiveForPromptChannelScope(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "global rule", "rule", 50, "global", "", "explicit", "")
	store.Save(ctx, "slack only", "style", 50, "channel", "slack", "explicit", "")

	// Webchat should only get global.
	list, _ := store.ActiveForPrompt(ctx, "webchat", "")
	if len(list) != 1 {
		t.Fatalf("webchat: got %d, want 1", len(list))
	}

	// Slack should get both.
	list, _ = store.ActiveForPrompt(ctx, "slack", "")
	if len(list) != 2 {
		t.Fatalf("slack: got %d, want 2", len(list))
	}
}

func TestStoreActiveForPromptSessionScope(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "session rule", "rule", 50, "session", "sess-A", "explicit", "")

	list, _ := store.ActiveForPrompt(ctx, "", "sess-A")
	if len(list) != 1 {
		t.Fatalf("matching session: got %d, want 1", len(list))
	}

	list, _ = store.ActiveForPrompt(ctx, "", "sess-B")
	if len(list) != 0 {
		t.Fatalf("non-matching session: got %d, want 0", len(list))
	}
}

func TestStoreCount(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "one", "rule", 50, "global", "", "explicit", "")
	store.Save(ctx, "two", "rule", 50, "global", "", "explicit", "")

	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestStoreArabicContent(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	content := "دائماً رد بالعربية عند التحية"
	id, err := store.Save(ctx, content, "rule", 80, "global", "", "explicit", "")
	if err != nil {
		t.Fatalf("save Arabic: %v", err)
	}

	inst, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if inst.Content != content {
		t.Errorf("Arabic content mismatch: got %q", inst.Content)
	}
}

func TestStoreConflictDetection(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "always use verbose output", "style", 50, "global", "", "explicit", "")

	conflicts, err := store.CheckConflicts(ctx, "always use concise output")
	if err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	if len(conflicts) == 0 {
		t.Fatal("expected at least one conflict (verbose vs concise)")
	}
}

func TestStoreConflictNegation(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "always respond in English", "rule", 50, "global", "", "explicit", "")

	conflicts, _ := store.CheckConflicts(ctx, "never respond in English")
	if len(conflicts) == 0 {
		t.Fatal("expected negation conflict")
	}
}

func TestStoreConflictNoConflict(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "be formal", "style", 50, "global", "", "explicit", "")

	conflicts, _ := store.CheckConflicts(ctx, "reply in Arabic")
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %d", len(conflicts))
	}
}

func TestStoreRenderMarkdown(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Save(ctx, "always respond in Arabic", "rule", 80, "global", "", "explicit", "")
	store.Save(ctx, "use formal tone", "style", 50, "global", "", "explicit", "")

	md, err := store.RenderMarkdown(ctx)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if md == "" {
		t.Fatal("expected non-empty markdown")
	}
	if !contains(md, "Learned Instructions (2 active)") {
		t.Errorf("markdown missing header: %s", md)
	}
	if !contains(md, "[P80]") {
		t.Errorf("markdown missing priority tag: %s", md)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
