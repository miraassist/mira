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
	// ThreadID is the thread ("Vorgang") this turn belongs to, if any.
	ThreadID  string
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
}

// ChatMessage is one entry of a session transcript. Roles follow the OpenAI chat
// shape: system, user, assistant, tool.
type ChatMessage struct {
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
	if ttl <= 0 {
		ttl = 100 * 365 * 24 * time.Hour // no time limit (Telegram)
	}
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
SELECT key, channel, conversation_id, device_id, area_id, language, thread_id, created_at, updated_at, expires_at
FROM sessions WHERE key = ?`, key).Scan(
		&sess.Key, &sess.Channel, &sess.ConversationID, &sess.DeviceID, &sess.AreaID, &sess.Language, &sess.ThreadID,
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

// AppendChatMessages adds turns to the end of a transcript. Sequence numbers are
// assigned here so callers cannot create gaps or collisions.
func (s *Store) AppendChatMessages(ctx context.Context, key string, msgs ...ChatMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	now := s.now()
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var next int
		err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(seq), 0) + 1 FROM chat_messages WHERE session_key = ?`, key).Scan(&next)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO chat_messages(session_key, seq, role, content, tool_calls, tool_call_id, name, created_at)
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
func (s *Store) ChatMessages(ctx context.Context, key string) ([]ChatMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, seq, role, content, tool_calls, tool_call_id, name, created_at
FROM chat_messages WHERE session_key = ? ORDER BY seq`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
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

// TruncateChatMessagesBefore drops the oldest turns of a session. History
// compaction uses it after folding them into a summary.
func (s *Store) TruncateChatMessagesBefore(ctx context.Context, key string, seq int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chat_messages WHERE session_key = ? AND seq < ?`, key, seq)
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

// DeleteSession removes a session and its transcript. An expired session is
// reset this way when its channel and id come back.
// ClearSession empties a session and keeps it: its messages, pending
// confirmations, link to a thread and open skills go; the session stays
// known (a Telegram chat after /neu, docs/spec/telegram-history.md,
// decision 6). ErrNotFound when there is none.
func (s *Store) ClearSession(ctx context.Context, key string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chat_messages WHERE session_key = ?`, key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM confirmations WHERE session_key = ?`, key); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE sessions SET thread_id = '', skills = '[]' WHERE key = ?`, key)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) DeleteSession(ctx context.Context, key string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chat_messages WHERE session_key = ?`, key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM confirmations WHERE session_key = ?`, key); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE key = ?`, key)
		return err
	})
}

// LastChatSeq is the sequence number of a session's last message, 0 for
// none.
func (s *Store) LastChatSeq(ctx context.Context, key string) (int, error) {
	var seq int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM chat_messages WHERE session_key = ?`, key).Scan(&seq)
	return seq, err
}

// MoveChatMessages moves a session's messages after seq to the end of
// another session's transcript, in one transaction: all of them or none
// (docs/spec/thread-history.md, a turn that comes into a thread late).
// It returns how many moved.
func (s *Store) MoveChatMessages(ctx context.Context, from, to string, afterSeq int) (int, error) {
	moved := 0
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var next int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM chat_messages WHERE session_key = ?`, to).Scan(&next); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM chat_messages WHERE session_key = ? AND seq > ? ORDER BY seq`, from, afterSeq)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE chat_messages SET session_key = ?, seq = ? WHERE id = ?`, to, next+i, id); err != nil {
				return fmt.Errorf("move message %d: %w", id, err)
			}
		}
		moved = len(ids)
		return nil
	})
	return moved, err
}

// TouchSession makes sure a session exists and extends its activity window,
// without changing what it holds; one that ran out starts over first, as a
// turn would start it (docs/spec/thread-history.md, the summary in an own
// history).
func (s *Store) TouchSession(ctx context.Context, key, channel string, ttl time.Duration) error {
	now := s.now()
	if ttl <= 0 {
		ttl = 100 * 365 * 24 * time.Hour
	}
	if sess, err := s.GetSession(ctx, key); err == nil && sess.ExpiresAt.Before(now) {
		if err := s.DeleteSession(ctx, key); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions(key, channel, created_at, updated_at, expires_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET updated_at = excluded.updated_at, expires_at = excluded.expires_at`,
		key, channel, formatTime(now), formatTime(now), formatTime(now.Add(ttl)))
	return err
}
