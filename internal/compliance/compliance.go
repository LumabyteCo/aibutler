// Package compliance provides audit logging and GDPR compliance tooling for AI Butler.
package compliance

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// AuditEntry represents a single compliance audit log row.
type AuditEntry struct {
	ID        int64
	Timestamp time.Time
	UserID    string
	Action    string
	Resource  string
	Details   string // JSON
	IPAddress string
	Outcome   string // "success", "denied", "error"
}

// AuditFilter controls which audit entries are returned by Query.
type AuditFilter struct {
	UserID   string
	Action   string
	Resource string
	Since    time.Time
	Until    time.Time
	Limit    int
}

// Logger provides compliance audit logging backed by SQLite.
type Logger struct {
	db *sql.DB
}

// New creates a new compliance Logger.
func New(db *sql.DB) *Logger {
	return &Logger{db: db}
}

// Log writes a single audit entry.
func (l *Logger) Log(ctx context.Context, userID, action, resource, details, ip, outcome string) error {
	_, err := l.db.ExecContext(ctx,
		`INSERT INTO compliance_audit (user_id, action, resource, details, ip_address, outcome)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, action, resource, details, ip, outcome)
	if err != nil {
		return fmt.Errorf("compliance: log: %w", err)
	}
	return nil
}

// Query returns audit entries matching the given filter.
func (l *Logger) Query(ctx context.Context, filter AuditFilter) ([]AuditEntry, error) {
	query := `SELECT id, timestamp, user_id, action, resource, details, ip_address, outcome
	          FROM compliance_audit WHERE 1=1`
	var args []interface{}

	if filter.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, filter.UserID)
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, filter.Action)
	}
	if filter.Resource != "" {
		query += " AND resource = ?"
		args = append(args, filter.Resource)
	}
	if !filter.Since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.Since.Format(time.DateTime))
	}
	if !filter.Until.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.Until.Format(time.DateTime))
	}
	query += " ORDER BY timestamp DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("compliance: query: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var details, ip sql.NullString
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.UserID, &e.Action, &e.Resource,
			&details, &ip, &e.Outcome); err != nil {
			return nil, fmt.Errorf("compliance: scan: %w", err)
		}
		e.Details = details.String
		e.IPAddress = ip.String
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Export writes audit entries matching the filter to w in the given format ("json" or "csv").
func (l *Logger) Export(ctx context.Context, format string, w io.Writer) error {
	entries, err := l.Query(ctx, AuditFilter{})
	if err != nil {
		return err
	}

	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	case "csv":
		cw := csv.NewWriter(w)
		defer cw.Flush()
		if err := cw.Write([]string{"id", "timestamp", "user_id", "action", "resource", "details", "ip_address", "outcome"}); err != nil {
			return err
		}
		for _, e := range entries {
			if err := cw.Write([]string{
				fmt.Sprintf("%d", e.ID),
				e.Timestamp.Format(time.RFC3339),
				e.UserID,
				e.Action,
				e.Resource,
				e.Details,
				e.IPAddress,
				e.Outcome,
			}); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("compliance: unsupported export format %q", format)
	}
}

// DeleteUserData removes all audit entries for a given user (GDPR right-to-erasure).
func (l *Logger) DeleteUserData(ctx context.Context, userID string) error {
	_, err := l.db.ExecContext(ctx,
		`DELETE FROM compliance_audit WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("compliance: delete user data: %w", err)
	}
	return nil
}

// PII redaction patterns.
var (
	emailRe  = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	phoneRe  = regexp.MustCompile(`\+?\d[\d\s\-]{7,}\d`)
	apiKeyRe = regexp.MustCompile(`(sk-ant-[a-zA-Z0-9_-]{3,})`)
	bearerRe = regexp.MustCompile(`Bearer\s+[a-zA-Z0-9._\-]+`)
)

// RedactPII masks PII in text: emails, phone numbers, API keys, and bearer tokens.
func (l *Logger) RedactPII(text string) string {
	text = emailRe.ReplaceAllStringFunc(text, redactEmail)
	text = phoneRe.ReplaceAllStringFunc(text, redactPhone)
	text = apiKeyRe.ReplaceAllString(text, "${1}***")
	// Re-apply api key redaction more precisely
	text = apiKeyRe.ReplaceAllStringFunc(text, func(s string) string {
		if len(s) > 7 {
			return s[:7] + "***"
		}
		return s + "***"
	})
	text = bearerRe.ReplaceAllString(text, "Bearer [REDACTED]")
	return text
}

func redactEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	user := parts[0]
	domain := parts[1]
	domainParts := strings.SplitN(domain, ".", 2)
	domainName := domainParts[0]

	uMask := string(user[0]) + "***"
	dMask := string(domainName[0]) + "***"
	if len(domainParts) == 2 {
		dMask += "." + domainParts[1]
	}
	return uMask + "@" + dMask
}

func redactPhone(phone string) string {
	digits := ""
	for _, c := range phone {
		if c >= '0' && c <= '9' {
			digits += string(c)
		}
	}
	if len(digits) < 4 {
		return phone
	}
	prefix := ""
	if strings.HasPrefix(phone, "+") {
		prefix = "+"
	}
	return prefix + digits[:1] + "***" + digits[len(digits)-3:]
}

// RetentionPurge deletes audit entries older than the given duration.
// Returns the number of entries deleted.
func (l *Logger) RetentionPurge(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan).Format(time.DateTime)
	res, err := l.db.ExecContext(ctx,
		`DELETE FROM compliance_audit WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("compliance: retention purge: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
