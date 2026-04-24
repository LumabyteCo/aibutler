package browser_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/browser"
)

func TestInteractiveClick(t *testing.T) {
	ic := browser.NewInteractiveClient()
	result, err := ic.Click(context.Background(), "https://example.com/page", "button.submit")
	if err != nil {
		t.Fatalf("Click: unexpected error: %v", err)
	}
	if !strings.Contains(result, "click") {
		t.Errorf("expected result to contain 'click', got: %s", result)
	}
	if !strings.Contains(result, "button.submit") {
		t.Errorf("expected result to contain selector, got: %s", result)
	}
}

func TestInteractiveType(t *testing.T) {
	ic := browser.NewInteractiveClient()
	result, err := ic.Type(context.Background(), "https://example.com/page", "input#name", "John Doe")
	if err != nil {
		t.Fatalf("Type: unexpected error: %v", err)
	}
	if !strings.Contains(result, "type") {
		t.Errorf("expected result to contain 'type', got: %s", result)
	}
	if !strings.Contains(result, "John Doe") {
		t.Errorf("expected result to contain typed text, got: %s", result)
	}
}

func TestInteractiveSubmit_Confirmation(t *testing.T) {
	ic := browser.NewInteractiveClient()

	// First call without confirmation.
	result, err := ic.Submit(context.Background(), "https://example.com/form", "form#signup", false)
	if err != nil {
		t.Fatalf("Submit (unconfirmed): unexpected error: %v", err)
	}

	var submitResult browser.SubmitResult
	if err := json.Unmarshal([]byte(result), &submitResult); err != nil {
		t.Fatalf("unmarshal submit result: %v", err)
	}
	if submitResult.Status != "confirmation_required" {
		t.Errorf("expected status=confirmation_required, got %s", submitResult.Status)
	}
	if submitResult.Confirmed {
		t.Error("expected confirmed=false")
	}

	// Second call with confirmation.
	result, err = ic.Submit(context.Background(), "https://example.com/form", "form#signup", true)
	if err != nil {
		t.Fatalf("Submit (confirmed): unexpected error: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &submitResult); err != nil {
		t.Fatalf("unmarshal submit result: %v", err)
	}
	if submitResult.Status != "submitted" {
		t.Errorf("expected status=submitted, got %s", submitResult.Status)
	}
	if !submitResult.Confirmed {
		t.Error("expected confirmed=true")
	}
}

func TestInteractiveType_PasswordRejection(t *testing.T) {
	ic := browser.NewInteractiveClient()

	testCases := []string{
		"input[type=\"password\"]",
		"input.password-field",
		"#password",
	}

	for _, sel := range testCases {
		_, err := ic.Type(context.Background(), "https://example.com/login", sel, "secret123")
		if err == nil {
			t.Errorf("expected error for password selector %q, got nil", sel)
		}
		if err != nil && !strings.Contains(err.Error(), "password") {
			t.Errorf("expected password-related error for selector %q, got: %v", sel, err)
		}
	}
}

func TestInteractiveCrossDomainBlocking(t *testing.T) {
	ic := browser.NewInteractiveClient()

	// First call sets the domain.
	_, err := ic.Click(context.Background(), "https://example.com/page1", "a.link")
	if err != nil {
		t.Fatalf("Click (first): unexpected error: %v", err)
	}

	// Same domain should work.
	_, err = ic.Click(context.Background(), "https://example.com/page2", "a.link")
	if err != nil {
		t.Fatalf("Click (same domain): unexpected error: %v", err)
	}

	// Different domain should be blocked.
	_, err = ic.Click(context.Background(), "https://evil.com/page", "a.link")
	if err == nil {
		t.Fatal("expected cross-domain error, got nil")
	}
	if !strings.Contains(err.Error(), "cross-domain") {
		t.Errorf("expected cross-domain error, got: %v", err)
	}

	// After reset, new domain should work.
	ic.ResetDomain()
	_, err = ic.Click(context.Background(), "https://other.com/page", "a.link")
	if err != nil {
		t.Fatalf("Click (after reset): unexpected error: %v", err)
	}
}
