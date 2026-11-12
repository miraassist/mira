package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/excerpt"
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
	// MessageFailed is given up: a fatal error, or the attempts used up
	// (docs/spec/retry.md).
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
	// SourceDevices is Mira's note about devices Home Assistant found
	// (docs/spec/found-devices.md).
	SourceDevices = "devices"
)

// Kinds of messages (docs/spec/messages.md, Kinds of messages). Every message
// has one, set by whoever puts it; the kind says what happens with it.
const (
	KindSimple       = "simple"
	KindReminder     = "reminder"
	KindUrgent       = "urgent"
	KindPrivate      = "private"
	KindConfidential = "confidential"
	KindPostfach     = "postfach"
	KindVoicemail    = "voicemail"
	KindDevice       = "device"
	// KindReply is what comes back to a request at once: a thread's
	// question or proposal, a helper's result (decision 16).
	KindReply = "reply"
)

// KindFor is the kind a caller's fields say (decision 17): private,
// confidential, importance 3 urgent, else simple.
func KindFor(importance int, confidentiality string) string {
	switch {
	case confidentiality == ConfidentialityPrivate:
		return KindPrivate
	case confidentiality == ConfidentialityConfidential:
		return KindConfidential
	case importance >= 3:
		return KindUrgent
	}
	return KindSimple
}

// gathering are the kinds of which a receiver has at most one unheard
// message: what comes while it is unheard becomes an entry of it (decision
// 3). The others are one message per entry (decision 6).
var gathering = map[string]bool{KindSimple: true, KindPostfach: true, KindVoicemail: true, KindDevice: true}

// KindSkill is the skill a kind's entries lead to through their threads; ""
// for a kind without one.
func KindSkill(kind string) string {
	switch kind {
	case KindPostfach:
		return "postfach"
	case KindVoicemail:
		return "anrufbeantworter"
	case KindDevice:
		return "geraet-einbinden"
	}
	return ""
}

// confidentialityOf is a kind's confidentiality: private and confidential
// are kinds of their own, every other kind is normal.
func confidentialityOf(kind string) string {
	switch kind {
	case KindPrivate:
		return ConfidentialityPrivate
	case KindConfidential:
		return ConfidentialityConfidential
	}
	return ConfidentialityNormal
}

// ReceiverHouse is the house as receiver: anyone present may hear the message.
const ReceiverHouse = "house"

// Confidentiality levels. A normal message may be spoken to whoever is in the
// room; a confidential one only when the notifier's policy says the right
// person is the one listening, otherwise it goes to a private channel. A
// private one is never spoken: it goes over the receiver's channels only
// (docs/spec/schedule-receiver.md).
const (
	ConfidentialityNormal       = "normal"
	ConfidentialityConfidential = "confidential"
	ConfidentialityPrivate      = "private"
)

// Message is what waits for one receiver (docs/spec/messages.md, The
// entities): a container of entries. Text, details, importance, expiry,
// sender, source, key and thread are its entries'; on the message they are
// worked out from them (the text is the entries' lines, the importance the
// highest, the thread the one all entries lead to, or none).
type Message struct {
	ID   string
	Kind string
	// Entries are what came into the message, oldest first; filled by
	// GetMessage and ListMessages.
	Entries      []Entry
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
	// Confidentiality is ConfidentialityNormal, ConfidentialityConfidential
	// or ConfidentialityPrivate.
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
	// OriginDevice is the satellite's device it was asked at: in a room
	// with several satellites it is spoken there (docs/spec/satellites-rooms.md).
	OriginDevice string
	SessionKey   string
	// Attempts counts failed deliveries: of a scheduled message, or of a
	// private one (docs/spec/retry.md).
	Attempts int
	// RetryAt is when a failed private delivery is tried next; zero for
	// at once.
	RetryAt time.Time
	// ThreadID is the thread ("Vorgang") the message belongs to, if any: a
	// reply to it continues that thread, and the message is withdrawn when
	// the thread ends.
	ThreadID string
}

