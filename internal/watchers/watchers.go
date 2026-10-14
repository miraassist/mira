// Package watchers turns events in the house into notices.
//
// A watcher subscribes to a Home Assistant state trigger for the entities it
// names; nothing looks at every state change. When the trigger fires, the
// model rates the event and words it once, on the background slot, and the
// result is queued. Delivery is someone else's job (the notifier).
package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sker65/lares/internal/config"
	haws "github.com/sker65/lares/internal/ha/ws"
	"github.com/sker65/lares/internal/prompt"
	"github.com/sker65/lares/internal/store"
)

// Assessor rates an event. The conversation engine implements it.
type Assessor interface {
	Assess(ctx context.Context, instruction string, schema json.RawMessage) (string, error)
}

// Subscriber is the part of the WebSocket client the watchers need.
type Subscriber interface {
	SubscribeTrigger(ctx context.Context, trigger map[string]any, handler haws.EventHandler) (func(), error)
}

type Options struct {
	Watchers      []config.Watcher
	MinImportance int
	Store         *store.Store
	Assessor      Assessor
	Logger        *slog.Logger
	Clock         func() time.Time
	Location      *time.Location
}

type Manager struct {
	opts Options
	log  *slog.Logger
}

func New(opts Options) *Manager {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	return &Manager{opts: opts, log: opts.Logger.With("component", "watchers")}
}

// Event is a fired trigger, reduced to what the assessment needs.
type Event struct {
	EntityID     string
	FriendlyName string
	From, To     string
	Attributes   map[string]any
}

type firing struct {
	watcher config.Watcher
	event   Event
}

// Run subscribes every trigger watcher and processes firings one at a time
// until ctx ends.
func (m *Manager) Run(ctx context.Context, sub Subscriber) error {
	queue := make(chan firing, 64)
	for _, w := range m.opts.Watchers {
		if w.Trigger == nil {
			if w.Schedule != "" {
				m.log.Info("scheduled watchers are not implemented yet (M4)", "watcher", w.Name)
			}
			continue
		}
		cancel, err := sub.SubscribeTrigger(ctx, TriggerFor(w), func(ev haws.Event) {
			e, ok := decode(ev)
			if !ok {
				return
			}
			select {
			case queue <- firing{watcher: w, event: e}:
			default:
				m.log.Warn("watcher queue full, dropping event", "watcher", w.Name)
			}
		})
		if err != nil {
			m.log.Error("could not subscribe watcher", "watcher", w.Name, "error", err)
			continue
		}
		defer cancel()
		m.log.Info("watching", "watcher", w.Name, "entities", w.Trigger.EntityID)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case f := <-queue:
			if _, _, err := m.Handle(ctx, f.watcher, f.event); err != nil && ctx.Err() == nil {
				m.log.Error("watcher event failed", "watcher", f.watcher.Name, "error", err)
			}
		}
	}
}

// TriggerFor builds the Home Assistant state trigger of a watcher.
func TriggerFor(w config.Watcher) map[string]any {
	t := map[string]any{"trigger": "state", "entity_id": w.Trigger.EntityID}
	if len(w.Trigger.To) > 0 {
		t["to"] = w.Trigger.To
	}
	if len(w.Trigger.From) > 0 {
		t["from"] = w.Trigger.From
	}
	return t
}

func decode(ev haws.Event) (Event, bool) {
	var v haws.TriggerVariables
	if err := json.Unmarshal(ev.Variables, &v); err != nil || v.Trigger.ToState == nil {
		return Event{}, false
	}
	e := Event{EntityID: v.Trigger.EntityID, To: v.Trigger.ToState.State,
		FriendlyName: v.Trigger.ToState.FriendlyName(), Attributes: v.Trigger.ToState.Attributes}
	if v.Trigger.FromState != nil {
		e.From = v.Trigger.FromState.State
	}
	return e, true
}

// DedupeKey is the key a watcher's notices share.
func DedupeKey(w config.Watcher) string {
	if w.DedupeKey != "" {
		return w.DedupeKey
	}
	return "watcher:" + w.Name
}

// Handle turns one firing into a notice. It reports whether a notice was
// stored; false with a nil error means it was deliberately not (already open,
// or not important enough).
func (m *Manager) Handle(ctx context.Context, w config.Watcher, e Event) (store.Notice, bool, error) {
	key := DedupeKey(w)
	// A flapping sensor fires again and again. While its notice is open,
	// the new firing adds nothing, and skipping it saves an LLM call.
	if open, err := m.opts.Store.OpenNoticeByDedupeKey(ctx, key); err == nil {
		m.log.Debug("notice already open", "watcher", w.Name, "notice", open.ID)
		return open, false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.Notice{}, false, err
	}

	a, err := m.assess(ctx, w, e)
	if err != nil {
		m.log.Warn("assessment failed, using the watcher's own description", "watcher", w.Name, "error", err)
		a = fallback(w, e)
	}
	if w.Importance != nil {
		a.Importance = *w.Importance
	}
	if a.Importance < m.opts.MinImportance {
		m.log.Info("event judged not worth a notice", "watcher", w.Name, "importance", a.Importance)
		return store.Notice{}, false, nil
	}
	expires := w.ExpiresIn.Duration()
	if expires == 0 {
		expires = time.Duration(a.ExpiresInHours) * time.Hour
	}
	if expires <= 0 {
		expires = 24 * time.Hour
	}
	n, _, err := m.opts.Store.PutNotice(ctx, store.Notice{
		ID: ulid.Make().String(), Source: store.SourceHATrigger, DedupeKey: key,
		Importance: a.Importance, Audience: w.Audience, Confidentiality: w.Confidentiality,
		Spoken: a.Spoken, Details: a.Details, ExpiresAt: m.opts.Clock().Add(expires),
	})
	if err != nil {
		return store.Notice{}, false, err
	}
	m.log.Info("notice queued", "watcher", w.Name, "notice", n.ID, "importance", n.Importance)
	return n, true, nil
}

