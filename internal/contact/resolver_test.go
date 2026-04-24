package contact_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/contact"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestResolveExactMatch(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	conn.ExecContext(ctx, `INSERT INTO user_contacts (name, email, preferred_channel) VALUES ('Alice Smith', 'alice@test.com', 'telegram')`)

	r := contact.NewResolver(conn)
	c, err := r.ResolveOne(ctx, "Alice Smith")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if c.Name != "Alice Smith" {
		t.Errorf("name = %q, want Alice Smith", c.Name)
	}
}

func TestResolveCaseInsensitive(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	conn.ExecContext(ctx, `INSERT INTO user_contacts (name) VALUES ('Bob Jones')`)

	r := contact.NewResolver(conn)
	c, err := r.ResolveOne(ctx, "bob jones")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if c.Name != "Bob Jones" {
		t.Errorf("name = %q, want Bob Jones", c.Name)
	}
}

func TestResolveFuzzy(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	conn.ExecContext(ctx, `INSERT INTO user_contacts (name) VALUES ('Muhammad Ali Hassan')`)

	r := contact.NewResolver(conn)
	c, err := r.ResolveOne(ctx, "Ali")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if c.Name != "Muhammad Ali Hassan" {
		t.Errorf("name = %q, want Muhammad Ali Hassan", c.Name)
	}
}

func TestResolveZeroMatches(t *testing.T) {
	db := testutil.TestDB(t)
	r := contact.NewResolver(db.Conn())

	_, err := r.ResolveOne(context.Background(), "Nobody")
	if err == nil {
		t.Fatal("expected error for zero matches")
	}
	if !strings.Contains(err.Error(), "no contact found") {
		t.Errorf("error = %q, want 'no contact found'", err.Error())
	}
}

func TestResolveDisambiguation(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	conn.ExecContext(ctx, `INSERT INTO user_contacts (name) VALUES ('Ali A')`)
	conn.ExecContext(ctx, `INSERT INTO user_contacts (name) VALUES ('Ali B')`)

	r := contact.NewResolver(conn)
	_, err := r.ResolveOne(ctx, "Ali")
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(err.Error(), "multiple matches") {
		t.Errorf("error = %q, want 'multiple matches'", err.Error())
	}
}

func TestResolveEmptyQuery(t *testing.T) {
	db := testutil.TestDB(t)
	r := contact.NewResolver(db.Conn())
	_, err := r.Resolve(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

// --- Arabic Normalization Tests ---

func TestNormalizeArabicHamzaFolding(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"أحمد", "احمد"},   // Hamza above → bare alef
		{"إبراهيم", "ابراهيم"}, // Hamza below → bare alef
		{"آدم", "ادم"},     // Madda → bare alef
	}
	for _, tc := range tests {
		got := contact.NormalizeArabic(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeArabic(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeArabicDiacritics(t *testing.T) {
	// "محمَّد" with shadda+fatha → "محمد"
	input := "مُحَمَّد"
	got := contact.NormalizeArabic(input)
	// All diacritics should be stripped.
	for _, r := range got {
		if r >= 0x064B && r <= 0x065F {
			t.Errorf("diacritic %U not stripped in %q", r, got)
		}
	}
}

func TestNormalizeArabicTatweel(t *testing.T) {
	input := "عـلـي"
	got := contact.NormalizeArabic(input)
	if strings.ContainsRune(got, 0x0640) {
		t.Errorf("tatweel not stripped in %q", got)
	}
}

func TestNormalizeArabicTaaMarbuta(t *testing.T) {
	input := "فاطمة"
	got := contact.NormalizeArabic(input)
	if strings.ContainsRune(got, 0x0629) {
		t.Errorf("taa marbuta not normalized in %q", got)
	}
}

func TestResolveArabicName(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	// Store with exact name.
	conn.ExecContext(ctx, `INSERT INTO user_contacts (name) VALUES ('احمد')`)

	r := contact.NewResolver(conn)
	// Search with same name — after normalization both should match.
	contacts, err := r.Resolve(ctx, "احمد")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(contacts) != 1 {
		t.Errorf("expected 1 contact, got %d", len(contacts))
	}
}

func TestResolveArabicSQLiteRoundTrip(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	// Verify Arabic text survives SQLite round-trip.
	arabicName := "محمد علي"
	conn.ExecContext(ctx, `INSERT INTO user_contacts (name) VALUES (?)`, arabicName)

	r := contact.NewResolver(conn)
	contacts, err := r.Resolve(ctx, "محمد")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	if contacts[0].Name != arabicName {
		t.Errorf("name = %q, want %q", contacts[0].Name, arabicName)
	}
}
