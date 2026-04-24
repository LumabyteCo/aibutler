package contact_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/media/contact"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

const minimalVCard = `BEGIN:VCARD
VERSION:3.0
FN:Alice Smith
END:VCARD`

const fullVCard = `BEGIN:VCARD
VERSION:3.0
FN:Bob Jones
N:Jones;Bob;;;
EMAIL;TYPE=INTERNET:bob@example.com
TEL;TYPE=CELL:+15551234567
ORG:Acme Corp
END:VCARD`

func TestParseVCard_Minimal(t *testing.T) {
	card, err := contact.ParseVCard(minimalVCard)
	if err != nil {
		t.Fatalf("ParseVCard minimal: %v", err)
	}
	if card.Name != "Alice Smith" {
		t.Errorf("expected 'Alice Smith', got %q", card.Name)
	}
}

func TestParseVCard_Full(t *testing.T) {
	card, err := contact.ParseVCard(fullVCard)
	if err != nil {
		t.Fatalf("ParseVCard full: %v", err)
	}
	if card.Name != "Bob Jones" {
		t.Errorf("expected 'Bob Jones', got %q", card.Name)
	}
	if card.Email != "bob@example.com" {
		t.Errorf("expected email 'bob@example.com', got %q", card.Email)
	}
	if card.Phone != "+15551234567" {
		t.Errorf("expected phone '+15551234567', got %q", card.Phone)
	}
	if card.Organization != "Acme Corp" {
		t.Errorf("expected org 'Acme Corp', got %q", card.Organization)
	}
}

func TestParseVCardFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.vcf")
	content := minimalVCard + "\n" + fullVCard
	os.WriteFile(path, []byte(content), 0600)

	cards, err := contact.ParseVCardFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseVCardFile: %v", err)
	}
	if len(cards) != 2 {
		t.Errorf("expected 2 contacts, got %d", len(cards))
	}
}

func TestRegisterContactTools(t *testing.T) {
	reg := newMockRegistry()
	contact.RegisterContactTools(reg)

	want := map[string]bool{
		"media.contact.parse":      false,
		"media.contact.parse_file": false,
	}
	for _, name := range reg.tools {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q was not registered", name)
		}
	}
}
