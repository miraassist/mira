package store

import (
	"context"
	"errors"
	"time"
)

// Assessment kinds.
const (
	AssessmentAssess = "assess"
	AssessmentDecide = "decide"
)

// Assessment is one judgement Mira made for a task: what the model saw (in
// short), what it decided and whether a notice came of it. It is the log
// behind Home Assistant's traces, which only show that mira.assess ran.
type Assessment struct {
	ID string
	// Task is the automation's id, or a key derived from the goal for an
	// automation that did not pass one.
	Task       string
	Kind       string
	Goal       string
	Input      string
	Importance int
	Answer     bool
	Spoken     string
	NoticeID   string
	DryRun     bool
	CreatedAt  time.Time
}

func (s *Store) AddAssessment(ctx context.Context, a Assessment) error {
	if a.ID == "" || a.Task == "" || a.Kind == "" {
		return errors.New("assessment needs id, task and kind")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO assessments(id, task, kind, goal, input, importance, answer, spoken, notice_id, dry_run, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Task, a.Kind, a.Goal, a.Input, a.Importance, a.Answer, a.Spoken, a.NoticeID, a.DryRun, formatTime(a.CreatedAt))
	return err
}

// Assessments returns a task's latest judgements, newest first.
func (s *Store) Assessments(ctx context.Context, task string, limit int) ([]Assessment, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, task, kind, goal, input, importance, answer, spoken, notice_id, dry_run, created_at
FROM assessments WHERE task = ? ORDER BY created_at DESC, id DESC LIMIT ?`, task, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Assessment
	for rows.Next() {
		var a Assessment
		var created string
		if err := rows.Scan(&a.ID, &a.Task, &a.Kind, &a.Goal, &a.Input, &a.Importance, &a.Answer, &a.Spoken,
			&a.NoticeID, &a.DryRun, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = parseTime(created)
		out = append(out, a)
	}
	return out, rows.Err()
}
