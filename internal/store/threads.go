package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
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
	ID     string
	Target string
	Place  string
	// Person is who started it, when known; its results are theirs.
	Person string
	// Area is the satellite's area it began at, for a voice turn: its
	// messages are spoken there.
	Area      string
	State     string
	Request   string
	Fields    map[string]string
	Log       []ThreadEntry
	Content   json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
	// Version counts the changes; PutThread writes only over the version it
	// was read with.
	Version int
	// Calls are the agents' runs filling its fields, by field ("plan" for
	// a plan): their call ids.
	Calls map[string]string
}

// ErrConflict: the thread changed since it was read.
var ErrConflict = errors.New("changed in the meantime")

// PutThread writes a thread, new or changed. A change applies only if the
// thread is still at the version it was read with, else ErrConflict; on
// success t carries the new version.
func (s *Store) PutThread(ctx context.Context, t *Thread) error {
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
	calls, _ := json.Marshal(t.Calls)
	if t.Calls == nil {
		calls = []byte("{}")
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO threads(id, target, place, person, state, request, fields, log, content, created_at, updated_at, expires_at, version, calls, area)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET state = excluded.state, request = excluded.request, fields = excluded.fields,
	log = excluded.log, content = excluded.content, updated_at = excluded.updated_at, expires_at = excluded.expires_at,
	calls = excluded.calls, version = threads.version + 1
WHERE threads.version = excluded.version`,
		t.ID, t.Target, t.Place, t.Person, t.State, t.Request, string(fields), string(log), string(content),
		formatTime(t.CreatedAt), formatTime(t.UpdatedAt), formatTime(t.ExpiresAt), t.Version, string(calls), t.Area)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrConflict
	}
	// A new thread keeps its version, a changed one is one further.
	var version int
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM threads WHERE id = ?`, t.ID).Scan(&version); err != nil {
		return err
	}
	t.Version = version
	return nil
}

const threadSelect = `SELECT id, target, place, person, state, request, fields, log, content, created_at, updated_at, expires_at, version, calls, area FROM threads`

func scanThread(row scannable) (Thread, error) {
	var t Thread
	var fields, log, content, created, updated, expires, calls string
	if err := row.Scan(&t.ID, &t.Target, &t.Place, &t.Person, &t.State, &t.Request, &fields, &log, &content,
		&created, &updated, &expires, &t.Version, &calls, &t.Area); err != nil {
		return Thread{}, err
	}
	_ = json.Unmarshal([]byte(fields), &t.Fields)
	_ = json.Unmarshal([]byte(log), &t.Log)
	_ = json.Unmarshal([]byte(calls), &t.Calls)
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

// RecentThreads returns the threads at a place changed since a time,
// newest first, in any state.
func (s *Store) RecentThreads(ctx context.Context, place string, since time.Time) ([]Thread, error) {
	rows, err := s.db.QueryContext(ctx, threadSelect+` WHERE place = ? AND updated_at >= ? ORDER BY updated_at DESC, id DESC`,
		place, formatTime(since))
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
// AddSessionSkill notes that a conversation opened a skill with an MCP
// server; its tools stay with the session until it expires.
func (s *Store) AddSessionSkill(ctx context.Context, sessionKey, skill string) error {
	open, err := s.SessionSkills(ctx, sessionKey)
	if err != nil || slices.Contains(open, skill) {
		return err
	}
	raw, _ := json.Marshal(append(open, skill))
	_, err = s.db.ExecContext(ctx, `UPDATE sessions SET skills = ? WHERE key = ?`, string(raw), sessionKey)
	return err
}

// SessionSkills are the skills a conversation opened (AddSessionSkill);
// none for a session that does not exist.
func (s *Store) SessionSkills(ctx context.Context, sessionKey string) ([]string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT skills FROM sessions WHERE key = ?`, sessionKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

func (s *Store) SetSessionThread(ctx context.Context, sessionKey, threadID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET thread_id = ? WHERE key = ?`, threadID, sessionKey)
	return err
}
