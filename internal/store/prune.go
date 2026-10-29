package store

import (
	"context"
	"time"
)

// What is kept of finished things (decided 2026-10-30, review D18): the
// messages' history the page shows, the judgements of tasks; announcements
// matter for a follow-up of minutes; a finished thread for a look back.
const (
	keepMessages      = 90 * 24 * time.Hour
	keepAssessments   = 90 * 24 * time.Hour
	keepAnnouncements = 7 * 24 * time.Hour
	keepThreads       = 30 * 24 * time.Hour
)

// Pruned is how many rows of each kind were removed.
type Pruned struct{ Messages, Assessments, Announcements, Threads int64 }

// PruneFinished removes what has long been finished: messages no longer
// pending or scheduled, judgements, announcements and ended threads, each
// older than it is kept. Nothing open is ever removed.
func (s *Store) PruneFinished(ctx context.Context) (Pruned, error) {
	now := s.now()
	var p Pruned
	steps := []struct {
		n    *int64
		stmt string
		args []any
	}{
		{&p.Messages, `DELETE FROM messages WHERE state NOT IN (?, ?) AND updated_at < ?`,
			[]any{MessagePending, MessageScheduled, formatTime(now.Add(-keepMessages))}},
		{&p.Assessments, `DELETE FROM assessments WHERE created_at < ?`,
			[]any{formatTime(now.Add(-keepAssessments))}},
		{&p.Announcements, `DELETE FROM announcements WHERE created_at < ?`,
			[]any{formatTime(now.Add(-keepAnnouncements))}},
		{&p.Threads, `DELETE FROM threads WHERE state IN (?, ?, ?) AND updated_at < ?`,
			[]any{ThreadDone, ThreadCancelled, ThreadExpired, formatTime(now.Add(-keepThreads))}},
	}
	for _, st := range steps {
		res, err := s.db.ExecContext(ctx, st.stmt, st.args...)
		if err != nil {
			return p, err
		}
		*st.n, _ = res.RowsAffected()
	}
	return p, nil
}
