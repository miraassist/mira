package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// LastRun is how a schedule's job ran last (docs/spec/schedules.md): kept per
// schedule (and person) in the table schedule_runs, each run replacing the
// one before.
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

// RunKey names a schedule's last run: the skill, the schedule's name (or
// its place, #1), and the person of a per-person schedule ("" for the house).
type RunKey struct {
	Skill, Schedule, Person string
}

// SetLastRun keeps a job's last run, replacing the one before.
func (s *Store) SetLastRun(ctx context.Context, k RunKey, r LastRun) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO schedule_runs(skill, schedule, person, started_at, ended_at, status, text, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(skill, schedule, person) DO UPDATE SET started_at = excluded.started_at, ended_at = excluded.ended_at,
    status = excluded.status, text = excluded.text, error = excluded.error`,
		k.Skill, k.Schedule, k.Person, formatTime(r.Started), formatTime(r.Ended), r.Status, r.Text, r.Error)
	return err
}

// LastRun is the job's last run; ok is false when it has not run yet.
func (s *Store) LastRun(ctx context.Context, k RunKey) (r LastRun, ok bool, err error) {
	var started, ended string
	err = s.db.QueryRowContext(ctx, `SELECT started_at, ended_at, status, text, error FROM schedule_runs
WHERE skill = ? AND schedule = ? AND person = ?`, k.Skill, k.Schedule, k.Person).
		Scan(&started, &ended, &r.Status, &r.Text, &r.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.Started, r.Ended = parseTime(started), parseTime(ended)
	return r, true, nil
}

// LastRuns are a schedule's last runs by person ("" for the house; one per
// person for a per-person schedule).
func (s *Store) LastRuns(ctx context.Context, skill, schedule string) (map[string]LastRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT person, started_at, ended_at, status, text, error FROM schedule_runs
WHERE skill = ? AND schedule = ?`, skill, schedule)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]LastRun{}
	for rows.Next() {
		var person, started, ended string
		var r LastRun
		if err := rows.Scan(&person, &started, &ended, &r.Status, &r.Text, &r.Error); err != nil {
			return nil, err
		}
		r.Started, r.Ended = parseTime(started), parseTime(ended)
		out[person] = r
	}
	return out, rows.Err()
}

// PruneLastRuns deletes a per-person schedule's last runs of persons not in
// persons (no longer among its persons); it returns how many went.
func (s *Store) PruneLastRuns(ctx context.Context, skill, schedule string, persons []string) (int64, error) {
	query := `DELETE FROM schedule_runs WHERE skill = ? AND schedule = ? AND person != ''`
	args := []any{skill, schedule}
	if len(persons) > 0 {
		query += ` AND person NOT IN (?` + strings.Repeat(", ?", len(persons)-1) + `)`
		for _, p := range persons {
			args = append(args, p)
		}
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
