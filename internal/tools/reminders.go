package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
)

const (
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
	t := textsFor(o.Lang)
	decode := func(raw json.RawMessage, v any) error { return decodeArgs(t, raw, v) }
	return []InternalTool{
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
				r := store.Reminder{ID: ulid.Make().String(), DueAt: due, Text: a.Text, SessionKey: inv.SessionKey,
					Audience: AudienceFor(a.For, inv, o.Persons())}
				if inv.Channel == config.ChannelVoice && o.Store != nil {
					if sess, err := o.Store.GetSession(ctx, inv.SessionKey); err == nil {
						r.AreaID = sess.AreaID
					}
				}
				if err := o.Store.AddReminder(ctx, r); err != nil {
					return "", err
				}
				return fmt.Sprintf(t.reminderSaved, i18n.FormatTime(o.Lang, due), r.ID), nil
			},
		},
		{
			Name:        ToolReminderList,
			Description: t.reminderListDesc,
			Schema:      `{"type":"object","properties":{}}`,
			ReadOnly:    true,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				list, err := o.Store.PendingReminders(ctx, time.Time{})
				if err != nil {
					return "", err
				}
				if len(list) == 0 {
					return t.noReminders, nil
				}
				var b strings.Builder
				for _, r := range list {
					fmt.Fprintf(&b, "- [%s] %s: %s", r.ID, i18n.FormatTime(o.Lang, r.DueAt.In(o.Location)), r.Text)
					if r.Audience != store.AudienceHousehold {
						fmt.Fprintf(&b, " (%s)", r.Audience)
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
				if err := o.Store.SetReminderState(ctx, a.ID, store.ReminderCancelled); errors.Is(err, store.ErrNotFound) {
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

// AudienceFor is the person a request is for: the one named, else the
// Telegram user who asked, else the household.
func AudienceFor(named string, inv Invocation, persons map[string]config.Person) string {
	if _, ok := persons[named]; ok {
		return named
	}
	if inv.Channel == config.ChannelTelegram {
		chat, err := strconv.ParseInt(strings.TrimPrefix(inv.SessionKey, string(config.ChannelTelegram)+":"), 10, 64)
		if err == nil {
			for id, p := range persons {
				if p.TelegramUserID == chat {
					return id
				}
			}
		}
	}
	return store.AudienceHousehold
}