// Entry is one thing that came into a message: a mail, a recording, a
// found device, a task's notice (docs/spec/messages.md, The entities).
type Entry struct {
	ID         string
	MessageID  string
	Text       string
	Details    string
	Sender     string
	Source     string
	Importance int
	ExpiresAt  time.Time
	DedupeKey  string
	// ThreadID is the thread the entry leads to, if any.
	ThreadID string
	// OriginArea is the room a person left it in; it is held back there for
	// a while (docs/spec/messages.md, decision 18).
	OriginArea string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Confidential reports whether the message must not be spoken to an unknown
// listener.
func (n Message) Confidential() bool { return n.Confidentiality == ConfidentialityConfidential }

// Private reports whether the message goes over the receiver's channels only,
// never spoken.
func (n Message) Private() bool { return n.Confidentiality == ConfidentialityPrivate }

// PutMessage puts one entry: n's text, details, importance, expiry, sender,
// source, key and thread. An open entry with the same key is replaced in its
// message (a sensor that flaps, a task that updates its notice), the
// message's state unchanged. Else, for a gathering kind without a delivery
// time, the entry joins the receiver's unheard message of that kind; else it
// becomes a new message. The kind is required (decision 1); its
// confidentiality follows from it. Returns the message and whether it is
// new.
func (s *Store) PutMessage(ctx context.Context, n Message) (Message, bool, error) {
	if n.ID == "" {
		return Message{}, false, fmt.Errorf("message needs an id")
	}
	if strings.TrimSpace(n.Spoken) == "" {
		return Message{}, false, fmt.Errorf("message needs spoken text")
	}
	switch n.Kind {
	case KindSimple, KindReminder, KindUrgent, KindPrivate, KindConfidential, KindPostfach, KindVoicemail, KindDevice,
		KindReply:
	case "":
		return Message{}, false, fmt.Errorf("message %q needs a kind", excerpt.Of(n.Spoken))
	default:
		return Message{}, false, fmt.Errorf("message %q: unknown kind %q", excerpt.Of(n.Spoken), n.Kind)
	}
	want := confidentialityOf(n.Kind)
	if n.Confidentiality != "" && n.Confidentiality != want {
		return Message{}, false, fmt.Errorf("message %q of kind %s cannot be %s", excerpt.Of(n.Spoken), n.Kind, n.Confidentiality)
	}
	n.Confidentiality = want
	if n.State == "" {
		n.State = MessagePending
	}
	if n.Receiver == "" {
		n.Receiver = ReceiverHouse
	}
	now := s.now()
	n.UpdatedAt = now
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}
	e := Entry{ID: n.ID, Text: n.Spoken, Details: n.Details, Sender: n.Sender, Source: n.Source,
		Importance: n.Importance, ExpiresAt: n.ExpiresAt, DedupeKey: n.DedupeKey, ThreadID: n.ThreadID,
		OriginArea: n.OriginArea, CreatedAt: n.CreatedAt, UpdatedAt: now}

	var created bool
	var id string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if n.DedupeKey != "" {
			var entryID, msgID string
			err := tx.QueryRowContext(ctx, `
SELECT e.id, e.message_id FROM message_entries e JOIN messages m ON m.id = e.message_id
WHERE e.dedupe_key = ? AND m.kind = ? AND m.state IN (?, ?)`, n.DedupeKey, n.Kind, MessagePending, MessageDelivered).Scan(&entryID, &msgID)
			switch {
			case err == nil:
				// Refresh its content, but keep the ids and creation time so
				// the follow-up link stays valid; a delivered message stays
				// delivered (announcing it again would say it twice).
				_, err = tx.ExecContext(ctx, `
UPDATE message_entries SET text = ?, details = ?, sender = ?, source = ?, importance = ?, expires_at = ?,
                           thread_id = ?, origin_area = ?, updated_at = ?
WHERE id = ?`, e.Text, e.Details, e.Sender, e.Source, e.Importance, formatTime(e.ExpiresAt), e.ThreadID,
					e.OriginArea, formatTime(now), entryID)
				if err != nil {
					return err
				}
				id = msgID
				return s.refreshMessage(ctx, tx, msgID)
			case err != sql.ErrNoRows:
				return err
			}
		}
		if gathering[n.Kind] && n.DeliverAt.IsZero() {
			var msgID string
			err := tx.QueryRowContext(ctx, `
SELECT id FROM messages WHERE kind = ? AND receiver = ? AND state = ? AND (expires_at = '' OR expires_at > ?)
ORDER BY created_at DESC LIMIT 1`, n.Kind, n.Receiver, MessagePending, formatTime(now)).Scan(&msgID)
			switch {
			case err == nil:
				id = msgID
				if err := insertEntry(ctx, tx, msgID, e); err != nil {
					return err
				}
				return s.refreshMessage(ctx, tx, msgID)
			case err != sql.ErrNoRows:
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO messages(id, kind, source, dedupe_key, importance, receiver, spoken, details,
                    created_at, updated_at, expires_at, state, delivered_via, delivered_at, confidentiality, thread_id,
                    sender, deliver_at, area_id, session_key, origin_area, origin_device)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.Kind, n.Source, n.DedupeKey, n.Importance, n.Receiver, n.Spoken, n.Details,
			formatTime(n.CreatedAt), formatTime(n.UpdatedAt), formatTime(n.ExpiresAt), n.State, n.Confidentiality, n.ThreadID,
			n.Sender, formatTime(n.DeliverAt), n.AreaID, n.SessionKey, n.OriginArea, n.OriginDevice)
		if err != nil {
			return err
		}
		if err := insertEntry(ctx, tx, n.ID, e); err != nil {
			return err
		}
		id, created = n.ID, true
		return nil
	})
	if err != nil {
		return Message{}, false, fmt.Errorf("put message: %w", err)
	}
	stored, err := s.GetMessage(ctx, id)
	s.messageChanged()
	return stored, created, err
}

