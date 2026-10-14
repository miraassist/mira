package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Notice states.
const (
	NoticePending   = "pending"
	NoticeDelivered = "delivered"
	NoticeAcked     = "acked"
	NoticeExpired   = "expired"
)

// Notice sources.
const (
	SourceHATrigger = "ha_trigger"
	SourceSchedule  = "schedule"
	SourceExternal  = "external"
)

// AudienceHousehold means anyone present may hear the notice.
const AudienceHousehold = "household"

// Confidentiality levels. A normal notice may be spoken to whoever is in the
// room; a confidential one only when the notifier's policy says the right
// person is the one listening, otherwise it goes to a private channel.
const (
	ConfidentialityNormal       = "normal"
	ConfidentialityConfidential = "confidential"
)

type Notice struct {
	ID           string
	Source       string
	DedupeKey    string
	Importance   int
	Audience     string
	Spoken       string
	Details      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ExpiresAt    time.Time
	State        string
	DeliveredVia string
	DeliveredAt  time.Time
	// Confidentiality is ConfidentialityNormal or ConfidentialityConfidential.
	Confidentiality string
}

// Confidential reports whether the notice must not be spoken to an unknown
// listener.
func (n Notice) Confidential() bool { return n.Confidentiality == ConfidentialityConfidential }

