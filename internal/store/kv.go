package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// Keys used by the agent. Kept here so nothing drifts apart.
const (
	KeyQuietMode      = "quiet_mode"
	KeyTelegramOffset = "telegram.offset"
)

func (s *Store) GetKV(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetKV(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO kv(key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, formatTime(s.now()))
	return err
}

// GetBool reads a flag, falling back to def when it was never written.
func (s *Store) GetBool(ctx context.Context, key string, def bool) (bool, error) {
	v, err := s.GetKV(ctx, key)
	if err == ErrNotFound {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	return v == "true" || v == "1", nil
}

func (s *Store) SetBool(ctx context.Context, key string, value bool) error {
	return s.SetKV(ctx, key, strconv.FormatBool(value))
}

func (s *Store) GetInt64(ctx context.Context, key string, def int64) (int64, error) {
	v, err := s.GetKV(ctx, key)
	if err == ErrNotFound {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def, nil
	}
	return n, nil
}

func (s *Store) SetInt64(ctx context.Context, key string, value int64) error {
	return s.SetKV(ctx, key, strconv.FormatInt(value, 10))
}

// Confirmation is a critical tool call waiting for the user to approve it in
// the next turn. The approval must come from the user, never from a tool
// result, which is why this is keyed by session and read only on user input.
type Confirmation struct {
	ID         string
	SessionKey string
	ToolName   string
	Arguments  string
	Summary    string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// PutConfirmation stores a pending confirmation, replacing any earlier one for
// the same session: only the most recent request can be answered with "yes".
func (s *Store) PutConfirmation(ctx context.Context, c Confirmation, ttl time.Duration) error {
	now := s.now()
	c.CreatedAt = now
	c.ExpiresAt = now.Add(ttl)
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM confirmations WHERE session_key = ?`, c.SessionKey); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO confirmations(id, session_key, tool_name, arguments, summary, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.SessionKey, c.ToolName, c.Arguments, c.Summary,
			formatTime(c.CreatedAt), formatTime(c.ExpiresAt))
		return err
	})
}

// TakeConfirmation returns and removes the pending confirmation of a session.
// Taking it is destructive on purpose: one approval authorises one call.
func (s *Store) TakeConfirmation(ctx context.Context, sessionKey string) (Confirmation, error) {
	var c Confirmation
	var created, expires string
	err := s.db.QueryRowContext(ctx, `
SELECT id, session_key, tool_name, arguments, summary, created_at, expires_at
FROM confirmations WHERE session_key = ? AND expires_at > ?
ORDER BY created_at DESC LIMIT 1`, sessionKey, formatTime(s.now())).
		Scan(&c.ID, &c.SessionKey, &c.ToolName, &c.Arguments, &c.Summary, &created, &expires)
	if err == sql.ErrNoRows {
		return Confirmation{}, ErrNotFound
	}
	if err != nil {
		return Confirmation{}, err
	}
	c.CreatedAt, c.ExpiresAt = parseTime(created), parseTime(expires)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM confirmations WHERE id = ?`, c.ID); err != nil {
		return Confirmation{}, err
	}
	return c, nil
}

// ClearConfirmations drops a session's pending confirmation without using it.
func (s *Store) ClearConfirmations(ctx context.Context, sessionKey string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM confirmations WHERE session_key = ?`, sessionKey)
	return err
}
