package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Message states. A message with a delivery time (a reminder) is scheduled
// until then; it counts as waiting only once it is due.
const (
	MessageScheduled = "scheduled"
	MessagePending   = "pending"
	MessageDelivered = "delivered"
	MessageAcked     = "acked"
	MessageExpired   = "expired"
	MessageCancelled = "cancelled"
	// MessageFailed is given up after MaxDeliveryAttempts.
	MessageFailed = "failed"
)

// Message sources.
const (
	SourceHATrigger = "ha_trigger"
	SourceSchedule  = "schedule"
	SourceExternal  = "external"
	SourceReminder  = "reminder"
	SourceTask      = "task"
	// SourcePerson is a message one person sends another or the house.
	SourcePerson = "person"
)

// ReceiverHouse is the house as receiver: anyone present may hear the message.
const ReceiverHouse = "house"

// Confidentiality levels. A normal message may be spoken to whoever is in the
// room; a confidential one only when the notifier's policy says the right
// person is the one listening, otherwise it goes to a private channel.
const (
	ConfidentialityNormal       = "normal"
	ConfidentialityConfidential = "confidential"
)

type Message struct {
	ID           string
	Source       string
	DedupeKey    string
	Importance   int
	Receiver     string
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
	// Sender is who sent it: "person:<key>", "area:<id>" (someone at a
	// satellite), or empty for Mira herself (a task, a skill, a reminder).
	Sender string
	// DeliverAt is when a scheduled message is delivered (a reminder); it
	// is then spoken in AreaID, the room it was asked for in, or sent
	// privately. Zero for a message that waits to be heard.
	DeliverAt time.Time
	AreaID    string
	// OriginArea is the room a person left the message in, "" when it did not
	// come from a satellite. The notifier holds it back there for a while.
	OriginArea string
	SessionKey string
	// Attempts counts failed deliveries of a scheduled message.
	Attempts int
	// ThreadID is the thread ("Vorgang") the message belongs to, if any: a
	// reply to it continues that thread, and the message is withdrawn when
	// the thread ends.
	ThreadID string
}

// Confidential reports whether the message must not be spoken to an unknown
// listener.
func (n Message) Confidential() bool { return n.Confidentiality == ConfidentialityConfidential }