// PutNotice inserts a notice, or updates the open notice with the same dedupe
// key instead of creating a duplicate. A sensor that flaps therefore produces
// one row, not many. Returns the stored notice and whether it was new.
func (s *Store) PutNotice(ctx context.Context, n Notice) (Notice, bool, error) {
	if n.ID == "" {
		return Notice{}, false, fmt.Errorf("notice needs an id")
	}
	if strings.TrimSpace(n.Spoken) == "" {
		return Notice{}, false, fmt.Errorf("notice needs spoken text")
	}
	if n.State == "" {
		n.State = NoticePending
	}
	if n.Audience == "" {
		n.Audience = AudienceHousehold
	}
	switch n.Confidentiality {
	case "":
		n.Confidentiality = ConfidentialityNormal
	case ConfidentialityNormal, ConfidentialityConfidential:
	default:
		return Notice{}, false, fmt.Errorf("unknown confidentiality %q", n.Confidentiality)
	}
	now := s.now()
	n.UpdatedAt = now
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}

	var created bool
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if n.DedupeKey != "" {
			var existing string
			err := tx.QueryRowContext(ctx, `
SELECT id FROM notices
WHERE dedupe_key = ? AND state IN (?, ?)`, n.DedupeKey, NoticePending, NoticeDelivered).Scan(&existing)
			switch {
			case err == nil:
				// Refresh content and importance, but keep the original id and
				// creation time so the follow-up link stays valid. Re-opening a
				// delivered notice would announce it twice, so the state stays.
				_, err = tx.ExecContext(ctx, `
UPDATE notices SET source = ?, importance = ?, audience = ?, spoken = ?, details = ?,
                   updated_at = ?, expires_at = ?, confidentiality = ?
WHERE id = ?`, n.Source, n.Importance, n.Audience, n.Spoken, n.Details,
					formatTime(n.UpdatedAt), formatTime(n.ExpiresAt), n.Confidentiality, existing)
				if err != nil {
					return err
				}
				n.ID = existing
				return nil
			case err != sql.ErrNoRows:
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO notices(id, source, dedupe_key, importance, audience, spoken, details,
                    created_at, updated_at, expires_at, state, delivered_via, delivered_at, confidentiality)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?)`,
			n.ID, n.Source, n.DedupeKey, n.Importance, n.Audience, n.Spoken, n.Details,
			formatTime(n.CreatedAt), formatTime(n.UpdatedAt), formatTime(n.ExpiresAt), n.State, n.Confidentiality)
		if err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return Notice{}, false, fmt.Errorf("put notice: %w", err)
	}
	stored, err := s.GetNotice(ctx, n.ID)
	return stored, created, err
}

func (s *Store) GetNotice(ctx context.Context, id string) (Notice, error) {
	row := s.db.QueryRowContext(ctx, noticeSelect+` WHERE id = ?`, id)
	n, err := scanNotice(row)
	if err == sql.ErrNoRows {
		return Notice{}, ErrNotFound
	}
	return n, err
}

const noticeSelect = `
SELECT id, source, dedupe_key, importance, audience, spoken, details,
       created_at, updated_at, expires_at, state, delivered_via, delivered_at, confidentiality
FROM notices`

type scannable interface{ Scan(...any) error }

func scanNotice(row scannable) (Notice, error) {
	var n Notice
	var created, updated, expires, delivered string
	err := row.Scan(&n.ID, &n.Source, &n.DedupeKey, &n.Importance, &n.Audience, &n.Spoken, &n.Details,
		&created, &updated, &expires, &n.State, &n.DeliveredVia, &delivered, &n.Confidentiality)
	if err != nil {
		return Notice{}, err
	}
	n.CreatedAt, n.UpdatedAt = parseTime(created), parseTime(updated)
	n.ExpiresAt, n.DeliveredAt = parseTime(expires), parseTime(delivered)
	return n, nil
}

// NoticeFilter selects notices for listing.
type NoticeFilter struct {
	States        []string
	MinImportance int
	Audience      string
	// IncludeExpired keeps notices whose expires_at has passed.
	IncludeExpired bool
	Limit          int
}

// ListNotices returns matching notices, most important first and oldest first
// within the same importance. That is the order announcements are built in.
func (s *Store) ListNotices(ctx context.Context, f NoticeFilter) ([]Notice, error) {
	q := noticeSelect + ` WHERE importance >= ?`
	args := []any{f.MinImportance}
	if len(f.States) > 0 {
		q += ` AND state IN (` + placeholders(len(f.States)) + `)`
		for _, st := range f.States {
			args = append(args, st)
		}
	}
	if f.Audience != "" {
		q += ` AND audience = ?`
		args = append(args, f.Audience)
	}
	if !f.IncludeExpired {
		q += ` AND (expires_at = '' OR expires_at > ?)`
		args = append(args, formatTime(s.now()))
	}
	q += ` ORDER BY importance DESC, created_at`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list notices: %w", err)
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		n, err := scanNotice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkDelivered records that a notice went out over a channel.
func (s *Store) MarkDelivered(ctx context.Context, id, via string) error {
	now := formatTime(s.now())
	res, err := s.db.ExecContext(ctx, `
UPDATE notices SET state = ?, delivered_via = ?, delivered_at = ?, updated_at = ?
WHERE id = ? AND state = ?`, NoticeDelivered, via, now, now, id, NoticePending)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNoticeState moves a notice to acked or expired.
func (s *Store) SetNoticeState(ctx context.Context, id, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE notices SET state = ?, updated_at = ? WHERE id = ?`,
		state, formatTime(s.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ExpireNotices moves every past-due pending or delivered notice to expired.
func (s *Store) ExpireNotices(ctx context.Context) (int64, error) {
	now := formatTime(s.now())
	res, err := s.db.ExecContext(ctx, `
UPDATE notices SET state = ?, updated_at = ?
WHERE state IN (?, ?) AND expires_at <> '' AND expires_at <= ?`,
		NoticeExpired, now, NoticePending, NoticeDelivered, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) CountPendingNotices(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM notices
WHERE state = ? AND (expires_at = '' OR expires_at > ?)`,
		NoticePending, formatTime(s.now())).Scan(&n)
	return n, err
}

// Announcement records what was spoken where, so a follow-up question from the
// same satellite can be linked back to the notices it is about.
type Announcement struct {
	ID        string
	DeviceID  string
	AreaID    string
	Channel   string
	NoticeIDs []string
	CreatedAt time.Time
}

func (s *Store) RecordAnnouncement(ctx context.Context, a Announcement) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO announcements(id, device_id, area_id, channel, notice_ids, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.DeviceID, a.AreaID, a.Channel, strings.Join(a.NoticeIDs, ","), formatTime(a.CreatedAt))
	return err
}

// RecentAnnouncement returns the last announcement to a device if it happened
// within the window, which is how the notifier links a reply to its notices.
func (s *Store) RecentAnnouncement(ctx context.Context, deviceID string, window time.Duration) (Announcement, error) {
	if deviceID == "" {
		return Announcement{}, ErrNotFound
	}
	cutoff := formatTime(s.now().Add(-window))
	var a Announcement
	var ids, created string
	err := s.db.QueryRowContext(ctx, `
SELECT id, device_id, area_id, channel, notice_ids, created_at
FROM announcements
WHERE device_id = ? AND created_at >= ?
ORDER BY created_at DESC LIMIT 1`, deviceID, cutoff).
		Scan(&a.ID, &a.DeviceID, &a.AreaID, &a.Channel, &ids, &created)
	if err == sql.ErrNoRows {
		return Announcement{}, ErrNotFound
	}
	if err != nil {
		return Announcement{}, err
	}
	if ids != "" {
		a.NoticeIDs = strings.Split(ids, ",")
	}
	a.CreatedAt = parseTime(created)
	return a, nil
}

// LastDeliveryToArea reports when the area last heard something, for cooldown.
func (s *Store) LastDeliveryToArea(ctx context.Context, areaID string) (time.Time, error) {
	var created string
	err := s.db.QueryRowContext(ctx, `
SELECT created_at FROM announcements WHERE area_id = ? ORDER BY created_at DESC LIMIT 1`, areaID).
		Scan(&created)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return parseTime(created), nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// OpenNoticeByDedupeKey returns the pending or delivered notice with the key.
// Watchers use it to skip re-evaluating a sensor that keeps flapping.
func (s *Store) OpenNoticeByDedupeKey(ctx context.Context, key string) (Notice, error) {
	if key == "" {
		return Notice{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, noticeSelect+` WHERE dedupe_key = ? AND state IN (?, ?)`,
		key, NoticePending, NoticeDelivered)
	n, err := scanNotice(row)
	if err == sql.ErrNoRows {
		return Notice{}, ErrNotFound
	}
	return n, err
}
