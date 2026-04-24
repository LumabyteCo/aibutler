package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p.MaxMemoryMB != 64 {
		t.Errorf("MaxMemoryMB = %d, want 64", p.MaxMemoryMB)
	}
	if !p.AllowNetwork {
		t.Error("DefaultPolicy should allow network")
	}
}

func TestStrictPolicy(t *testing.T) {
	p := StrictPolicy()
	if p.MaxMemoryMB != 32 {
		t.Errorf("MaxMemoryMB = %d, want 32", p.MaxMemoryMB)
	}
	if p.AllowNetwork {
		t.Error("StrictPolicy should not allow network")
	}
	if p.AllowFileSystem {
		t.Error("StrictPolicy should not allow filesystem")
	}
	if p.MaxExecutionSecs != 30 {
		t.Errorf("MaxExecutionSecs = %d, want 30", p.MaxExecutionSecs)
	}
}

func TestValidate_StrictRejectsNetwork(t *testing.T) {
	sb := New(StrictPolicy())
	m := &Manifest{Name: "net-plugin", Capabilities: []string{"http.get"}}
	err := sb.Validate(m)
	if err == nil {
		t.Error("expected error for network capability under strict policy")
	}
}

func TestValidate_DefaultAllowsNetwork(t *testing.T) {
	sb := New(DefaultPolicy())
	m := &Manifest{Name: "net-plugin", Capabilities: []string{"http.get"}}
	err := sb.Validate(m)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWrapExecution_Timeout(t *testing.T) {
	policy := Policy{
		MaxExecutionSecs: 1,
		MaxOutputBytes:   1024,
	}
	sb := New(policy)

	_, err := sb.WrapExecution(context.Background(), func(ctx context.Context) ([]byte, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return []byte("done"), nil
		}
	})
	if err == nil {
		t.Error("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestWrapExecution_OutputLimit(t *testing.T) {
	policy := Policy{
		MaxExecutionSecs: 10,
		MaxOutputBytes:   5,
	}
	sb := New(policy)

	_, err := sb.WrapExecution(context.Background(), func(ctx context.Context) ([]byte, error) {
		return []byte("this is way too long"), nil
	})
	if err == nil {
		t.Error("expected output size error")
	}
}
