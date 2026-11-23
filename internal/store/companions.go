package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Companion is a registered companion service (docs/spec/companions.md).
type Companion struct {
	Name    string
	Title   string
	URL     string
	Token   string // Mira passes its page's calls on with it
	TokenID string // the access token that registered it
	Updated time.Time
}

// ErrCompanionTaken is a name registered by another access token (decision 6).
var ErrCompanionTaken = errors.New("companion registered by another token")

// PutCompanion keeps a registration; a name another token registered is ErrCompanionTaken.
func (s *Store) PutCompanion(ctx context.Context, c Companion) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var owner string
		err := tx.QueryRowContext(ctx, `SELECT token_id FROM companions WHERE name = ?`, c.Name).Scan(&owner)
		switch {
		case err == nil && owner != c.TokenID:
			return ErrCompanionTaken
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return err
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO companions(name, title, url, token, token_id, updated_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET title = excluded.title, url = excluded.url, token = excluded.token,
    token_id = excluded.token_id, updated_at = excluded.updated_at`,
			c.Name, c.Title, c.URL, c.Token, c.TokenID, s.now().Format(timeFormat))
		return err
	})
}

// DeleteCompanion removes a registration of that token; false when there was none of it.
func (s *Store) DeleteCompanion(ctx context.Context, name, tokenID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM companions WHERE name = ? AND token_id = ?`, name, tokenID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Companions are the registered companions, by name.
func (s *Store) Companions(ctx context.Context) ([]Companion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, title, url, token, token_id, updated_at FROM companions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Companion
	for rows.Next() {
		var c Companion
		var at string
		if err := rows.Scan(&c.Name, &c.Title, &c.URL, &c.Token, &c.TokenID, &at); err != nil {
			return nil, err
		}
		c.Updated, _ = time.Parse(timeFormat, at)
		out = append(out, c)
	}
	return out, rows.Err()
}
