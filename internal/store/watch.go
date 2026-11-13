package store

import (
	"context"
	"time"
)

// The devices whose watch the user declined, per blueprint
// (docs/spec/device-watch.md, decision 21): they are not offered again.

// WatchDecline is one declined watch: a device's entity and the blueprint
// that would have watched it.
type WatchDecline struct {
	EntityID, Blueprint string
	DeclinedAt          time.Time
}

// DeclineWatch remembers that the user declined the watch of an entity by
// a blueprint; declining it again keeps one row with the later time.
func (s *Store) DeclineWatch(ctx context.Context, entityID, blueprint string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO watch_declined(entity_id, blueprint, declined_at) VALUES (?, ?, ?)
ON CONFLICT(entity_id, blueprint) DO UPDATE SET declined_at = excluded.declined_at`,
		entityID, blueprint, formatTime(s.now()))
	return err
}

// WatchDeclines are all declined watches.
func (s *Store) WatchDeclines(ctx context.Context) ([]WatchDecline, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entity_id, blueprint, declined_at FROM watch_declined ORDER BY entity_id, blueprint`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchDecline
	for rows.Next() {
		var d WatchDecline
		var at string
		if err := rows.Scan(&d.EntityID, &d.Blueprint, &at); err != nil {
			return nil, err
		}
		d.DeclinedAt = parseTime(at)
		out = append(out, d)
	}
	return out, rows.Err()
}
