// Package mailcal is the mail calendar (docs/spec/mail-kalender.md): a mail
// that announces something with a time (a parcel, an appointment) is read in
// an isolated model call and kept as an entry in a calendar. The entry is
// found again by a key in its description, a newer mail changes it, an
// older one is dropped, a cancellation removes it, and what is done or long
// past is cleaned away.
//
// What a mail of an application means is told by a skill (its mail.extract);
// the schema and the checks are here. A mail is someone else's text: it
// reaches the model only as data, and only checked fields come back.
package mailcal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Mail is one mail as the Mira integration hands it over.
type Mail struct {
	// Entry is the name of the IMAP entry in Home Assistant: "<app>" or
	// "<app> <login>". The calendar has the same name.
	Entry   string    `json:"entry"`
	Sender  string    `json:"sender"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Text    string    `json:"text"`
}

// Event is a calendar entry as the calendar reports it.
type Event struct {
	UID         string
	Summary     string
	Description string
	Location    string
	// Day is the start day; Start/End are set for an entry with a time.
	Day        time.Time
	Start, End time.Time
	AllDay     bool
}

// Entry is what Mira writes into the calendar.
type Entry struct {
	Summary, Description, Location string
	Day                            time.Time // all day when Start is zero
	Start, End                     time.Time
}

// Calendar is what the mail calendar needs from Home Assistant.
type Calendar interface {
	// Find returns the entity of the exposed calendar with this name.
	Find(ctx context.Context, name string) (string, bool)
	// Names lists the names of the exposed calendars.
	Names(ctx context.Context) ([]string, error)
	Events(ctx context.Context, entity string, from, to time.Time) ([]Event, error)
	Create(ctx context.Context, entity string, e Entry) error
	Update(ctx context.Context, entity, uid string, e Entry) error
	Delete(ctx context.Context, entity, uid string) error
}

// Model runs one isolated request with a JSON answer.
type Model interface {
	Ask(ctx context.Context, system, instruction string, schema json.RawMessage) (string, error)
}

type Options struct {
	Calendar Calendar
	Model    Model
	// Apps returns the mail applications: skill name (lower case) to its
	// mail.extract.
	Apps     func() map[string]string
	Location *time.Location
	Clock    func() time.Time
	Logger   *slog.Logger
}

type Service struct {
	o     Options
	log   *slog.Logger
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func New(o Options) *Service {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{o: o, log: o.Logger.With("component", "mailcal"), locks: map[string]*sync.Mutex{}}
}

// AppNames lists the mail applications, for /v1/status: the integration
// passes on only the mails of IMAP entries named like one.
func (s *Service) AppNames() []string {
	var out []string
	for name := range s.o.Apps() {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// App says which application an IMAP entry's name belongs to ("Pakete
// stefan" is pakete); "" when none.
func (s *Service) App(entry string) string {
	f := strings.Fields(strings.ToLower(entry))
	if len(f) == 0 {
		return ""
	}
	if _, ok := s.o.Apps()[f[0]]; ok {
		return f[0]
	}
	return ""
}

// Outcome says what a mail did.
type Outcome struct {
	Action string `json:"action"` // created, updated, deleted, ignored
	Reason string `json:"reason,omitempty"`
	Title  string `json:"title,omitempty"`
}

// ErrNoApp is the error for a mail of an entry no application has.
var ErrNoApp = errors.New("no mail application for this entry")

// Handle reads one mail and keeps its entry. Mails of one calendar are
// handled one after the other.
func (s *Service) Handle(ctx context.Context, m Mail) (Outcome, error) {
	app := s.App(m.Entry)
	if app == "" {
		return Outcome{}, fmt.Errorf("%w: %q", ErrNoApp, m.Entry)
	}
	name := strings.Join(strings.Fields(m.Entry), " ")
	lock := s.lock(strings.ToLower(name))
	lock.Lock()
	defer lock.Unlock()

	entity, ok := s.o.Calendar.Find(ctx, name)
	if !ok {
		return Outcome{Action: "ignored", Reason: "no calendar " + name}, nil
	}
	if m.Date.IsZero() {
		m.Date = s.o.Clock()
	}
	item, err := s.extract(ctx, s.o.Apps()[app], m)
	if err != nil {
		return Outcome{}, err
	}
	if !item.Relevant {
		return Outcome{Action: "ignored", Reason: "not relevant"}, nil
	}
	entry, ok := s.check(&item, m.Date)
	if !ok {
		return Outcome{Action: "ignored", Reason: "no title or day"}, nil
	}

	now := s.o.Clock()
	events, err := s.o.Calendar.Events(ctx, entity, now.AddDate(0, 0, -30), now.AddDate(1, 0, 1))
	if err != nil {
		return Outcome{}, err
	}
	found := s.find(ctx, events, item)
	out := Outcome{Title: entry.Summary}
	switch {
	case found != nil && !newer(m.Date, *found):
		return Outcome{Action: "ignored", Reason: "older than the entry", Title: found.Summary}, nil
	case found != nil && item.Status == statusCancelled:
		out.Action, err = "deleted", s.o.Calendar.Delete(ctx, entity, found.UID)
	case found != nil:
		entry.Description = describe(item, m.Date)
		out.Action, err = "updated", s.o.Calendar.Update(ctx, entity, found.UID, entry)
	case item.Status == statusCancelled:
		return Outcome{Action: "ignored", Reason: "cancels nothing known"}, nil
	default:
		entry.Description = describe(item, m.Date)
		out.Action, err = "created", s.o.Calendar.Create(ctx, entity, entry)
	}
	if err != nil {
		return Outcome{}, err
	}
	s.log.Info("mail kept", "calendar", name, "action", out.Action, "title", out.Title, "key", item.Key != "")
	return out, nil
}

func (s *Service) lock(name string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks[name] == nil {
		s.locks[name] = &sync.Mutex{}
	}
	return s.locks[name]
}

// --- the entry's own lines -------------------------------------------------

const (
	markKey    = "mira-mail: "
	markStand  = "Stand: "
	markStatus = "Status: "

	statusScheduled = "scheduled"
	statusCancelled = "cancelled"
	statusDone      = "done"
)

func describe(it item, date time.Time) string {
	key := it.Key
	if key == "" {
		key = "-"
	}
	var b strings.Builder
	if d := strings.TrimSpace(it.Details); d != "" {
		b.WriteString(d + "\n\n")
	}
	b.WriteString(markKey + key + "\n")
	b.WriteString(markStand + date.UTC().Format(time.RFC3339) + "\n")
	b.WriteString(markStatus + it.Status)
	return b.String()
}

// marks reads the lines Mira wrote; ok is false for an entry Mira did not make.
func marks(description string) (key string, stand time.Time, status string, ok bool) {
	for _, line := range strings.Split(description, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, markKey):
			key, ok = strings.TrimSpace(strings.TrimPrefix(line, markKey)), true
		case strings.HasPrefix(line, markStand):
			stand, _ = time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(line, markStand)))
		case strings.HasPrefix(line, markStatus):
			status = strings.TrimSpace(strings.TrimPrefix(line, markStatus))
		}
	}
	return key, stand, status, ok
}

// newer: the mail is newer than what the entry was last changed from.
func newer(date time.Time, e Event) bool {
	_, stand, _, _ := marks(e.Description)
	return stand.IsZero() || date.After(stand)
}

// --- the mail to text --------------------------------------------------------

var (
	reBlocks = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)>`)
	reBreaks = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/li|/h\d)[^>]*>`)
	reTags   = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpaces = regexp.MustCompile(`[ \t\r\f\v\x{a0}]+`)
	reLines  = regexp.MustCompile(`\n\s*\n+`)
)

// plain turns a mail's text into plain text: HTML without its markup,
// whitespace collapsed, cut to a size a model reads well.
func plain(text string) string {
	if strings.Contains(text, "<") && strings.Contains(strings.ToLower(text), "</") {
		text = reBlocks.ReplaceAllString(text, " ")
		text = reBreaks.ReplaceAllString(text, "\n")
		text = reTags.ReplaceAllString(text, " ")
		text = html.UnescapeString(text)
	}
	text = reSpaces.ReplaceAllString(text, " ")
	text = reLines.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > maxText {
		text = string(r[:maxText]) + " …"
	}
	return text
}

const maxText = 12000