func insertEntry(ctx context.Context, tx *sql.Tx, messageID string, e Entry) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO message_entries(id, message_id, text, details, sender, source, importance, expires_at, dedupe_key,
                            thread_id, origin_area, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.ID, messageID, e.Text, e.Details, e.Sender, e.Source, e.Importance,
		formatTime(e.ExpiresAt), e.DedupeKey, e.ThreadID, e.OriginArea, formatTime(e.CreatedAt), formatTime(e.UpdatedAt))
	return err
}

// refreshMessage works out a message's row from its entries that have not
// expired: the text is their lines, the details theirs, the importance the
// highest, the expiry the latest (none when one never expires), the thread
// the one all lead to, sender, source and key those of the first. A message
// whose entries are all gone is expired.
func (s *Store) refreshMessage(ctx context.Context, tx *sql.Tx, messageID string) error {
	rows, err := tx.QueryContext(ctx, entrySelect+` WHERE message_id = ? ORDER BY created_at, id`, messageID)
	if err != nil {
		return err
	}
	var entries []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	now := s.now()
	if len(entries) == 0 {
		_, err := tx.ExecContext(ctx, `UPDATE messages SET state = ?, updated_at = ? WHERE id = ? AND state IN (?, ?, ?)`,
			MessageExpired, formatTime(now), messageID, MessagePending, MessageDelivered, MessageScheduled)
		return err
	}
	m := summarize(entries)
	_, err = tx.ExecContext(ctx, `
UPDATE messages SET spoken = ?, details = ?, importance = ?, expires_at = ?, thread_id = ?, sender = ?, source = ?,
                    dedupe_key = ?, updated_at = ?
WHERE id = ?`, m.Spoken, m.Details, m.Importance, formatTime(m.ExpiresAt), m.ThreadID, m.Sender, m.Source,
		m.DedupeKey, formatTime(now), messageID)
	return err
}

// summarize works out a message's own fields from its entries.
func summarize(entries []Entry) Message {
	var m Message
	var lines, details []string
	thread, oneThread := entries[0].ThreadID, true
	for i, e := range entries {
		lines = append(lines, strings.TrimSpace(e.Text))
		if d := strings.TrimSpace(e.Details); d != "" {
			details = append(details, d)
		}
		if i == 0 || e.Importance > m.Importance {
			m.Importance = e.Importance
		}
		if e.ThreadID != thread {
			oneThread = false
		}
	}
	m.ExpiresAt = entries[0].ExpiresAt
	for _, e := range entries {
		if e.ExpiresAt.IsZero() {
			m.ExpiresAt = time.Time{}
			break
		}
		if e.ExpiresAt.After(m.ExpiresAt) {
			m.ExpiresAt = e.ExpiresAt
		}
	}
	m.Spoken, m.Details = strings.Join(lines, "\n"), strings.Join(details, "\n")
	if oneThread {
		m.ThreadID = thread
	}
	m.Sender, m.Source, m.DedupeKey = entries[0].Sender, entries[0].Source, entries[0].DedupeKey
	return m
}

const entrySelect = `
SELECT id, message_id, text, details, sender, source, importance, expires_at, dedupe_key, thread_id,
       origin_area, created_at, updated_at
FROM message_entries`

func scanEntry(row scannable) (Entry, error) {
	var e Entry
	var expires, created, updated string
	if err := row.Scan(&e.ID, &e.MessageID, &e.Text, &e.Details, &e.Sender, &e.Source, &e.Importance, &expires,
		&e.DedupeKey, &e.ThreadID, &e.OriginArea, &created, &updated); err != nil {
		return Entry{}, err
	}
	e.ExpiresAt, e.CreatedAt, e.UpdatedAt = parseTime(expires), parseTime(created), parseTime(updated)
	return e, nil
}

