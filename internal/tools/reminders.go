package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

const (
	ToolMessageSend    = "message_send"
	ToolReminderSet    = "reminder_set"
	ToolReminderList   = "reminder_list"
	ToolReminderCancel = "reminder_cancel"
)

type ReminderOptions struct {
	Lang     i18n.Lang
	Store    *store.Store
	Clock    func() time.Time
	Location *time.Location
	// Persons are the configured persons; the web UI can change them.
	Persons func() map[string]config.Person
	// AreaName names an area, for the sender "someone in the kitchen".
	AreaName func(id string) string
}

// Reminders returns the reminder tools. They are always there, not part of
// a skill: "remind me tomorrow at seven" is said often, and loading a skill
// first would cost the voice channel a whole model round.
//
// The model gives either a local time or minutes from now; it is bad at
// adding minutes to a clock time, so a timer is "in_minutes". A reminder
// asked for by voice is spoken in that room; one asked for on Telegram goes
// to that person.
func Reminders(o ReminderOptions) []InternalTool {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	if o.Persons == nil {
		o.Persons = func() map[string]config.Person { return nil }
	}
	if o.AreaName == nil {
		o.AreaName = func(id string) string { return id }
	}
	t := textsFor(o.Lang)
	decode := func(raw json.RawMessage, v any) error { return decodeArgs(t, raw, v) }
	return []InternalTool{
		{
			Name:        ToolMessageSend,
			AskAfterWeb: true,
			Description: t.messageSendDesc,
			Schema: schema(`{"type":"object","properties":{"to":{"type":"string","description":%q},"text":{"type":"string","description":%q},"at":{"type":"string","description":%q},"in_minutes":{"type":"integer","description":%q}},"required":["to","text"]}`,
				t.messageTo, t.messageSendText, t.reminderAt, t.reminderInMinutes),
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					To        string `json:"to"`
					Text      string `json:"text"`
					At        string `json:"at"`
					InMinutes int    `json:"in_minutes"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				text := strings.TrimRight(strings.TrimSpace(a.Text), ".")
				if text == "" {
					return "", errors.New(t.textMissing)
				}
				persons := o.Persons()
				receiver := resolveReceiver(a.To, persons)
				if receiver == "" {
					return "", fmt.Errorf(t.unknownReceiver, a.To)
				}
				if err := accepts(t, persons, receiver, inv); err != nil {
					return "", err
				}
				from := senderName(t, persons, o.AreaName, inv)
				format := t.fromPerson
				switch {
				case receiver == store.ReceiverHouse && senderOf(inv) != "" && !strings.HasPrefix(senderOf(inv), "area:"):
					format = t.fromPersonHouse
				case receiver == store.ReceiverHouse:
					format = t.fromAreaHouse
				case strings.HasPrefix(senderOf(inv), "area:") || senderOf(inv) == "":
					format = t.fromArea
				}
				spoken := fmt.Sprintf(format, from, text) + "."
				if receiver == inv.Person {
					// "Send that to my phone": no "you ask me to tell you".
					spoken = text + "."
				}
				m := store.Message{ID: ulid.Make().String(), Source: store.SourcePerson, Sender: senderOf(inv),
					Importance: 1, Receiver: receiver, Spoken: spoken, SessionKey: inv.SessionKey,
					ExpiresAt: o.Clock().Add(7 * 24 * time.Hour)}
				if a.At != "" || a.InMinutes != 0 {
					now := o.Clock().In(o.Location)
					due, err := dueTime(now, a.At, a.InMinutes, o.Location)
					if err != nil {
						return "", fmt.Errorf(t.reminderBadTime, err)
					}
					if due.Before(now.Add(-time.Minute)) {
						return "", errors.New(t.reminderPast)
					}
					m.DeliverAt, m.Importance, m.ExpiresAt = due, 2, time.Time{}
					if _, err := o.Store.ScheduleMessage(ctx, m); err != nil {
						return "", err
					}
				} else if _, _, err := o.Store.PutMessage(ctx, m); err != nil {
					return "", err
				}
				return fmt.Sprintf(t.messageSent, receiverName(persons, receiver)), nil
			},
		},
		{
			Name:        ToolReminderSet,
			Description: t.reminderSetDesc,
			Schema: schema(`{"type":"object","properties":{"text":{"type":"string","description":%q},"at":{"type":"string","description":%q},"in_minutes":{"type":"integer","description":%q},"for":{"type":"string","description":%q}},"required":["text"]}`,
				t.reminderText, t.reminderAt, t.reminderInMinutes, t.reminderFor),
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					Text      string `json:"text"`
					At        string `json:"at"`
					InMinutes int    `json:"in_minutes"`
					For       string `json:"for"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if strings.TrimSpace(a.Text) == "" {
					return "", errors.New(t.textMissing)
				}
				now := o.Clock().In(o.Location)
				due, err := dueTime(now, a.At, a.InMinutes, o.Location)
				if err != nil {
					return "", fmt.Errorf(t.reminderBadTime, err)
				}
				if due.Before(now.Add(-time.Minute)) {
					return "", errors.New(t.reminderPast)
				}
				if due.After(now.AddDate(1, 1, 0)) {
					return "", errors.New(t.reminderTooFar)
				}
				// A reminder is a message with a delivery time
				// (docs/spec/nachrichten.md).
				m := store.Message{ID: ulid.Make().String(), Source: store.SourceReminder, Importance: 2,
					Spoken: strings.TrimSpace(a.Text), DeliverAt: due, SessionKey: inv.SessionKey,
					Receiver: ReceiverFor(a.For, inv, o.Persons())}
				if m.Receiver != store.ReceiverHouse && m.Receiver != inv.Person {
					// A reminder for someone else is a message from the
					// speaker: their settings decide, and it says who.
					persons := o.Persons()
					if err := accepts(t, persons, m.Receiver, inv); err != nil {
						return "", err
					}
					m.Source, m.Sender = store.SourcePerson, senderOf(inv)
					m.Spoken = fmt.Sprintf(t.remindFrom, senderName(t, persons, o.AreaName, inv), m.Spoken)
				}
				if inv.Channel == config.ChannelVoice && o.Store != nil {
					if sess, err := o.Store.GetSession(ctx, inv.SessionKey); err == nil {
						m.AreaID = sess.AreaID
					}
				}
				if _, err := o.Store.ScheduleMessage(ctx, m); err != nil {
					return "", err
				}
				return fmt.Sprintf(t.reminderSaved, i18n.FormatTime(o.Lang, due), m.ID), nil
			},
		},
		{
			Name:        ToolReminderList,
			Description: t.reminderListDesc,
			Schema:      `{"type":"object","properties":{}}`,
			ReadOnly:    true,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				list, err := o.Store.ScheduledMessages(ctx, time.Time{})
				if err != nil {
					return "", err
				}
				if len(list) == 0 {
					return t.noReminders, nil
				}
				var b strings.Builder
				for _, r := range list {
					fmt.Fprintf(&b, "- [%s] %s: %s", r.ID, i18n.FormatTime(o.Lang, r.DeliverAt.In(o.Location)), r.Spoken)
					if r.Receiver != store.ReceiverHouse {
						fmt.Fprintf(&b, " (%s)", r.Receiver)
					}
					b.WriteByte('\n')
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        ToolReminderCancel,
			Description: t.reminderCancelDesc,
			Schema:      `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if err := o.Store.CancelScheduled(ctx, a.ID); errors.Is(err, store.ErrNotFound) {
					return "", errors.New(t.noSuchReminder)
				} else if err != nil {
					return "", err
				}
				return t.reminderCancelled, nil
			},
		},
	}
}

// dueTime reads "at" as a local date and time, or adds in_minutes to now.
func dueTime(now time.Time, at string, inMinutes int, loc *time.Location) (time.Time, error) {
	at = strings.TrimSpace(at)
	switch {
	case at != "" && inMinutes != 0:
		return time.Time{}, errors.New("give either at or in_minutes")
	case inMinutes > 0:
		return now.Add(time.Duration(inMinutes) * time.Minute), nil
	case at == "":
		return time.Time{}, errors.New("at or in_minutes is needed")
	}
	at = strings.Replace(at, "T", " ", 1)
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, at, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not YYYY-MM-DD HH:MM", at)
}

// ReceiverFor is the person a request is for: the one named, else the
// one who asked, else the house.
func ReceiverFor(named string, inv Invocation, persons map[string]config.Person) string {
	if _, ok := persons[named]; ok {
		return named
	}
	if _, ok := persons[inv.Person]; ok {
		return inv.Person
	}
	return store.ReceiverHouse
}

// senderOf is who sends from this turn: the person speaking, else the
// satellite's area, else nobody known (empty).
func senderOf(inv Invocation) string {
	switch {
	case inv.Person != "":
		return "person:" + inv.Person
	case inv.Area != "":
		return "area:" + inv.Area
	}
	return ""
}

// senderName is how the receiver hears the sender.
func senderName(t texts, persons map[string]config.Person, areaName func(string) string, inv Invocation) string {
	if inv.Person != "" {
		return receiverName(persons, inv.Person)
	}
	if inv.Area != "" {
		return areaName(inv.Area)
	}
	return "?"
}

func receiverName(persons map[string]config.Person, key string) string {
	if p, ok := persons[key]; ok && p.Name != "" {
		return p.Name
	}
	return key
}

// resolveReceiver reads the model's "to": the house, a person key or a
// person's name; "" when nobody fits.
func resolveReceiver(to string, persons map[string]config.Person) string {
	to = strings.TrimSpace(to)
	switch strings.ToLower(to) {
	case "", "house", "haus", "household", "haushalt":
		return store.ReceiverHouse
	}
	if _, ok := persons[to]; ok {
		return to
	}
	for key, p := range persons {
		if strings.EqualFold(p.Name, to) || strings.EqualFold(key, to) {
			return key
		}
	}
	return ""
}

// accepts checks the receiver's setting for messages from people
// (docs/spec/nachrichten.md): the house takes everything; a person takes
// all, only messages from persons Mira recognised, or none. A message to
// oneself is always fine.
func accepts(t texts, persons map[string]config.Person, receiver string, inv Invocation) error {
	if receiver == store.ReceiverHouse || receiver == inv.Person {
		return nil
	}
	p := persons[receiver]
	switch p.AcceptFrom {
	case config.AcceptNone:
		return fmt.Errorf(t.refusedNone, receiverName(persons, receiver))
	case config.AcceptPersons:
		if inv.Person == "" {
			return fmt.Errorf(t.refusedPersons, receiverName(persons, receiver))
		}
	}
	return nil
}
