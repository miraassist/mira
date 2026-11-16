package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// An access token beside the integration's (docs/spec/access-tokens.md):
// only its hash and first characters are kept.
type AccessToken struct {
	ID      string
	Name    string
	Kind    string
	Hash    string
	Prefix  string
	Created time.Time
}

// ErrNameTaken is returned when an access token of that name exists.
var ErrNameTaken = errors.New("name taken")

// AddAccessToken stores a token's hash under a new name; ErrNameTaken for a
// name in use.
func (s *Store) AddAccessToken(ctx context.Context, name, kind, hash, prefix string) (AccessToken, error) {
	t := AccessToken{ID: strings.ToLower(ulid.Make().String()), Name: name, Kind: kind, Hash: hash, Prefix: prefix,
		Created: s.now()}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM access_tokens WHERE name = ?`, name).Scan(&n); err != nil {
		return AccessToken{}, err
	}
	if n > 0 {
		return AccessToken{}, ErrNameTaken
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO access_tokens(id, name, kind, hash, prefix, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Kind, t.Hash, t.Prefix, formatTime(t.Created))
	return t, err
}

// AccessTokens lists the stored tokens, oldest first.
func (s *Store) AccessTokens(ctx context.Context) ([]AccessToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, kind, hash, prefix, created_at FROM access_tokens ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessToken
	for rows.Next() {
		var t AccessToken
		var created string
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Hash, &t.Prefix, &created); err != nil {
			return nil, err
		}
		t.Created = parseTime(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAccessToken removes a token; ErrNotFound for an unknown id. Its
// activity stays until it ages out.
func (s *Store) DeleteAccessToken(ctx context.Context, id string) (AccessToken, error) {
	var t AccessToken
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, kind, hash, prefix, created_at FROM access_tokens WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Kind, &t.Hash, &t.Prefix, &created)
	if err == sql.ErrNoRows {
		return AccessToken{}, ErrNotFound
	}
	if err != nil {
		return AccessToken{}, err
	}
	t.Created = parseTime(created)
	_, err = s.db.ExecContext(ctx, `DELETE FROM access_tokens WHERE id = ?`, id)
	return t, err
}

// AccessCall is one call of a partner token.
type AccessCall struct {
	TokenID     string    `json:"-"`
	TokenName   string    `json:"token"`
	PartnerUser string    `json:"partner_user"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	Status      int       `json:"status"`
	At          time.Time `json:"at"`
}

// RecordAccessCall keeps one call of a partner token.
func (s *Store) RecordAccessCall(ctx context.Context, c AccessCall) error {
	if c.At.IsZero() {
		c.At = s.now()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO access_log(token_id, token_name, partner_user, method, path, status, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.TokenID, c.TokenName, c.PartnerUser, c.Method, c.Path, c.Status, formatTime(c.At))
	return err
}

// AccessCalls lists a token's calls, newest first, a page of limit from offset.
func (s *Store) AccessCalls(ctx context.Context, tokenID string, limit, offset int) ([]AccessCall, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT token_id, token_name, partner_user, method, path, status, at FROM access_log
WHERE token_id = ? ORDER BY at DESC, id DESC LIMIT ? OFFSET ?`, tokenID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessCall
	for rows.Next() {
		var c AccessCall
		var at string
		if err := rows.Scan(&c.TokenID, &c.TokenName, &c.PartnerUser, &c.Method, &c.Path, &c.Status, &at); err != nil {
			return nil, err
		}
		c.At = parseTime(at)
		out = append(out, c)
	}
	return out, rows.Err()
}
