package memory

import (
	"context"
	"fmt"
	"time"
)

// Transcript represents a single turn in a session.
type Transcript struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	TurnNumber int    `json:"turn_number"`
	CreatedAt  string `json:"created_at"`
}

// SaveTranscript persists a conversation turn for cross-session recall.
func (s *Store) SaveTranscript(ctx context.Context, sessionID, role, content string, turnNumber int) (int64, error) {
	if content == "" {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES (?, ?, ?, ?, ?)`,
		sessionID, role, content, turnNumber, now)
	if err != nil {
		return 0, fmt.Errorf("memory.save_transcript: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	s.indexAsync(ctx, "transcript", id, content)
	return id, nil
}

// GetTranscripts retrieves transcripts for a session, ordered by turn number.
func (s *Store) GetTranscripts(ctx context.Context, sessionID string, limit int) ([]Transcript, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, role, content, turn_number, created_at
		 FROM session_transcripts
		 WHERE session_id = ?
		 ORDER BY turn_number ASC
		 LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("memory.get_transcripts: %w", err)
	}
	defer rows.Close()

	var transcripts []Transcript
	for rows.Next() {
		var t Transcript
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Role, &t.Content, &t.TurnNumber, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("memory.get_transcripts: scan: %w", err)
		}
		transcripts = append(transcripts, t)
	}
	return transcripts, rows.Err()
}

// NextTurnNumber returns the next available turn number for a session.
// This avoids turn-number collisions across multiple runs in the same session.
func (s *Store) NextTurnNumber(ctx context.Context, sessionID string) int {
	var maxTurn int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(turn_number), -1) FROM session_transcripts WHERE session_id = ?`, sessionID).Scan(&maxTurn)
	if err != nil {
		return 0
	}
	return maxTurn + 1
}

// TranscriptCount returns the total number of indexed transcripts.
func (s *Store) TranscriptCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_transcripts`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("memory.transcript_count: %w", err)
	}
	return count, nil
}
