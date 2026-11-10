package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/miraassist/mira/internal/excerpt"
	"time"
)

// LastRun is how a schedule's job ran last (docs/spec/schedules.md): kept per
// schedule (and person) in kv, each run replacing the one before.
type LastRun struct {
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`
	// Status: done, nothing (nothing to report), failed.
	Status string `json:"status"`
	// Text is what came of it: the spoken text, or Mira's own result.
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

const (
	RunDone    = "done"
	RunNothing = "nothing"
	RunFailed  = "failed"
)

// SetLastRun keeps a job's last run under key.
func (s *Store) SetLastRun(ctx context.Context, key string, r LastRun) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.SetKV(ctx, key, string(raw))
}

// LastRun is the job's last run; ok is false when it has not run yet.
func (s *Store) LastRun(ctx context.Context, key string) (r LastRun, ok bool, err error) {
	raw, err := s.GetKV(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return r, false, fmt.Errorf("%q not understood: %w", excerpt.Of(raw), err)
	}
	return r, true, nil
}

// LastRuns are the last runs kept under keys that begin with prefix, by key
// (a per-person schedule's: one per person).
func (s *Store) LastRuns(ctx context.Context, prefix string) (map[string]LastRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM kv WHERE key >= ? AND key < ?`, prefix, prefix+"\xff")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]LastRun{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var r LastRun
		if json.Unmarshal([]byte(raw), &r) == nil {
			out[key] = r
		}
	}
	return out, rows.Err()
}
