package store

import (
	"context"
	"database/sql"
	"errors"
)

// The automations Mira created herself, her tasks and timers, by their id in
// Home Assistant: she deletes only these (docs/spec/hacs-default.md,
// decision 7). The labels in Home Assistant mark them; they permit nothing.

// keyOwnTakenOver is set once the automations labelled as Mira's before the
// table existed were taken over.
const keyOwnTakenOver = "own_automations.taken_over"

// RecordOwnAutomation notes an automation Mira created; kind is "task" or
// "once". Recording one again keeps the first.
func (s *Store) RecordOwnAutomation(ctx context.Context, id, kind string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO own_automations(id, kind, created_at) VALUES (?, ?, ?) ON CONFLICT(id) DO NOTHING`,
		id, kind, s.now().Format(timeFormat))
	return err
}

// IsOwnAutomation tells whether Mira created the automation.
func (s *Store) IsOwnAutomation(ctx context.Context, id string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM own_automations WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ForgetOwnAutomation drops the note of a deleted automation.
func (s *Store) ForgetOwnAutomation(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM own_automations WHERE id = ?`, id)
	return err
}

// OwnAutomationsTakenOver tells whether the automations from before the
// table were taken over.
func (s *Store) OwnAutomationsTakenOver(ctx context.Context) (bool, error) {
	_, err := s.GetKV(ctx, keyOwnTakenOver)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// TakeOverOwnAutomations records the automations labelled as Mira's before
// the table existed (id → kind), once: all of them and the mark that it is
// done, in one transaction.
func (s *Store) TakeOverOwnAutomations(ctx context.Context, ids map[string]string) error {
	now := s.now().Format(timeFormat)
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for id, kind := range ids {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO own_automations(id, kind, created_at) VALUES (?, ?, ?) ON CONFLICT(id) DO NOTHING`,
				id, kind, now); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO kv(key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			keyOwnTakenOver, now, now)
		return err
	})
}
