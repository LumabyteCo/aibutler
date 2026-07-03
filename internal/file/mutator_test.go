package file

import (
	"path/filepath"
	"testing"
)

// mutationTarget declares the raw path for checkpointing; boundary
// enforcement is the checkpoint layer's job (one authoritative pre-read
// check), so any well-formed path is declared — including ones Execute will
// later reject.
func TestMutationTargetExtraction(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "f.txt")

	got := mutationTarget(`{"path":"` + inside + `"}`)
	if len(got) != 1 || got[0] != inside {
		t.Fatalf("path = %v, want [%s]", got, inside)
	}
	// Even out-of-boundary paths are declared — the checkpoint layer fails
	// the call on them, which is the fail-closed behavior we want.
	got = mutationTarget(`{"path":"/etc/passwd"}`)
	if len(got) != 1 || got[0] != "/etc/passwd" {
		t.Fatalf("out-of-boundary path should still be declared, got %v", got)
	}
	if got := mutationTarget(`{"path":""}`); got != nil {
		t.Fatalf("empty path must yield nil, got %v", got)
	}
	if got := mutationTarget(`not-json`); got != nil {
		t.Fatalf("bad json must yield nil, got %v", got)
	}
}
