package store

import (
	"context"
	"database/sql"
)

// GetOAuthToken is a skill's stored connection, as written; ErrNotFound for
// none.
func (s *Store) GetOAuthToken(ctx context.Context, skill string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT token FROM oauth_tokens WHERE skill = ?`, skill).Scan(&v)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetOAuthToken(ctx context.Context, skill, token string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_tokens(skill, token, updated_at) VALUES (?, ?, ?)
ON CONFLICT(skill) DO UPDATE SET token = excluded.token, updated_at = excluded.updated_at`,
		skill, token, formatTime(s.now()))
	return err
}

func (s *Store) DeleteOAuthToken(ctx context.Context, skill string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE skill = ?`, skill)
	return err
}
