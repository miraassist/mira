package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// OwnerHouse owns the household's memory; every other owner is a person key.
const OwnerHouse = "house"

// Fact is one remembered statement about the household or a person.
type Fact struct {
	ID   string
	Text string
	Tags []string
	// Owner is OwnerHouse or a person key: whose memory this is.
	Owner string
	// Author is who said it: a person key, or "area:<id>" for someone
	// unknown at a satellite; empty when unknown.
	Author    string
	Source    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Owners is what a person may read: the house and, when known, their own.
func Owners(person string) []string {
	if person == "" || person == OwnerHouse {
		return []string{OwnerHouse}
	}
	return []string{OwnerHouse, person}
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
	if f.Owner == "" {
		f.Owner = OwnerHouse
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	tags := joinTags(f.Tags)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memory_facts(id, text, tags, owner, author, source, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    text = excluded.text, tags = excluded.tags, owner = excluded.owner,
    author = excluded.author, source = excluded.source, updated_at = excluded.updated_at`,
		f.ID, f.Text, tags, f.Owner, f.Author, f.Source, formatTime(f.CreatedAt), formatTime(f.UpdatedAt))
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

const factColumns = `f.id, f.text, f.tags, f.owner, f.author, f.source, f.created_at, f.updated_at`

// ownerClause limits a query to some owners; none means every owner.
func ownerClause(owners []string) (string, []any) {
	if len(owners) == 0 {
		return "1", nil
	}
	args := make([]any, len(owners))
	for i, o := range owners {
		args[i] = o
	}
	return "f.owner IN (?" + strings.Repeat(",?", len(owners)-1) + ")", args
}

// SearchFacts runs a full-text query over the memories of these owners (all
// when none). An empty query returns the most recently updated facts.
func (s *Store) SearchFacts(ctx context.Context, query string, owners []string, limit int) ([]Fact, error) {
	if limit <= 0 {
		limit = 10
	}
	where, args := ownerClause(owners)
	var rows *sql.Rows
	var err error
	if q := ftsQuery(query); q == "" {
		rows, err = s.db.QueryContext(ctx, `
SELECT `+factColumns+`
FROM memory_facts f WHERE `+where+` ORDER BY f.updated_at DESC LIMIT ?`, append(args, limit)...)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT `+factColumns+`
FROM memory_facts_fts fts
JOIN memory_facts f ON f.rowid = fts.rowid
WHERE memory_facts_fts MATCH ? AND `+where+`
ORDER BY bm25(memory_facts_fts) LIMIT ?`, append(append([]any{q}, args...), limit)...)
	}
	if err != nil {
		return nil, fmt.Errorf("search facts: %w", err)
	}
	defer rows.Close()
	return scanFacts(rows)
}

// AllFacts returns the facts of these owners (all when none), oldest first.
func (s *Store) AllFacts(ctx context.Context, owners ...string) ([]Fact, error) {
	where, args := ownerClause(owners)
	rows, err := s.db.QueryContext(ctx, `
SELECT `+factColumns+`
FROM memory_facts f WHERE `+where+` ORDER BY f.created_at`, args...)
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
		if err := rows.Scan(&f.ID, &f.Text, &tags, &f.Owner, &f.Author, &f.Source, &created, &updated); err != nil {
			return nil, err
		}
		f.Tags = splitTags(tags)
		f.CreatedAt, f.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFact returns one fact.
func (s *Store) GetFact(ctx context.Context, id string) (Fact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+factColumns+` FROM memory_facts f WHERE f.id = ?`, id)
	if err != nil {
		return Fact{}, err
	}
	defer rows.Close()
	facts, err := scanFacts(rows)
	if err != nil {
		return Fact{}, err
	}
	if len(facts) == 0 {
		return Fact{}, ErrNotFound
	}
	return facts[0], nil
}

// FactOwners lists every owner that has facts, the house first.
func (s *Store) FactOwners(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT owner FROM memory_facts ORDER BY owner != 'house', owner`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Profile returns an owner's compacted profile. The house's forms prompt
// layer 5, a person's goes into their turns.
func (s *Store) Profile(ctx context.Context, owner string) (string, time.Time, error) {
	var text, updated string
	err := s.db.QueryRowContext(ctx, `SELECT text, updated_at FROM memory_profiles WHERE owner = ?`, owner).
		Scan(&text, &updated)
	if err == sql.ErrNoRows {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return text, parseTime(updated), nil
}

func (s *Store) SetProfile(ctx context.Context, owner, text string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memory_profiles(owner, text, updated_at) VALUES (?, ?, ?)
ON CONFLICT(owner) DO UPDATE SET text = excluded.text, updated_at = excluded.updated_at`,
		owner, text, formatTime(s.now()))
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
// quoted and the words are ORed. The index is by trigrams, so a word matches
// as part of a longer one; a German ending is taken off first, so an
// inflected form finds its base ("Kalenders" finds "Kalender", "Pilze"
// "Pilz"). Words shorter than three letters cannot match trigrams and are
// left out.
func ftsQuery(q string) string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return !(r == '_' || r == '-' || r >= '0' && r <= '9' ||
			r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127)
	})
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		f = stem(strings.ToLower(f))
		if utf8.RuneCountInString(f) < 3 {
			continue
		}
		terms = append(terms, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(terms, " OR ")
}

// germanEndings are the endings stem takes off, longest first.
var germanEndings = []string{"ern", "en", "er", "es", "e", "n", "s"}

// stem takes one German ending off a word that keeps at least four letters
// without it. It is no real stemmer: with the trigram index a shorter word
// only has to be part of the stored one.
func stem(w string) string {
	n := utf8.RuneCountInString(w)
	for _, e := range germanEndings {
		if strings.HasSuffix(w, e) && n-len(e) >= 4 {
			return strings.TrimSuffix(w, e)
		}
	}
	return w
}
