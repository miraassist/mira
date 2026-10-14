package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Session is one conversation thread. The key is "<channel>:<id>", built by
// the conversation package.
type Session struct {
	Key            string
	Channel        string
	ConversationID string
	DeviceID       string
	AreaID         string
	Language       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      time.Time
}

// Message is one entry of a session transcript. Roles follow the OpenAI chat
// shape: system, user, assistant, tool.
type Message struct {
	ID         int64
	Seq        int
	Role       string
	Content    string
	ToolCalls  json.RawMessage
	ToolCallID string
	Name       string
	CreatedAt  time.Time
}

// UpsertSession creates the session or refreshes its activity window. The
// transcript is untouched: history is append-only.
func (s *Store) UpsertSession(ctx context.Context, sess Session, ttl time.Duration) (Session, error) {
	now := s.now()
	sess.UpdatedAt = now
	sess.ExpiresAt = now.Add(ttl)
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions(key, channel, conversation_id, device_id, area_id, language, created_at, updated_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    conversation_id = excluded.conversation_id,
    device_id       = excluded.device_id,
    area_id         = excluded.area_id,
    language        = excluded.language,
    updated_at      = excluded.updated_at,
    expires_at      = excluded.expires_at`,
		sess.Key, sess.Channel, sess.ConversationID, sess.DeviceID, sess.AreaID, sess.Language,
		formatTime(sess.CreatedAt), formatTime(sess.UpdatedAt), formatTime(sess.ExpiresAt))
	if err != nil {
		return Session{}, fmt.Errorf("upsert session: %w", err)
	}
	return s.GetSession(ctx, sess.Key)
}

func (s *Store) GetSession(ctx context.Context, key string) (Session, error) {
	var sess Session
	var created, updated, expires string
	err := s.db.QueryRowContext(ctx, `
SELECT key, channel, conversation_id, device_id, area_id, language, created_at, updated_at, expires_at
FROM sessions WHERE key = ?`, key).Scan(
		&sess.Key, &sess.Channel, &sess.ConversationID, &sess.DeviceID, &sess.AreaID, &sess.Language,
		&created, &updated, &expires)
	if err == sql.ErrNoRows {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	sess.CreatedAt, sess.UpdatedAt, sess.ExpiresAt = parseTime(created), parseTime(updated), parseTime(expires)
	return sess, nil
}

// AppendMessages adds turns to the end of a transcript. Sequence numbers are
// assigned here so callers cannot create gaps or collisions.
func (s *Store) AppendMessages(ctx context.Context, key string, msgs ...Message) error {
	if len(msgs) == 0 {
		return nil
	}
	now := s.now()
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var next int
		err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_key = ?`, key).Scan(&next)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO messages(session_key, seq, role, content, tool_calls, tool_call_id, name, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for i, m := range msgs {
			if _, err := stmt.ExecContext(ctx, key, next+i, m.Role, m.Content,
				string(m.ToolCalls), m.ToolCallID, m.Name, formatTime(now)); err != nil {
				return fmt.Errorf("append message %d: %w", i, err)
			}
		}
		return nil
	})
}

// Messages returns the transcript in order.
func (s *Store) Messages(ctx context.Context, key string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, seq, role, content, tool_calls, tool_call_id, name, created_at
FROM messages WHERE session_key = ? ORDER BY seq`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var toolCalls, created string
		if err := rows.Scan(&m.ID, &m.Seq, &m.Role, &m.Content, &toolCalls, &m.ToolCallID, &m.Name, &created); err != nil {
			return nil, err
		}
		if toolCalls != "" {
			m.ToolCalls = json.RawMessage(toolCalls)
		}
		m.CreatedAt = parseTime(created)
		out = append(out, m)
	}
	return out, rows.Err()
}

// TruncateMessagesBefore drops the oldest turns of a session. History
// compaction uses it after folding them into a summary.
func (s *Store) TruncateMessagesBefore(ctx context.Context, key string, seq int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE session_key = ? AND seq < ?`, key, seq)
	return err
}

// PurgeExpiredSessions removes sessions past their TTL together with their
// transcripts. Returns how many sessions went.
func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, formatTime(s.now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
