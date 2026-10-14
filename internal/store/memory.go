package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Fact is one remembered statement about the household or the user.
type Fact struct {
	ID        string
	Text      string
	Tags      []string
	Source    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *Store) SaveFact(ctx context.Context, f Fact) (Fact, error) {
	now := s.now()
	if f.ID == "" {
		return Fact{}, fmt.Errorf("fact needs an id")
	}
	if strings.TrimSpace(f.Text) == "" {
		return Fact{}, fmt.Errorf("fact needs text")
	}
	f.UpdatedAt = now
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	tags := joinTags(f.Tags)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memory_facts(id, text, tags, source, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    text = excluded.text, tags = excluded.tags,
    source = excluded.source, updated_at = excluded.updated_at`,
		f.ID, f.Text, tags, f.Source, formatTime(f.CreatedAt), formatTime(f.UpdatedAt))
	if err != nil {
		return Fact{}, fmt.Errorf("save fact: %w", err)
	}
	return f, nil
}

func (s *Store) DeleteFact(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM memory_facts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SearchFacts runs a full-text query. An empty query returns the most recently
// updated facts, which is what the profile rebuild wants.
func (s *Store) SearchFacts(ctx context.Context, query string, limit int) ([]Fact, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows *sql.Rows
	var err error
	if q := ftsQuery(query); q == "" {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, text, tags, source, created_at, updated_at
FROM memory_facts ORDER BY updated_at DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT f.id, f.text, f.tags, f.source, f.created_at, f.updated_at
FROM memory_facts_fts fts
JOIN memory_facts f ON f.rowid = fts.rowid
WHERE memory_facts_fts MATCH ?
ORDER BY bm25(memory_facts_fts) LIMIT ?`, q, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search facts: %w", err)
	}
	defer rows.Close()
	return scanFacts(rows)
}

// AllFacts returns every fact, oldest first. The nightly profile job uses it.
func (s *Store) AllFacts(ctx context.Context) ([]Fact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, text, tags, source, created_at, updated_at
FROM memory_facts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

func scanFacts(rows *sql.Rows) ([]Fact, error) {
	var out []Fact
	for rows.Next() {
		var f Fact
		var tags, created, updated string
		if err := rows.Scan(&f.ID, &f.Text, &tags, &f.Source, &created, &updated); err != nil {
			return nil, err
		}
		f.Tags = splitTags(tags)
		f.CreatedAt, f.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, f)
	}
	return out, rows.Err()
}

// Profile returns the compacted user profile that forms prompt layer 5.
func (s *Store) Profile(ctx context.Context) (string, time.Time, error) {
	var text, updated string
	err := s.db.QueryRowContext(ctx, `SELECT text, updated_at FROM memory_profile WHERE id = 1`).
		Scan(&text, &updated)
	if err == sql.ErrNoRows {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return text, parseTime(updated), nil
}

func (s *Store) SetProfile(ctx context.Context, text string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memory_profile(id, text, updated_at) VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET text = excluded.text, updated_at = excluded.updated_at`,
		text, formatTime(s.now()))
	return err
}

func joinTags(tags []string) string {
	clean := make([]string, 0, len(tags))
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			clean = append(clean, t)
		}
	}
	return strings.Join(clean, " ")
}

func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

// ftsQuery turns free user text into a safe FTS5 MATCH expression. FTS5 syntax
// characters in user input would otherwise be parse errors, so every word is
// quoted and the words are ORed.
func ftsQuery(q string) string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return !(r == '_' || r == '-' || r >= '0' && r <= '9' ||
			r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127)
	})
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		terms = append(terms, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(terms, " OR ")
}
