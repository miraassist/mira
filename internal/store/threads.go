package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Thread states.
const (
	// ThreadCollecting: the model fills the target's fields.
	ThreadCollecting = "collecting"
	// ThreadWaiting: Mira asked the user and waits for the answer.
	ThreadWaiting = "waiting"
	// ThreadWorking: the target's work runs in the background.
	ThreadWorking = "working"
	// ThreadProposed: a proposal waits for the user's yes.
	ThreadProposed  = "proposed"
	ThreadDone      = "done"
	ThreadCancelled = "cancelled"
	ThreadExpired   = "expired"
)

// ThreadOpen reports whether a thread in this state may still continue.
func ThreadOpen(state string) bool {
	switch state {
	case ThreadDone, ThreadCancelled, ThreadExpired:
		return false
	}
	return true
}

// ThreadEntry is one line of a thread's log.
type ThreadEntry struct {
	At   time.Time `json:"at"`
	Who  string    `json:"who"` // user, mira
	Text string    `json:"text"`
}

// Thread ("Vorgang") is a concern that runs over several turns: planning a
// task with a question back and a yes. Target names its definition (a
// skill), Place is where it happens ("area:<id>" or a chat's session key).
type Thread struct {
	ID        string
	Target    string
	Place     string
	State     string
	Request   string
	Fields    map[string]string
	Log       []ThreadEntry
	Content   json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
}

func (s *Store) PutThread(ctx context.Context, t Thread) error {
	if t.ID == "" || t.Target == "" || t.Place == "" || t.State == "" {
		return errors.New("thread needs id, target, place and state")
	}
	now := s.now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.Fields == nil {
		t.Fields = map[string]string{}
	}
	fields, _ := json.Marshal(t.Fields)
	log, _ := json.Marshal(t.Log)
	if t.Log == nil {
		log = []byte("[]")
	}
	content := t.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO threads(id, target, place, state, request, fields, log, content, created_at, updated_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET state = excluded.state, request = excluded.request, fields = excluded.fields,
	log = excluded.log, content = excluded.content, updated_at = excluded.updated_at, expires_at = excluded.expires_at`,
		t.ID, t.Target, t.Place, t.State, t.Request, string(fields), string(log), string(content),
		formatTime(t.CreatedAt), formatTime(t.UpdatedAt), formatTime(t.ExpiresAt))
	return err
}

const threadSelect = `SELECT id, target, place, state, request, fields, log, content, created_at, updated_at, expires_at FROM threads`

func scanThread(row scannable) (Thread, error) {
	var t Thread
	var fields, log, content, created, updated, expires string
	if err := row.Scan(&t.ID, &t.Target, &t.Place, &t.State, &t.Request, &fields, &log, &content,
		&created, &updated, &expires); err != nil {
		return Thread{}, err
	}
	_ = json.Unmarshal([]byte(fields), &t.Fields)
	_ = json.Unmarshal([]byte(log), &t.Log)
	t.Content = json.RawMessage(content)
	t.CreatedAt, t.UpdatedAt, t.ExpiresAt = parseTime(created), parseTime(updated), parseTime(expires)
	return t, nil
}

func (s *Store) GetThread(ctx context.Context, id string) (Thread, error) {
	t, err := scanThread(s.db.QueryRowContext(ctx, threadSelect+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Thread{}, ErrNotFound
	}
	return t, err
}

// OpenThreads returns the open threads at a place, newest first; all open
// threads when place is empty.
func (s *Store) OpenThreads(ctx context.Context, place string) ([]Thread, error) {
	q := threadSelect + ` WHERE state NOT IN (?, ?, ?)`
	args := []any{ThreadDone, ThreadCancelled, ThreadExpired}
	if place != "" {
		q += ` AND place = ?`
		args = append(args, place)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY updated_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetSessionThread links a turn (session) to a thread.
func (s *Store) SetSessionThread(ctx context.Context, sessionKey, threadID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET thread_id = ? WHERE key = ?`, threadID, sessionKey)
	return err
}