// PutMessage inserts a message, or updates the open message with the same dedupe
// key instead of creating a duplicate. A sensor that flaps therefore produces
// one row, not many. Returns the stored message and whether it was new.
func (s *Store) PutMessage(ctx context.Context, n Message) (Message, bool, error) {
	if n.ID == "" {
		return Message{}, false, fmt.Errorf("message needs an id")
	}
	if strings.TrimSpace(n.Spoken) == "" {
		return Message{}, false, fmt.Errorf("message needs spoken text")
	}
	if n.State == "" {
		n.State = MessagePending
	}
	if n.Receiver == "" {
		n.Receiver = ReceiverHouse
	}
	switch n.Confidentiality {
	case "":
		n.Confidentiality = ConfidentialityNormal
	case ConfidentialityNormal, ConfidentialityConfidential:
	default:
		return Message{}, false, fmt.Errorf("unknown confidentiality %q", n.Confidentiality)
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
SELECT id FROM messages
WHERE dedupe_key = ? AND state IN (?, ?)`, n.DedupeKey, MessagePending, MessageDelivered).Scan(&existing)
			switch {
			case err == nil:
				// Refresh content and importance, but keep the original id and
				// creation time so the follow-up link stays valid. Re-opening a
				// delivered message would announce it twice, so the state stays.
				_, err = tx.ExecContext(ctx, `
UPDATE messages SET source = ?, importance = ?, receiver = ?, spoken = ?, details = ?,
                   updated_at = ?, expires_at = ?, confidentiality = ?
WHERE id = ?`, n.Source, n.Importance, n.Receiver, n.Spoken, n.Details,
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
INSERT INTO messages(id, source, dedupe_key, importance, receiver, spoken, details,
                    created_at, updated_at, expires_at, state, delivered_via, delivered_at, confidentiality, thread_id,
                    sender, deliver_at, area_id, session_key, origin_area)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.Source, n.DedupeKey, n.Importance, n.Receiver, n.Spoken, n.Details,
			formatTime(n.CreatedAt), formatTime(n.UpdatedAt), formatTime(n.ExpiresAt), n.State, n.Confidentiality, n.ThreadID,
			n.Sender, formatTime(n.DeliverAt), n.AreaID, n.SessionKey, n.OriginArea)
		if err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return Message{}, false, fmt.Errorf("put message: %w", err)
	}
	stored, err := s.GetMessage(ctx, n.ID)
	s.messageChanged()
	return stored, created, err
}

func (s *Store) GetMessage(ctx context.Context, id string) (Message, error) {
	row := s.db.QueryRowContext(ctx, messageSelect+` WHERE id = ?`, id)
	n, err := scanMessage(row)
	if err == sql.ErrNoRows {
		return Message{}, ErrNotFound
	}
	return n, err
}

const messageSelect = `
SELECT id, source, dedupe_key, importance, receiver, spoken, details,
       created_at, updated_at, expires_at, state, delivered_via, delivered_at, confidentiality, thread_id,
       sender, deliver_at, area_id, session_key, attempts, origin_area
FROM messages`

type scannable interface{ Scan(...any) error }

func scanMessage(row scannable) (Message, error) {
	var n Message
	var created, updated, expires, delivered, deliverAt string
	err := row.Scan(&n.ID, &n.Source, &n.DedupeKey, &n.Importance, &n.Receiver, &n.Spoken, &n.Details,
		&created, &updated, &expires, &n.State, &n.DeliveredVia, &delivered, &n.Confidentiality, &n.ThreadID,
		&n.Sender, &deliverAt, &n.AreaID, &n.SessionKey, &n.Attempts, &n.OriginArea)
	if err != nil {
		return Message{}, err
	}
	n.CreatedAt, n.UpdatedAt, n.DeliverAt = parseTime(created), parseTime(updated), parseTime(deliverAt)
	n.ExpiresAt, n.DeliveredAt = parseTime(expires), parseTime(delivered)
	return n, nil
}

// MessageFilter selects messages for listing.
type MessageFilter struct {
	States        []string
	MinImportance int
	Receiver      string
	// IncludeExpired keeps messages whose expires_at has passed.
	IncludeExpired bool
	// NewestFirst orders by creation, newest first, for a history view.
	NewestFirst bool
	Limit       int
	// Offset skips that many matches, for paging.
	Offset int
}

// ListMessages returns matching messages, most important first and oldest first
// within the same importance. That is the order announcements are built in.
func (s *Store) ListMessages(ctx context.Context, f MessageFilter) ([]Message, error) {
	q := messageSelect + ` WHERE importance >= ?`
	args := []any{f.MinImportance}
	if len(f.States) > 0 {
		q += ` AND state IN (` + placeholders(len(f.States)) + `)`
		for _, st := range f.States {
			args = append(args, st)
		}
	}
	if f.Receiver != "" {
		q += ` AND receiver = ?`
		args = append(args, f.Receiver)
	}
	if !f.IncludeExpired {
		q += ` AND (expires_at = '' OR expires_at > ?)`
		args = append(args, formatTime(s.now()))
	}
	if f.NewestFirst {
		q += ` ORDER BY created_at DESC`
	} else {
		q += ` ORDER BY importance DESC, created_at`
	}
	if f.Limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		n, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkDelivered records that a message went out over a channel.
func (s *Store) MarkDelivered(ctx context.Context, id, via string) error {
	now := formatTime(s.now())
	res, err := s.db.ExecContext(ctx, `
UPDATE messages SET state = ?, delivered_via = ?, delivered_at = ?, updated_at = ?
WHERE id = ? AND state = ?`, MessageDelivered, via, now, now, id, MessagePending)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetMessageState moves a message to acked or expired.
func (s *Store) SetMessageState(ctx context.Context, id, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state = ?, updated_at = ? WHERE id = ?`,
		state, formatTime(s.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ExpireMessages moves every past-due pending or delivered message to expired.
func (s *Store) ExpireMessages(ctx context.Context) (int64, error) {
	now := formatTime(s.now())
	res, err := s.db.ExecContext(ctx, `
UPDATE messages SET state = ?, updated_at = ?
WHERE state IN (?, ?) AND expires_at <> '' AND expires_at <= ?`,
		MessageExpired, now, MessagePending, MessageDelivered, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) CountPendingMessages(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM messages
WHERE state = ? AND (expires_at = '' OR expires_at > ?)`,
		MessagePending, formatTime(s.now())).Scan(&n)
	return n, err
}

// Announcement records what was spoken where, so a follow-up question from the
// same satellite can be linked back to the messages it is about.
type Announcement struct {
	ID         string
	DeviceID   string
	AreaID     string
	Channel    string
	MessageIDs []string
	// ThreadID is the thread the announcement belongs to, when it names one
	// itself rather than through a message of the thread.
	ThreadID  string
	CreatedAt time.Time
}

func (s *Store) RecordAnnouncement(ctx context.Context, a Announcement) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO announcements(id, device_id, area_id, channel, message_ids, thread_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.DeviceID, a.AreaID, a.Channel, strings.Join(a.MessageIDs, ","), a.ThreadID, formatTime(a.CreatedAt))
	return err
}

// RecentAnnouncement returns the last announcement to a device if it happened
// within the window, which is how the notifier links a reply to its messages.
func (s *Store) RecentAnnouncement(ctx context.Context, deviceID string, window time.Duration) (Announcement, error) {
	if deviceID == "" {
		return Announcement{}, ErrNotFound
	}
	cutoff := formatTime(s.now().Add(-window))
	var a Announcement
	var ids, created string
	err := s.db.QueryRowContext(ctx, `
SELECT id, device_id, area_id, channel, message_ids, thread_id, created_at
FROM announcements
WHERE device_id = ? AND created_at >= ?
ORDER BY created_at DESC LIMIT 1`, deviceID, cutoff).
		Scan(&a.ID, &a.DeviceID, &a.AreaID, &a.Channel, &ids, &a.ThreadID, &created)
	if err == sql.ErrNoRows {
		return Announcement{}, ErrNotFound
	}
	if err != nil {
		return Announcement{}, err
	}
	if ids != "" {
		a.MessageIDs = strings.Split(ids, ",")
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

// OpenMessageByDedupeKey returns the pending or delivered message with the key.
// Watchers use it to skip re-evaluating a sensor that keeps flapping.
func (s *Store) OpenMessageByDedupeKey(ctx context.Context, key string) (Message, error) {
	if key == "" {
		return Message{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, messageSelect+` WHERE dedupe_key = ? AND state IN (?, ?)`,
		key, MessagePending, MessageDelivered)
	n, err := scanMessage(row)
	if err == sql.ErrNoRows {
		return Message{}, ErrNotFound
	}
	return n, err
}

// WithdrawThreadMessages expires every open message of a thread: when the
// thread ends, its questions and proposals are moot.
func (s *Store) WithdrawThreadMessages(ctx context.Context, threadID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE messages SET state = ?, updated_at = ?
WHERE thread_id = ? AND state IN (?, ?)`, MessageExpired, formatTime(s.now()), threadID, MessagePending, MessageDelivered)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
