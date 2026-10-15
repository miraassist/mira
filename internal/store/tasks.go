package store

import (
	"context"

	"errors"
	"strings"
	"time"
)

// Task states. A task is proposed until the user confirms its plan.
const (
	TaskProposed = "proposed"
	TaskActive   = "active"
	// TaskPaused is not checked until it is resumed (made active again).
	TaskPaused = "paused"
)

// Task is a standing order ("Achte auf den Wasserverbrauch"): the goal in
// the user's words and the plan the model made for it (JSON, see package
// tasks).
type Task struct {
	ID         string
	Goal       string
	Title      string
	Summary    string
	Plan       string
	State      string
	SessionKey string
	// Origin is where the task was asked for: "area:<id>" for a voice
	// satellite, else the session key (a Telegram chat). A yes in the same
	// place confirms the newest proposal there.
	Origin string
	// LastAlert is when the task last raised a notice, per rule, as JSON.
	LastAlert string
	CreatedAt time.Time
}

func (s *Store) PutTask(ctx context.Context, t Task) error {
	if t.ID == "" || strings.TrimSpace(t.Goal) == "" || t.Plan == "" {
		return errors.New("task needs id, goal and plan")
	}
	if t.State == "" {
		t.State = TaskProposed
	}
	now := formatTime(s.now())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks(id, goal, title, summary, plan, state, session_key, origin, last_alert, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET goal = excluded.goal, title = excluded.title, summary = excluded.summary,
	plan = excluded.plan, state = excluded.state, last_alert = excluded.last_alert, updated_at = excluded.updated_at`,
		t.ID, t.Goal, t.Title, t.Summary, t.Plan, t.State, t.SessionKey, t.Origin, t.LastAlert, now, now)
	return err
}

func (s *Store) Tasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, goal, title, summary, plan, state, session_key, origin, last_alert, created_at FROM tasks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		var t Task
		var created string
		if err := rows.Scan(&t.ID, &t.Goal, &t.Title, &t.Summary, &t.Plan, &t.State, &t.SessionKey, &t.Origin, &t.LastAlert, &created); err != nil {
			return nil, err
		}
		t.CreatedAt = parseTime(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	list, err := s.Tasks(ctx)
	if err != nil {
		return Task{}, err
	}
	for _, t := range list {
		if t.ID == id {
			return t, nil
		}
	}
	return Task{}, ErrNotFound
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
