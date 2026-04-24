package memory_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newTranscriptStore(t *testing.T) *memory.Store {
	t.Helper()
	db := testutil.TestDB(t)
	return memory.NewStore(db.Conn())
}

func TestSaveAndGetTranscript(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()

	id, err := store.SaveTranscript(ctx, "sess-1", "user", "Hello, how are you?", 1)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	transcripts, err := store.GetTranscripts(ctx, "sess-1", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("got %d, want 1", len(transcripts))
	}
	if transcripts[0].Content != "Hello, how are you?" {
		t.Errorf("content = %q", transcripts[0].Content)
	}
	if transcripts[0].Role != "user" {
		t.Errorf("role = %q, want user", transcripts[0].Role)
	}
	if transcripts[0].TurnNumber != 1 {
		t.Errorf("turn = %d, want 1", transcripts[0].TurnNumber)
	}
}

func TestTranscriptMultipleTurns(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()

	store.SaveTranscript(ctx, "sess-1", "user", "What is Go?", 1)
	store.SaveTranscript(ctx, "sess-1", "assistant", "Go is a programming language.", 2)
	store.SaveTranscript(ctx, "sess-1", "user", "Tell me more.", 3)

	transcripts, err := store.GetTranscripts(ctx, "sess-1", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(transcripts) != 3 {
		t.Fatalf("got %d, want 3", len(transcripts))
	}

	// Should be ordered by turn_number ASC.
	for i, tr := range transcripts {
		if tr.TurnNumber != i+1 {
			t.Errorf("transcripts[%d].TurnNumber = %d, want %d", i, tr.TurnNumber, i+1)
		}
	}
}

func TestTranscriptSessionIsolation(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()

	store.SaveTranscript(ctx, "sess-1", "user", "First session", 1)
	store.SaveTranscript(ctx, "sess-2", "user", "Second session", 1)
	store.SaveTranscript(ctx, "sess-1", "assistant", "Reply to first", 2)

	t1, _ := store.GetTranscripts(ctx, "sess-1", 10)
	if len(t1) != 2 {
		t.Errorf("sess-1 transcripts = %d, want 2", len(t1))
	}

	t2, _ := store.GetTranscripts(ctx, "sess-2", 10)
	if len(t2) != 1 {
		t.Errorf("sess-2 transcripts = %d, want 1", len(t2))
	}
}

func TestTranscriptEmptyContent(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()

	id, err := store.SaveTranscript(ctx, "sess-1", "user", "", 1)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id != 0 {
		t.Errorf("expected id=0 for empty content, got %d", id)
	}
}

func TestTranscriptCount(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()

	count, _ := store.TranscriptCount(ctx)
	if count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	store.SaveTranscript(ctx, "sess-1", "user", "Hello", 1)
	store.SaveTranscript(ctx, "sess-1", "assistant", "Hi", 2)
	store.SaveTranscript(ctx, "sess-2", "user", "Hey", 1)

	count, _ = store.TranscriptCount(ctx)
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestTranscriptLimit(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()

	for i := 1; i <= 10; i++ {
		store.SaveTranscript(ctx, "sess-1", "user", "message", i)
	}

	transcripts, _ := store.GetTranscripts(ctx, "sess-1", 5)
	if len(transcripts) != 5 {
		t.Errorf("got %d, want 5", len(transcripts))
	}
	// First 5 turns (ordered by turn_number ASC).
	if transcripts[0].TurnNumber != 1 {
		t.Errorf("first turn = %d, want 1", transcripts[0].TurnNumber)
	}
}

func TestTranscriptDefaultLimit(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		store.SaveTranscript(ctx, "sess-1", "user", "message", i)
	}

	// 0 means use default (100).
	transcripts, _ := store.GetTranscripts(ctx, "sess-1", 0)
	if len(transcripts) != 5 {
		t.Errorf("got %d, want 5", len(transcripts))
	}
}

func TestNextTurnNumber(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()

	// Empty session should start at 0.
	next := store.NextTurnNumber(ctx, "new-sess")
	if next != 0 {
		t.Errorf("NextTurnNumber for empty session = %d, want 0", next)
	}

	// After saving turns 0,1,2 — next should be 3.
	store.SaveTranscript(ctx, "sess-x", "user", "hello", 0)
	store.SaveTranscript(ctx, "sess-x", "assistant", "hi", 1)
	store.SaveTranscript(ctx, "sess-x", "tool", "result", 2)

	next = store.NextTurnNumber(ctx, "sess-x")
	if next != 3 {
		t.Errorf("NextTurnNumber = %d, want 3", next)
	}

	// Different session should be independent.
	next = store.NextTurnNumber(ctx, "other-sess")
	if next != 0 {
		t.Errorf("NextTurnNumber for other session = %d, want 0", next)
	}
}
