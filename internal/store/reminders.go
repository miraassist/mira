package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Reminder states.
const (
	ReminderPending   = "pending"
	ReminderDone      = "done"
	ReminderCancelled = "cancelled"
	// ReminderFailed is given up after MaxReminderAttempts.
	ReminderFailed = "failed"
)

// MaxReminderAttempts bounds retries of a reminder that cannot be delivered.
const MaxReminderAttempts = 5

// Reminder is something the user asked to be reminded of at a time.
type Reminder struct {
	ID    string
	DueAt time.Time
	Text  string
	// Receiver is "house" or a person id.
	Receiver string
	// AreaID is where it was asked for by voice; it is spoken there.
	AreaID     string
	SessionKey string
	State      string
	Attempts   int
	CreatedAt  time.Time
}

func (s *Store) AddReminder(ctx context.Context, r Reminder) error {
	if r.ID == "" || strings.TrimSpace(r.Text) == "" || r.DueAt.IsZero() {
		return fmt.Errorf("reminder needs id, text and due time")
	}
	if r.Receiver == "" {
		r.Receiver = ReceiverHouse
	}
	now := formatTime(s.now())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO reminders(id, due_at, text, receiver, area_id, session_key, state, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		r.ID, formatTime(r.DueAt), strings.TrimSpace(r.Text), r.Receiver, r.AreaID, r.SessionKey, now, now)
	return err
}

// PendingReminders lists open reminders by due time; with due set, only
// those due at or before it.
func (s *Store) PendingReminders(ctx context.Context, due time.Time) ([]Reminder, error) {
	q := `SELECT id, due_at, text, receiver, area_id, session_key, state, attempts, created_at
FROM reminders WHERE state = 'pending'`
	var args []any
	if !due.IsZero() {
		q += ` AND due_at <= ?`
		args = append(args, formatTime(due))
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY due_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		var r Reminder
		var dueAt, created string
		if err := rows.Scan(&r.ID, &dueAt, &r.Text, &r.Receiver, &r.AreaID, &r.SessionKey, &r.State, &r.Attempts, &created); err != nil {
			return nil, err
		}
		r.DueAt, r.CreatedAt = parseTime(dueAt), parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetReminderState moves a pending reminder on. Only pending reminders
// change; anything else is ErrNotFound.
func (s *Store) SetReminderState(ctx context.Context, id, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE reminders SET state = ?, updated_at = ? WHERE id = ? AND state = 'pending'`,
		state, formatTime(s.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReminderAttemptFailed counts a failed delivery and gives up after
// MaxReminderAttempts. It reports whether the reminder was given up.
func (s *Store) ReminderAttemptFailed(ctx context.Context, id string) (bool, error) {
	var attempts int
	err := s.db.QueryRowContext(ctx, `UPDATE reminders SET attempts = attempts + 1, updated_at = ? WHERE id = ? AND state = 'pending' RETURNING attempts`,
		formatTime(s.now()), id).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if attempts >= MaxReminderAttempts {
		return true, s.SetReminderState(ctx, id, ReminderFailed)
	}
	return false, nil
}