// entriesOf loads the entries of the messages, oldest first, leaving out
// those expired by now.
func (s *Store) entriesOf(ctx context.Context, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	idx := map[string]int{}
	args := make([]any, 0, len(msgs))
	for i, m := range msgs {
		idx[m.ID] = i
		args = append(args, m.ID)
	}
	rows, err := s.db.QueryContext(ctx, entrySelect+` WHERE message_id IN (`+placeholders(len(msgs))+`)
ORDER BY created_at, id`, args...)
	if err != nil {
		return fmt.Errorf("read the messages' entries: %w", err)
	}
	defer rows.Close()
	now := s.now()
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return err
		}
		if !e.ExpiresAt.IsZero() && !e.ExpiresAt.After(now) {
			continue
		}
		i := idx[e.MessageID]
		msgs[i].Entries = append(msgs[i].Entries, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// An entry may have expired since the row was worked out: what is read
	// is worked out from the entries left.
	for i := range msgs {
		if len(msgs[i].Entries) > 0 {
			msgs[i] = msgs[i].with(msgs[i].Entries)
		}
	}
	return nil
}

// with is the message with only these entries, its own fields worked out
// from them.
func (n Message) with(entries []Entry) Message {
	m := summarize(entries)
	n.Entries = entries
	n.Spoken, n.Details, n.Importance = m.Spoken, m.Details, m.Importance
	n.ExpiresAt, n.ThreadID = m.ExpiresAt, m.ThreadID
	n.Sender, n.Source, n.DedupeKey = m.Sender, m.Source, m.DedupeKey
	return n
}

