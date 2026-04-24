package contact

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Contact holds resolved contact information.
type Contact struct {
	ID               int
	Name             string
	Phone            string
	Email            string
	PreferredChannel string
	ChannelIDs       string // JSON map of channel→accountID
}

// Resolver performs fuzzy name lookups against the contacts table.
type Resolver struct {
	db *sql.DB
}

// NewResolver creates a contact resolver backed by the given DB.
func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{db: db}
}

// Resolve finds contacts matching a query (case-insensitive, fuzzy).
// Returns all matches, leaving disambiguation to the caller.
func (r *Resolver) Resolve(ctx context.Context, query string) ([]Contact, error) {
	if query == "" {
		return nil, fmt.Errorf("contact: empty query")
	}

	// Normalize for search.
	normalized := NormalizeArabic(strings.TrimSpace(query))

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(phone,''), COALESCE(email,''), COALESCE(preferred_channel,''), COALESCE(channel_ids,'')
		 FROM user_contacts
		 WHERE name LIKE ? COLLATE NOCASE
		 ORDER BY name ASC
		 LIMIT 10`,
		"%"+normalized+"%")
	if err != nil {
		return nil, fmt.Errorf("contact.resolve: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.Email, &c.PreferredChannel, &c.ChannelIDs); err != nil {
			return nil, fmt.Errorf("contact.resolve: scan: %w", err)
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

// ResolveOne finds exactly one matching contact, or returns an error.
func (r *Resolver) ResolveOne(ctx context.Context, query string) (*Contact, error) {
	contacts, err := r.Resolve(ctx, query)
	if err != nil {
		return nil, err
	}

	switch len(contacts) {
	case 0:
		return nil, fmt.Errorf("contact: no contact found matching %q", query)
	case 1:
		return &contacts[0], nil
	default:
		names := make([]string, len(contacts))
		for i, c := range contacts {
			names[i] = fmt.Sprintf("%s (id:%d)", c.Name, c.ID)
		}
		return nil, fmt.Errorf("contact: multiple matches for %q: %s", query, strings.Join(names, ", "))
	}
}
