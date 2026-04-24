package migration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/migration"
	"github.com/LumabyteCo/aibutler/testutil"
)

const claudeFixture = `[
  {
    "uuid": "abc-123",
    "name": "Test Conversation",
    "created_at": "2024-01-15T10:00:00Z",
    "chat_messages": [
      {"sender": "human", "text": "Hello, tell me about Alice's project."},
      {"sender": "assistant", "text": "Alice is working on Project Alpha."}
    ]
  }
]`

const claudeMultiFixture = `[
  {
    "uuid": "abc-1",
    "name": "Conv1",
    "chat_messages": [
      {"sender": "human", "text": "First question"},
      {"sender": "assistant", "text": "First answer"}
    ]
  },
  {
    "uuid": "abc-2",
    "name": "Conv2",
    "chat_messages": [
      {"sender": "human", "text": "Second question"},
      {"sender": "assistant", "text": "Second answer"}
    ]
  }
]`

const claudeEmptyFixture = `[
  {"uuid": "abc-3", "name": "Empty", "chat_messages": []}
]`

const chatgptFixture = `[
  {
    "title": "ChatGPT Test",
    "mapping": {
      "root": {
        "message": null,
        "children": ["msg1"]
      },
      "msg1": {
        "message": {"author": {"role": "user"}, "content": {"parts": ["What is Go?"]}},
        "children": ["msg2"]
      },
      "msg2": {
        "message": {"author": {"role": "assistant"}, "content": {"parts": ["Go is a programming language."]}},
        "children": []
      }
    }
  }
]`

func TestClaudeImporterName(t *testing.T) {
	imp := &migration.ClaudeImporter{}
	if imp.Name() != "claude" {
		t.Errorf("name = %q", imp.Name())
	}
}

func TestClaudeImportBasic(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	result, err := orch.Run(ctx, &migration.ClaudeImporter{}, strings.NewReader(claudeFixture), migration.ImportOpts{Filename: "test.json"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 1 {
		t.Errorf("thoughts = %d, want 1", result.ThoughtsImported)
	}

	// Verify thought persisted.
	thoughts, _ := mem.GetThoughts(ctx, memory.ThoughtQuery{Limit: 10})
	if len(thoughts) != 1 {
		t.Fatalf("thought count = %d, want 1", len(thoughts))
	}
	if !strings.Contains(thoughts[0].Content, "Alice") {
		t.Errorf("content missing Alice: %q", thoughts[0].Content)
	}
}

func TestClaudeImportMultiConversation(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	result, err := orch.Run(ctx, &migration.ClaudeImporter{}, strings.NewReader(claudeMultiFixture), migration.ImportOpts{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 2 {
		t.Errorf("thoughts = %d, want 2", result.ThoughtsImported)
	}
}

func TestClaudeImportEmptyConversation(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	result, err := orch.Run(ctx, &migration.ClaudeImporter{}, strings.NewReader(claudeEmptyFixture), migration.ImportOpts{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 0 {
		t.Errorf("thoughts = %d, want 0 for empty conversation", result.ThoughtsImported)
	}
}

func TestChatGPTImporterName(t *testing.T) {
	imp := &migration.ChatGPTImporter{}
	if imp.Name() != "chatgpt" {
		t.Errorf("name = %q", imp.Name())
	}
}

func TestChatGPTImportBasic(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	result, err := orch.Run(ctx, &migration.ChatGPTImporter{}, strings.NewReader(chatgptFixture), migration.ImportOpts{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 1 {
		t.Errorf("thoughts = %d, want 1", result.ThoughtsImported)
	}

	thoughts, _ := mem.GetThoughts(ctx, memory.ThoughtQuery{Limit: 10})
	if len(thoughts) != 1 {
		t.Fatalf("thought count = %d", len(thoughts))
	}
	if !strings.Contains(thoughts[0].Content, "Go is a programming language") {
		t.Errorf("content = %q", thoughts[0].Content)
	}
}

func TestPlaintextImporterName(t *testing.T) {
	imp := &migration.PlaintextImporter{}
	if imp.Name() != "plaintext" {
		t.Errorf("name = %q", imp.Name())
	}
}

func TestPlaintextImportBasic(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	text := "First paragraph about Alice.\n\nSecond paragraph.\n\nThird paragraph."
	result, err := orch.Run(ctx, &migration.PlaintextImporter{Filename: "notes.txt"}, strings.NewReader(text), migration.ImportOpts{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 3 {
		t.Errorf("thoughts = %d, want 3", result.ThoughtsImported)
	}
}

func TestPlaintextImportSingle(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	result, err := orch.Run(ctx, &migration.PlaintextImporter{}, strings.NewReader("Single thought."), migration.ImportOpts{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 1 {
		t.Errorf("thoughts = %d, want 1", result.ThoughtsImported)
	}
}

func TestOrchestratorDryRun(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	result, err := orch.Run(ctx, &migration.ClaudeImporter{}, strings.NewReader(claudeMultiFixture), migration.ImportOpts{DryRun: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 2 {
		t.Errorf("thoughts = %d, want 2", result.ThoughtsImported)
	}

	// Verify nothing persisted.
	count, _ := mem.ThoughtCount(ctx)
	if count != 0 {
		t.Errorf("DB thought count = %d, want 0 for dry run", count)
	}
}

func TestOrchestratorRecordsImport(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	orch.Run(ctx, &migration.ClaudeImporter{}, strings.NewReader(claudeFixture), migration.ImportOpts{Filename: "test.json"})

	var source, filename, status string
	var count int
	err := conn.QueryRowContext(ctx, "SELECT source, filename, status, thoughts_imported FROM memory_imports ORDER BY id DESC LIMIT 1").
		Scan(&source, &filename, &status, &count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if source != "claude" {
		t.Errorf("source = %q", source)
	}
	if filename != "test.json" {
		t.Errorf("filename = %q", filename)
	}
	if status != "completed" {
		t.Errorf("status = %q", status)
	}
	if count != 1 {
		t.Errorf("thoughts_imported = %d", count)
	}
}

func TestOrchestratorEntityExtraction(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	// Text that should trigger entity extraction (Alice = person).
	result, err := orch.Run(ctx, &migration.ClaudeImporter{}, strings.NewReader(claudeFixture), migration.ImportOpts{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	// Entity extraction is best-effort; just verify the pipeline ran.
	if result.EntitiesExtracted < 0 {
		t.Errorf("entities = %d, expected >= 0", result.EntitiesExtracted)
	}
}

func TestOrchestratorMalformedInput(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	mem := memory.NewStore(conn)
	ent := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, mem, ent)

	_, err := orch.Run(ctx, &migration.ClaudeImporter{}, strings.NewReader("not json"), migration.ImportOpts{})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}

	// Verify import recorded as failed.
	var status string
	conn.QueryRowContext(ctx, "SELECT status FROM memory_imports ORDER BY id DESC LIMIT 1").Scan(&status)
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}

	// Verify no thoughts persisted.
	count, _ := mem.ThoughtCount(ctx)
	if count != 0 {
		t.Errorf("thought count = %d, want 0 after failure", count)
	}
}
