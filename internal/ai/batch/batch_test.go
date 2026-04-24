package batch_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/ai/batch"
)

type mockRunner struct {
	callCount int
}

func (m *mockRunner) CallTool(ctx context.Context, name, input string) (string, error) {
	m.callCount++
	return fmt.Sprintf("result-%d", m.callCount), nil
}

func TestBatchGenerate(t *testing.T) {
	runner := &mockRunner{}
	prompts := []string{"prompt1", "prompt2", "prompt3"}

	results, err := batch.BatchGenerate(context.Background(), runner, "test.tool", prompts)
	if err != nil {
		t.Fatalf("batch generate: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("result count = %d, want 3", len(results))
	}
	if runner.callCount != 3 {
		t.Errorf("call count = %d, want 3", runner.callCount)
	}
}

func TestBatchGenerateEmptyPrompts(t *testing.T) {
	runner := &mockRunner{}
	results, err := batch.BatchGenerate(context.Background(), runner, "test.tool", []string{})
	if err != nil {
		t.Fatalf("batch generate empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("result count = %d, want 0", len(results))
	}
	if runner.callCount != 0 {
		t.Errorf("call count = %d, want 0", runner.callCount)
	}
}