// Assessment is the model's verdict on an event.
type Assessment struct {
	Importance     int    `json:"importance"`
	Spoken         string `json:"spoken"`
	Details        string `json:"details"`
	ExpiresInHours int    `json:"expires_in_hours"`
}

// assessmentSchema constrains the model's answer.
var assessmentSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"importance":{"type":"integer","minimum":0,"maximum":3},` +
	`"spoken":{"type":"string"},"details":{"type":"string"},` +
	`"expires_in_hours":{"type":"integer","minimum":1,"maximum":168}},` +
	`"required":["importance","spoken","details","expires_in_hours"]}`)

func (m *Manager) assess(ctx context.Context, w config.Watcher, e Event) (Assessment, error) {
	if m.opts.Assessor == nil {
		return Assessment{}, errors.New("no assessor")
	}
	raw, err := m.opts.Assessor.Assess(ctx, m.instruction(w, e), assessmentSchema)
	if err != nil {
		return Assessment{}, err
	}
	var a Assessment
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return Assessment{}, fmt.Errorf("unparseable assessment: %w", err)
	}
	a.Spoken, a.Details = strings.TrimSpace(a.Spoken), strings.TrimSpace(a.Details)
	if a.Spoken == "" || a.Importance < 0 || a.Importance > 3 {
		return Assessment{}, fmt.Errorf("incomplete assessment: %s", raw)
	}
	return a, nil
}

func (m *Manager) instruction(w config.Watcher, e Event) string {
	var b strings.Builder
	b.WriteString("Ein Ereignis im Haus ist eingetreten. Bewerte, wie wichtig es für den Haushalt ist, und formuliere eine Meldung.\n\n")
	if w.Description != "" {
		fmt.Fprintf(&b, "Bedeutung: %s\n", w.Description)
	}
	name := e.FriendlyName
	if name == "" {
		name = e.EntityID
	}
	fmt.Fprintf(&b, "Entität: %s (%s)\n", name, e.EntityID)
	if e.From != "" {
		fmt.Fprintf(&b, "Zustand vorher: %s\n", e.From)
	}
	fmt.Fprintf(&b, "Zustand jetzt: %s\n", e.To)
	fmt.Fprintf(&b, "Zeitpunkt: %s\n", prompt.FormatTime(m.opts.Clock().In(m.opts.Location)))
	if attrs := formatAttributes(e.Attributes); attrs != "" {
		fmt.Fprintf(&b, "Attribute: %s\n", attrs)
	}
	b.WriteString(`
Antworte nur mit einem JSON-Objekt:
- importance: 0 = nicht erwähnenswert, 1 = nebenbei erwähnen, 2 = wichtig, sollte heute jemand ansehen, 3 = dringend, Gefahr oder Schaden droht.
  Beispiele: Waschmaschine fertig = 1. Wechselrichter- oder Heizungsstörung = 2. Wassermelder, Rauchmelder, offene Haustür nachts = 3.
- spoken: ein bis zwei kurze Sätze zum Vorlesen, sachlich und ruhig, den Nutzer mit "du" ansprechen, ohne Markdown, Zahlen und Uhrzeiten ausgeschrieben, ohne Begrüßung
- details: die Langfassung für Nachfragen, mit den konkreten Werten und dem Zeitpunkt
- expires_in_hours: wie lange die Meldung relevant bleibt`)
	return b.String()
}

// formatAttributes renders the attributes in a stable order and drops the
// ones that only matter to the UI.
func formatAttributes(attrs map[string]any) string {
	var keys []string
	for k := range attrs {
		switch k {
		case "friendly_name", "icon", "entity_picture", "supported_features", "device_class", "state_class":
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, attrs[k]))
	}
	s := strings.Join(parts, ", ")
	if len(s) > 1000 {
		s = s[:1000] + " …"
	}
	return s
}

// fallback words a notice without the model, so an event is never lost
// because the LLM is down.
func fallback(w config.Watcher, e Event) Assessment {
	name := e.FriendlyName
	if name == "" {
		name = e.EntityID
	}
	spoken := w.Description
	if spoken == "" {
		spoken = fmt.Sprintf("%s ist jetzt %s.", name, e.To)
	}
	details := fmt.Sprintf("%s (%s) wechselte von %q zu %q.", name, e.EntityID, e.From, e.To)
	return Assessment{Importance: 2, Spoken: spoken, Details: details, ExpiresInHours: 24}
}