// Only is the message with the entries keep accepts, its own fields worked
// out from them, and whether any is left. A message without entries (of
// the form before kinds) is kept as it is.
func (n Message) Only(keep func(Entry) bool) (Message, bool) {
	if len(n.Entries) == 0 {
		return n, true
	}
	var kept []Entry
	for _, e := range n.Entries {
		if keep(e) {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		return n, false
	}
	return n.with(kept), true
}

func (s *Store) GetMessage(ctx context.Context, id string) (Message, error) {
	row := s.db.QueryRowContext(ctx, messageSelect+` WHERE id = ?`, id)
	n, err := scanMessage(row)
	if err == sql.ErrNoRows {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	one := []Message{n}
	if err := s.entriesOf(ctx, one); err != nil {
		return Message{}, err
	}
	return one[0], nil
}

const messageSelect = `
SELECT id, kind, source, dedupe_key, importance, receiver, spoken, details,
       created_at, updated_at, expires_at, state, delivered_via, delivered_at, confidentiality, thread_id,
       sender, deliver_at, area_id, session_key, attempts, origin_area, origin_device, retry_at
FROM messages`

type scannable interface{ Scan(...any) error }

func scanMessage(row scannable) (Message, error) {
	var n Message
	var created, updated, expires, delivered, deliverAt, retryAt string
	err := row.Scan(&n.ID, &n.Kind, &n.Source, &n.DedupeKey, &n.Importance, &n.Receiver, &n.Spoken, &n.Details,
		&created, &updated, &expires, &n.State, &n.DeliveredVia, &delivered, &n.Confidentiality, &n.ThreadID,
		&n.Sender, &deliverAt, &n.AreaID, &n.SessionKey, &n.Attempts, &n.OriginArea, &n.OriginDevice, &retryAt)
	if err != nil {
		return Message{}, err
	}
	n.CreatedAt, n.UpdatedAt, n.DeliverAt = parseTime(created), parseTime(updated), parseTime(deliverAt)
	n.RetryAt = parseTime(retryAt)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, s.entriesOf(ctx, out)
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

// PrivateDeliveryFailed counts a failed private delivery of a pending
// message (docs/spec/retry.md, row 1): it is tried again at retryAt, or,
// with a zero retryAt, given up as failed. It returns the attempts so far.
func (s *Store) PrivateDeliveryFailed(ctx context.Context, id string, retryAt time.Time) (int, error) {
	state, at := MessagePending, ""
	if retryAt.IsZero() {
		state = MessageFailed
	} else {
		at = formatTime(retryAt)
	}
	var attempts int
	err := s.db.QueryRowContext(ctx, `
UPDATE messages SET attempts = attempts + 1, retry_at = ?, state = ?, updated_at = ?
WHERE id = ? AND state = ? RETURNING attempts`,
		at, state, formatTime(s.now()), id, MessagePending).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return attempts, err
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
	var id string
	err := s.db.QueryRowContext(ctx, `
SELECT m.id FROM message_entries e JOIN messages m ON m.id = e.message_id
WHERE e.dedupe_key = ? AND m.state IN (?, ?)`, key, MessagePending, MessageDelivered).Scan(&id)
	if err == sql.ErrNoRows {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	return s.GetMessage(ctx, id)
}

// WithdrawThreadMessages takes a thread's entries out of their open
// messages when the thread ends (done, cancelled or expired): its questions
// and proposals are moot. A message left without entries is expired
// (docs/spec/messages.md, decision 12). It returns how many entries left.
func (s *Store) WithdrawThreadMessages(ctx context.Context, threadID string) (int64, error) {
	var removed int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
SELECT DISTINCT e.message_id FROM message_entries e JOIN messages m ON m.id = e.message_id
WHERE e.thread_id = ? AND m.state IN (?, ?, ?)`, threadID, MessagePending, MessageDelivered, MessageScheduled)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			res, err := tx.ExecContext(ctx, `DELETE FROM message_entries WHERE message_id = ? AND thread_id = ?`, id, threadID)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			removed += n
			if err := s.refreshMessage(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && removed > 0 {
		s.messageChanged()
	}
	return removed, err
}

// AckEntry marks one entry done (docs/spec/messages.md, decision 18): it
// leaves its message; the last one ends the message as acked. An id that is
// no entry's is taken as a message's (one of the form before kinds).
func (s *Store) AckEntry(ctx context.Context, id string) error {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var msgID string
		err := tx.QueryRowContext(ctx, `SELECT message_id FROM message_entries WHERE id = ?`, id).Scan(&msgID)
		if errors.Is(err, sql.ErrNoRows) {
			res, err := tx.ExecContext(ctx, `UPDATE messages SET state = ?, updated_at = ? WHERE id = ?`,
				MessageAcked, formatTime(s.now()), id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return ErrNotFound
			}
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_entries WHERE id = ?`, id); err != nil {
			return err
		}
		var left int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_entries WHERE message_id = ?`, msgID).Scan(&left); err != nil {
			return err
		}
		if left == 0 {
			_, err := tx.ExecContext(ctx, `UPDATE messages SET state = ?, updated_at = ? WHERE id = ?`,
				MessageAcked, formatTime(s.now()), msgID)
			return err
		}
		return s.refreshMessage(ctx, tx, msgID)
	})
	if err == nil {
		s.messageChanged()
	}
	return err
}

// Lead is an open entry of a kind with a skill, with its kind.
type Lead struct {
	Kind string
	Entry
}

// Leads are the open entries of kinds with a skill (postfach, voicemail,
// device) for the receivers, oldest first: what still waits to be taken up
// (docs/spec/messages.md, decision 10). Entries expired by now are left out.
func (s *Store) Leads(ctx context.Context, receivers []string) ([]Lead, error) {
	if len(receivers) == 0 {
		return nil, nil
	}
	args := []any{KindPostfach, KindVoicemail, KindDevice, MessagePending, MessageDelivered, formatTime(s.now())}
	for _, r := range receivers {
		args = append(args, r)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT m.kind, e.id, e.message_id, e.text, e.details, e.sender, e.source, e.importance, e.expires_at, e.dedupe_key,
       e.thread_id, e.origin_area, e.created_at, e.updated_at
FROM message_entries e JOIN messages m ON m.id = e.message_id
WHERE m.kind IN (?, ?, ?) AND m.state IN (?, ?) AND (e.expires_at = '' OR e.expires_at > ?)
  AND m.receiver IN (`+placeholders(len(receivers))+`)
ORDER BY e.created_at, e.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read what waits: %w", err)
	}
	defer rows.Close()
	var out []Lead
	for rows.Next() {
		var l Lead
		var expires, created, updated string
		if err := rows.Scan(&l.Kind, &l.ID, &l.MessageID, &l.Text, &l.Details, &l.Sender, &l.Source, &l.Importance,
			&expires, &l.DedupeKey, &l.ThreadID, &l.OriginArea, &created, &updated); err != nil {
			return nil, err
		}
		l.ExpiresAt, l.CreatedAt, l.UpdatedAt = parseTime(expires), parseTime(created), parseTime(updated)
		out = append(out, l)
	}
	return out, rows.Err()
}

// DropOldFormMessages expires the open messages of the form before kinds
// (docs/spec/messages.md, decision 15): no migration into entries. It
// returns how many there were.
func (s *Store) DropOldFormMessages(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state = ?, updated_at = ? WHERE kind = '' AND state IN (?, ?, ?)`,
		MessageExpired, formatTime(s.now()), MessagePending, MessageDelivered, MessageScheduled)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
