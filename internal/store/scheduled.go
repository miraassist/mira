package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxDeliveryAttempts bounds retries of a scheduled message that cannot be
// delivered.
const MaxDeliveryAttempts = 5

// ScheduleMessage stores a message to be delivered at m.DeliverAt (a
// reminder). Until then it is scheduled and does not count as waiting.
func (s *Store) ScheduleMessage(ctx context.Context, m Message) (Message, error) {
	if m.DeliverAt.IsZero() {
		return Message{}, fmt.Errorf("a scheduled message needs a delivery time")
	}
	m.State = MessageScheduled
	stored, _, err := s.PutMessage(ctx, m)
	return stored, err
}

// ScheduledMessages lists scheduled messages by delivery time; with due set,
// only those due at or before it.
func (s *Store) ScheduledMessages(ctx context.Context, due time.Time) ([]Message, error) {
	q := messageSelect + ` WHERE state = ?`
	args := []any{MessageScheduled}
	if !due.IsZero() {
		q += ` AND deliver_at <= ?`
		args = append(args, formatTime(due))
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY deliver_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CancelScheduled cancels a scheduled message; anything else is ErrNotFound.
func (s *Store) CancelScheduled(ctx context.Context, id string) error {
	return s.moveScheduled(ctx, `UPDATE messages SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		MessageCancelled, formatTime(s.now()), id, MessageScheduled)
}

// ReleaseScheduled makes a due scheduled message pending, with the text it
// is spoken with, so it can be delivered like any other.
func (s *Store) ReleaseScheduled(ctx context.Context, id, spoken string) error {
	if strings.TrimSpace(spoken) == "" {
		return fmt.Errorf("release %s: no text", id)
	}
	return s.moveScheduled(ctx, `UPDATE messages SET state = ?, spoken = ?, updated_at = ? WHERE id = ? AND state = ?`,
		MessagePending, spoken, formatTime(s.now()), id, MessageScheduled)
}

func (s *Store) moveScheduled(ctx context.Context, q string, args ...any) error {
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeliveryFailed counts a failed delivery of a released scheduled message:
// it is scheduled again for the next try, or given up after
// MaxDeliveryAttempts. It reports whether it was given up.
func (s *Store) DeliveryFailed(ctx context.Context, id string) (bool, error) {
	var attempts int
	err := s.db.QueryRowContext(ctx, `
UPDATE messages SET attempts = attempts + 1,
                    state = CASE WHEN attempts + 1 >= ? THEN ? ELSE ? END,
                    updated_at = ?
WHERE id = ? AND state IN (?, ?) RETURNING attempts`,
		MaxDeliveryAttempts, MessageFailed, MessageScheduled, formatTime(s.now()),
		id, MessagePending, MessageScheduled).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return attempts >= MaxDeliveryAttempts, nil
}
