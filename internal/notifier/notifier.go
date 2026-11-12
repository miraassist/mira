// Package notifier decides when, where and how pending messages reach a
// person. It is deterministic code: the wording was produced when the message
// was created, so delivery never waits for the model.
//
// Voice delivery happens on presence: the first motion in a configured area
// outside quiet hours and outside the area's cooldown announces up to
// MaxItemsPerAnnouncement messages on that area's satellite. The morning rule
// of the plan is exactly that: quiet hours end, someone walks into the
// bathroom. Messages that must not be spoken right then go to a private
// channel instead, and important ones that no satellite delivered in time
// escalate to one.
package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/telemetry"
)

// HA is the part of the Home Assistant client the notifier needs.
type HA interface {
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
	// GetStates reads the states Home Assistant filters for it.
	GetStates(ctx context.Context, f haws.StateFilter) ([]haws.State, error)
	Entities(ctx context.Context) ([]haws.Entity, error)
	// WaitState waits until ok accepts an entity's state, for at most
	// limit, watching that entity rather than asking again and again.
	WaitState(ctx context.Context, entity string, limit time.Duration, ok func(state string) bool) bool
}

type Options struct {
	Config config.Config
	Store  *store.Store
	HA     HA
	// Structure is the registries as Mira keeps them current (devices and
	// their entities): a satellite's device and speakers are looked up
	// there, never read from a registry per delivery. Nil in tests that need
	// neither.
	Structure func() structure.Snapshot
	Clock     func() time.Time
	Location  *time.Location
	Logger    *slog.Logger
	Telemetry *telemetry.Provider
	// Composer words several messages as one announcement (compose.go).
	Composer Composer
	// Screens are on-screen overlay actions per satellite (screen.go); found
	// by the app.
	Screens map[string]string
	// Languages says which language an area or a person gets
	// (docs/spec/language.md); tests without it get the default language.
	Languages Languages
	// Phone is what is new on the FRITZ!Box's answering machine
	// (docs/spec/voicemail.md); nil without the skill anrufbeantworter.
	Phone func(ctx context.Context) (PhoneNews, error)
}

// PhoneNews is what is new at the FRITZ!Box: messages not heard, missed
// calls not seen.
type PhoneNews struct{ Messages, Missed int }

// Languages is package language as the notifier needs it.
type Languages interface {
	ForArea(ctx context.Context, area string) i18n.Lang
	ForPerson(ctx context.Context, person string) i18n.Lang
	Last(ctx context.Context) i18n.Lang
}

type Notifier struct {
	structure func() structure.Snapshot
	// indicatorOn is the last state set per area ("" off, else what it
	// shows: indicatorMira, indicatorVoicemail, indicatorMissed); missing
	// means unknown.
	indicatorOn map[string]string
	phone       func(ctx context.Context) (PhoneNews, error)
	// phoneShown is what each screen's phone hint last showed.
	phoneShown map[string]PhoneNews
	// phoneHeld are the areas playing messages of the answering machine:
	// their screen gets no new hint until it ends.
	phoneHeld map[string]bool
	// awaitingIdle are the satellites with a screen in a conversation, with
	// a hint waiting until it is idle again (screen.go).
	awaitingIdle map[string]bool
	// delivering are the scheduled messages being delivered now (remind.go).
	delivering map[string]bool
	langs      Languages
	cfg        config.Notifier
	// sats are each area's satellites, the primary first.
	sats     map[string]config.Entities
	persons  map[string]config.Person
	store    *store.Store
	ha       HA
	clock    func() time.Time
	loc      *time.Location
	log      *slog.Logger
	tel      *telemetry.Provider
	composer Composer
	screens  map[string]string
	// screenCount is what each screen overlay last showed; screenShown when.
	screenCount map[string]int
	screenShown map[string]time.Time
	areas       map[string]bool

	// speakingIn marks areas whose indicator glows for an announcement;
	// speakingStart is how long the satellite may take to start speaking.
	speakingIn    map[string]bool
	speakingStart time.Duration
	// speakPause is the breath between the end of one utterance and an
	// announcement that follows it.
	speakPause time.Duration

	// mu serialises decisions, so two motion events a second apart cannot
	// both announce the same messages.
	mu sync.Mutex
}

func New(opts Options) *Notifier {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Telemetry == nil {
		opts.Telemetry = telemetry.NewTestProvider()
	}
	n := &Notifier{
		speakingIn:    map[string]bool{},
		speakingStart: 15 * time.Second,
		speakPause:    1500 * time.Millisecond,
		langs:         opts.Languages,
		cfg:           opts.Config.Notifier,
		sats:          opts.Config.Satellites,
		persons:       opts.Config.Persons,
		store:         opts.Store,
		ha:            opts.HA,
		structure:     opts.Structure,
		clock:         opts.Clock,
		loc:           opts.Location,
		log:           opts.Logger.With("component", "notifier"),
		tel:           opts.Telemetry,
		composer:      opts.Composer,
		screens:       opts.Screens,
		screenCount:   map[string]int{},
		screenShown:   map[string]time.Time{},
		areas:         map[string]bool{},
		indicatorOn:   map[string]string{},
		phone:         opts.Phone,
		phoneShown:    map[string]PhoneNews{},
		phoneHeld:     map[string]bool{},
		awaitingIdle:  map[string]bool{},
		delivering:    map[string]bool{},
	}
	for _, a := range opts.Config.Notifier.MorningTrigger.Areas {
		n.areas[a] = true
	}
	return n
}

// Outcome of OnPresence, for logs and tests.
type Outcome struct {
	Skipped   string   // why nothing was announced, if nothing was
	Announced []string // message ids spoken
	Private   []string // message ids sent privately instead
}

// OnPresence handles motion in an area, spoken at its primary.
func (n *Notifier) OnPresence(ctx context.Context, area string) (Outcome, error) {
	return n.OnPresenceAt(ctx, area, n.primary(area))
}

// OnPresenceAt handles motion in an area, spoken at the satellite the motion
// sensor belongs to (docs/spec/satellites-rooms.md).
func (n *Notifier) OnPresenceAt(ctx context.Context, area, sat string) (Outcome, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.areas[area] {
		// No announcing on entry here: an indicator light, if any, shows
		// what waits, and it is read out when asked.
		n.syncIndicators(ctx)
		return Outcome{Skipped: "no announcing on entry"}, nil
	}
	now := n.clock()
	if quiet, err := n.store.GetBool(ctx, store.KeyQuietMode, false); err != nil {
		return Outcome{}, err
	} else if quiet {
		return Outcome{Skipped: "quiet mode"}, nil
	}
	if n.inQuietHours(now) {
		return Outcome{Skipped: "quiet hours"}, nil
	}
	last, err := n.store.LastDeliveryToArea(ctx, area)
	if err != nil {
		return Outcome{}, err
	}
	if !last.IsZero() && now.Sub(last) < n.cfg.Cooldown.Duration() {
		return Outcome{Skipped: "cooldown"}, nil
	}

	pending, err := n.pending(ctx)
	if err != nil {
		return Outcome{}, err
	}
	if len(pending) == 0 {
		return Outcome{Skipped: "nothing pending"}, nil
	}
	home := n.whoIsHome(ctx)

	var voice, private []store.Message
	for _, nt := range pending {
		switch n.route(nt, home) {
		case routeVoice:
			if m, ok := n.unheld(nt, area); ok {
				voice = append(voice, m)
			}
		case routePrivate:
			private = append(private, nt)
		}
	}

	var out Outcome
	for _, nt := range private {
		if n.deliverPrivate(ctx, nt) {
			out.Private = append(out.Private, nt.ID)
		}
	}
	if len(voice) == 0 {
		if out.Skipped = "nothing for this room"; len(out.Private) > 0 {
			out.Skipped = ""
		}
		return out, nil
	}
	if max := n.cfg.MaxItemsPerAnnouncement; len(voice) > max {
		voice = voice[:max] // the rest waits for the next occasion
	}
	if err := n.announce(ctx, area, sat, voice, true); err != nil {
		return out, err
	}
	for _, nt := range voice {
		out.Announced = append(out.Announced, nt.ID)
	}
	return out, nil
}

// Tick runs the time-driven rules: expiry, confidential messages that are
// never spoken, and escalation. Call it about once a minute.
func (n *Notifier) Tick(ctx context.Context) ([]string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, err := n.store.ExpireMessages(ctx); err != nil {
		return nil, err
	}
	now := n.clock()
	sent, err := n.deliverUrgent(ctx)
	if err != nil {
		return sent, err
	}
	// Indicators also during quiet hours: a light wakes nobody, and what came
	// in at night is shown in the morning.
	defer n.syncIndicators(ctx)
	if n.inQuietHours(now) {
		return sent, nil
	}
	pending, err := n.pending(ctx)
	if err != nil {
		return sent, err
	}
	esc := n.cfg.EscalateToTelegram
	local := now.In(n.loc)
	afterToday := local.Hour()*60+local.Minute() >= esc.After.MinutesOfDay()

	for _, nt := range pending {
		neverSpoken := nt.Private() || nt.Confidential() && n.cfg.ConfidentialVoice == config.ConfidentialVoiceNever
		escalate := nt.Importance >= esc.MinImportance && afterToday &&
			now.Sub(nt.CreatedAt) >= esc.Grace.Duration()
		if !neverSpoken && !escalate {
			continue
		}
		if !n.deliverPrivate(ctx, nt) {
			continue
		}
		reason := "escalation"
		switch {
		case nt.Private():
			reason = "private"
		case neverSpoken:
			reason = "confidential"
		}
		n.log.Info("message delivered privately", "message", nt.ID, "reason", reason)
		sent = append(sent, nt.ID)
	}
	return sent, nil
}

// deliverUrgent sends every pending urgent message at once, ignoring quiet
// hours, quiet mode and cooldowns: a water leak at three in the morning must
// not wait for the morning announcement. It goes privately to everyone and,
// unless confidential, is spoken on every satellite.
func (n *Notifier) deliverUrgent(ctx context.Context) ([]string, error) {
	urgent := n.cfg.Urgent
	if urgent.MinImportance <= 0 {
		return nil, nil
	}
	pending, err := n.pending(ctx)
	if err != nil {
		return nil, err
	}
	var sent []string
	now := n.clock()
	for _, nt := range pending {
		if nt.Importance < urgent.MinImportance {
			continue
		}
		// The private part waits for its next attempt (docs/spec/retry.md,
		// row 1); the announcement does not.
		due := nt.RetryAt.IsZero() || !now.Before(nt.RetryAt)
		privErr := errNotDue
		if due {
			privErr = n.sendPrivate(ctx, nt)
		}
		spoken := false
		if urgent.Announce && !nt.Confidential() && !nt.Private() {
			spoken = n.announceEverywhere(ctx, nt)
		}
		switch {
		case privErr == nil || spoken:
			if due && privErr != nil {
				n.log.Error("urgent private delivery failed; the message was spoken", "message", nt.ID, "error", privErr)
			}
			n.log.Warn("urgent message delivered", "message", nt.ID, "spoken", spoken, "private", privErr == nil)
			sent = append(sent, nt.ID)
		case due:
			n.privateFailed(ctx, nt, now, privErr)
		}
	}
	return sent, nil
}

// announceEverywhere speaks one message once in each room, at its primary.
func (n *Notifier) announceEverywhere(ctx context.Context, nt store.Message) bool {
	done := map[string]bool{}
	spoken := false
	for _, area := range sortedKeys(n.sats) {
		sat := n.primary(area)
		if sat == "" || done[sat] {
			continue
		}
		done[sat] = true
		if err := n.announce(ctx, area, sat, []store.Message{nt}, true); err != nil {
			n.log.Error("urgent announcement failed", "area", area, "error", err)
			continue
		}
		spoken = true
	}
	return spoken
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (n *Notifier) pending(ctx context.Context) ([]store.Message, error) {
	msgs, err := n.store.ListMessages(ctx, store.MessageFilter{
		States: []string{store.MessagePending}, MinImportance: n.cfg.MinImportance,
	})
	return n.important(msgs), err
}

// important keeps of each message the entries at or above the importance
// threshold (docs/spec/messages.md, decision 18): an entry below it counts as
// not there for announcing, the ring and reading out.
func (n *Notifier) important(msgs []store.Message) []store.Message {
	var out []store.Message
	for _, nt := range msgs {
		if m, ok := nt.Only(func(e store.Entry) bool { return e.Importance >= n.cfg.MinImportance }); ok {
			out = append(out, m)
		}
	}
	return out
}

type route int

const (
	routeWait route = iota
	routeVoice
	routePrivate
)

// route decides what happens to a message when someone is in a room with a
// satellite. Nobody knows who that someone is; the person entities only say
// who is home.
func (n *Notifier) route(nt store.Message, home map[string]bool) route {
	if nt.Private() {
		return routePrivate // never spoken, whoever is in the room
	}
	if nt.Receiver != store.ReceiverHouse {
		if p, ok := n.persons[nt.Receiver]; ok && p.Entity != "" && !home[nt.Receiver] && !home["?"+nt.Receiver] {
			// The recipient is away: whoever walked in is someone else.
			return routeWait
		}
	}
	if !nt.Confidential() {
		return routeVoice
	}
	switch n.cfg.ConfidentialVoice {
	case config.ConfidentialVoiceAlways:
		return routeVoice
	case config.ConfidentialVoiceWhenAlone:
		alone := len(home) == 1
		if nt.Receiver != store.ReceiverHouse {
			alone = alone && home[nt.Receiver]
		}
		if alone {
			return routeVoice
		}
	}
	return routePrivate
}

// whoIsHome reads the person entities. An unreadable state counts as
// unknown, which is treated like "not alone": it errs towards privacy.
func (n *Notifier) whoIsHome(ctx context.Context) map[string]bool {
	home := map[string]bool{}
	if len(n.persons) == 0 {
		return home
	}
	var entities []string
	for _, p := range n.persons {
		if p.Entity != "" {
			entities = append(entities, p.Entity)
		}
	}
	states, err := n.ha.GetStates(ctx, haws.StateFilter{EntityIDs: entities})
	if err != nil {
		n.log.Warn("could not read person states", "error", err)
		home["?"] = true
		return home
	}
	byEntity := map[string]string{}
	for _, s := range states {
		byEntity[s.EntityID] = s.State
	}
	for id, p := range n.persons {
		if p.Entity == "" {
			continue
		}
		switch byEntity[p.Entity] {
		case "home":
			home[id] = true
		case "", "unknown", "unavailable":
			// No tracker or no reading: this person might be there.
			home["?"+id] = true
		}
	}
	return home
}

func (n *Notifier) inQuietHours(t time.Time) bool {
	local := t.In(n.loc)
	m := local.Hour()*60 + local.Minute()
	from, to := n.cfg.QuietHours.From.MinutesOfDay(), n.cfg.QuietHours.To.MinutesOfDay()
	if from == to {
		return false
	}
	if from < to {
		return m >= from && m < to
	}
	return m >= from || m < to // wraps midnight
}

// texts are the words around the messages of an announcement.
type texts struct {
	goodMorning, intro, also, and string // intro: %s = count word
	reminder                      string // %s = what to remember
	byTheWay                      string
	counts                        []string
	// For the model's wording (compose.go): the task (%s now, %s the
	// list), the morning greeting, and how times are described.
	composeTask, composeMorning, importance               string
	whenToday, whenNight, whenYesterday, whenDate         string // %s clock (date first for whenDate)
	prefixToday, prefixNight, prefixYesterday, prefixDate string
	// The screen overlay (screen.go): one, several (%d), all heard.
	screenOne, screenMany, screenHeard string
	// The answering machine on the screen: one message, several (%d), one
	// missed call, several (%d).
	phoneMessage, phoneMessages, phoneMissed, phoneMisseds string
}

func textsFor(l i18n.Lang) texts { return i18n.Pick(catalog, l) }

var catalog = map[i18n.Lang]texts{
	i18n.DE: {goodMorning: "Guten Morgen. ", intro: "Ich habe %s Nachrichten. ", also: " Außerdem: ", and: " Und: ", reminder: "Erinnerung: %s.", byTheWay: "Übrigens: ",
		counts: []string{"null", "eine", "zwei", "drei", "vier", "fünf"},
		composeTask: `Formuliere aus diesen wartenden Nachrichten EINE gesprochene Ansage für den Raum. Jetzt ist es %s Uhr.

%s
Regeln:
- Nimm jede Nachricht auf, keine darf fehlen. Erfinde nichts dazu und ändere keine Werte, Namen oder Zeiten.
- Wichtigeres zuerst. Ähnliches oder Doppeltes darfst du zusammenfassen.
- Nenne die Zeit, wenn sie für das Verständnis hilft (z. B. bei Dingen, die vorbei sind: „heute Nacht um drei“, „vorhin um eins“). Bei Dingen, die kommen (Abholung morgen), nicht.
- Eine Nachricht, die schon sagt, von wem sie ist („Anna lässt dir ausrichten …“), behält das.
- Gesprochene Sprache, Du-Form, kurz, natürliche Übergänge statt Aufzählung. Keine Listen, keine Emojis, keine Markdown-Zeichen.
- Antworte mit JSON: "text" ist die Ansage, "ids" die IDs aller Nachrichten, die darin vorkommen.`,
		composeMorning: "\nBeginne mit „Guten Morgen“.",
		importance:     "Wichtigkeit",
		whenToday:      "heute %s", whenNight: "heute Nacht %s", whenYesterday: "gestern %s", whenDate: "am %s um %s",
		prefixToday: "Um %s: ", prefixNight: "Heute Nacht um %s: ", prefixYesterday: "Gestern um %s: ", prefixDate: "Am %s um %s: ",
		screenOne:    "1 Nachricht wartet – frag: „Hab ich neue Nachrichten?“",
		screenMany:   "%d Nachrichten warten – frag: „Hab ich neue Nachrichten?“",
		screenHeard:  "Alles gehört",
		phoneMessage: "1 Nachricht auf dem Anrufbeantworter", phoneMessages: "%d Nachrichten auf dem Anrufbeantworter",
		phoneMissed: "1 verpasster Anruf", phoneMisseds: "%d verpasste Anrufe"},
	i18n.EN: {goodMorning: "Good morning. ", intro: "I have %s messages. ", also: " Also: ", and: " And: ", reminder: "Reminder: %s.", byTheWay: "By the way: ",
		counts: []string{"zero", "one", "two", "three", "four", "five"},
		composeTask: `Word these waiting messages as ONE spoken announcement for the room. It is %s now.

%s
Rules:
- Include every message; none may be missing. Add nothing and do not change values, names or times.
- More important first. You may merge similar or duplicate ones.
- Say the time when it helps understanding (for things that are over: "tonight at three", "earlier at one"). Not for things to come (collection tomorrow).
- A message that already says who it is from ("Anna asks me to tell you …") keeps that.
- Spoken language, addressing the listener as "you", short, natural transitions instead of a list. No lists, no emojis, no Markdown.
- Answer with JSON: "text" is the announcement, "ids" the ids of all messages in it.`,
		composeMorning: "\nStart with \"Good morning\".",
		importance:     "importance",
		whenToday:      "today %s", whenNight: "tonight %s", whenYesterday: "yesterday %s", whenDate: "on %s at %s",
		prefixToday: "At %s: ", prefixNight: "Tonight at %s: ", prefixYesterday: "Yesterday at %s: ", prefixDate: "On %s at %s: ",
		screenOne:    "1 message waiting – ask: \"Do I have new messages?\"",
		screenMany:   "%d messages waiting – ask: \"Do I have new messages?\"",
		screenHeard:  "All heard",
		phoneMessage: "1 message on the answering machine", phoneMessages: "%d messages on the answering machine",
		phoneMissed: "1 missed call", phoneMisseds: "%d missed calls"},
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s != "" && !strings.ContainsAny(s[len(s)-1:], ".!?") {
		s += "."
	}
	return s
}

func countWord(t texts, n int) string {
	if n < len(t.counts) {
		return t.counts[n]
	}
	return fmt.Sprint(n)
}

// preannounce plays the satellite's chime first; an answer someone waits
// for in that room does without it. sat is the satellite of the room that
// speaks (docs/spec/satellites-rooms.md, Who speaks).
func (n *Notifier) announce(ctx context.Context, area, sat string, messages []store.Message, preannounce bool) error {
	ctx = n.inArea(ctx, area)
	text := n.compose(ctx, messages)
	method, err := n.method(ctx, sat)
	if err != nil {
		return fmt.Errorf("announce in %s on %s: %w", area, sat, err)
	}
	n.speaking(ctx, area, sat)
	ids := make([]string, len(messages))
	for i, nt := range messages {
		ids[i] = nt.ID
	}
	// start_conversation returns only once the satellite stopped listening,
	// after the reply. The reply must find what was announced (the record)
	// and must not wait for this notifier (Afterword, ReadOut take n.mu):
	// so the messages count as delivered and the record is written before
	// the call, and n.mu is let go while the satellite speaks and listens.
	// Should the call fail, the messages wait again.
	if method == "start_conversation" {
		if err := n.delivered(ctx, area, sat, ids); err != nil {
			return err
		}
		n.mu.Unlock()
		err := n.deliver(ctx, sat, method, text, preannounce)
		n.mu.Lock()
		if err != nil {
			for _, id := range ids {
				if err := n.store.SetMessageState(ctx, id, store.MessagePending); err != nil {
					// docs/spec/fallbacks.md, row 25.
					n.log.Error("a message that was not delivered cannot be set back to open; it may not come again", "message", id, "error", err)
				}
			}
			return fmt.Errorf("announce in %s on %s: %w", area, sat, err)
		}
	} else {
		if err := n.deliver(ctx, sat, method, text, preannounce); err != nil {
			return fmt.Errorf("announce in %s on %s: %w", area, sat, err)
		}
		if err := n.delivered(ctx, area, sat, ids); err != nil {
			return err
		}
	}
	n.tel.RecordMessageDelivery(ctx, "voice", len(ids))
	n.log.Info("announced", "area", area, "satellite", sat, "method", method, "messages", len(ids))
	return nil
}

// delivered marks messages delivered by voice in an area and records the
// announcement, so a reply from the satellite finds them.
func (n *Notifier) delivered(ctx context.Context, area, sat string, ids []string) error {
	for _, id := range ids {
		if err := n.store.MarkDelivered(ctx, id, "voice:"+area); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	return n.store.RecordAnnouncement(ctx, store.Announcement{
		ID: ulid.Make().String(), DeviceID: n.deviceID(ctx, sat), AreaID: area,
		Channel: "voice", MessageIDs: ids,
	})
}

// method is how a satellite speaks: tts for a plain speaker or by setting,
// else the configured one, or by the satellite's features.
func (n *Notifier) method(ctx context.Context, sat string) (string, error) {
	if strings.HasPrefix(sat, "media_player.") || n.cfg.Delivery == "tts" {
		return "tts", nil
	}
	if n.cfg.Delivery == "auto" {
		return n.satelliteMethod(ctx, sat)
	}
	return n.cfg.Delivery, nil
}

// deliver speaks text on a satellite by method.
func (n *Notifier) deliver(ctx context.Context, sat, method, text string, preannounce bool) error {
	target := &haws.Target{EntityID: []string{sat}}
	switch method {
	case "tts":
		return n.ha.CallService(ctx, "tts", "speak",
			map[string]any{"media_player_entity_id": sat, "message": text},
			&haws.Target{EntityID: []string{n.cfg.TTSEntity}})
	case "start_conversation":
		// The satellite listens afterwards; the reply comes back through the
		// integration and finds the messages through the announcement record.
		// Whether the satellite speaks is watched during the call (its state
		// responding), so that a call that runs out of time is decided by it.
		watch, stop := context.WithCancel(ctx)
		said := make(chan bool, 1)
		go func() {
			said <- n.ha.WaitState(watch, sat, time.Hour, func(state string) bool { return state == "responding" })
		}()
		err := n.ha.CallService(ctx, "assist_satellite", "start_conversation",
			map[string]any{"start_message": text, "preannounce": preannounce}, target)
		stop()
		spoke := <-said
		// The call returns only once the satellite has stopped listening,
		// after the reply. At its deadline the satellite's state decides:
		// it spoke when it was responding, else nothing was said
		// (docs/spec/fallbacks.md, row 10).
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			if spoke {
				n.log.Info("start_conversation still listening after speaking, counted as spoken", "satellite", sat)
				return nil
			}
			return fmt.Errorf("start_conversation on %s ran out of time and the satellite did not speak: %w", sat, err)
		}
		return err
	default:
		return n.ha.CallService(ctx, "assist_satellite", "announce",
			map[string]any{"message": text, "preannounce": preannounce}, target)
	}
}

// Feature bits of assist_satellite entities.
const (
	featureAnnounce          = 1
	featureStartConversation = 2
)

// satelliteMethod: start_conversation when the satellite can listen after
// speaking, else announce. A state that cannot be read is an error, no
// announcement that does not listen (docs/spec/fallbacks.md, row 9).
func (n *Notifier) satelliteMethod(ctx context.Context, sat string) (string, error) {
	states, err := n.ha.GetStates(ctx, haws.StateFilter{EntityIDs: []string{sat}})
	if err != nil {
		return "", fmt.Errorf("the state of %s: %w", sat, err)
	}
	for _, s := range states {
		if f, ok := s.Attributes["supported_features"].(float64); ok && int(f)&featureStartConversation != 0 {
			return "start_conversation", nil
		}
	}
	return "announce", nil
}

// primary is the area's primary satellite, "" without one.
func (n *Notifier) primary(area string) string {
	if sats := n.sats[area]; len(sats) > 0 {
		return sats[0]
	}
	return ""
}

// satOn is the area's satellite on device, else its primary: what was
// asked for at a satellite is spoken there while it is still in the room.
func (n *Notifier) satOn(ctx context.Context, area, device string) string {
	if device != "" {
		for _, sat := range n.sats[area] {
			if n.deviceID(ctx, sat) == device {
				return sat
			}
		}
	}
	return n.primary(area)
}

// deviceID is the device of a satellite entity, for linking a follow-up
// question to what was announced there.
func (n *Notifier) deviceID(_ context.Context, entityID string) string {
	if n.structure == nil {
		return ""
	}
	return n.structure().Entities[entityID].DeviceID
}

// sendPrivate delivers a message to its recipients over the first private
// channel that works for each of them.
// privatePolicy bounds the attempts of a private delivery
// (docs/spec/retry.md, row 1): 1, 2, 4 … 256 minutes apart, about 8.5 hours
// in all; the message's expiry ends it earlier.
var privatePolicy = retry.Policy{Attempts: 10, First: time.Minute, Growth: retry.Exponential}

// deliverPrivate sends a message over its receiver's channels once its next
// attempt is due, and keeps a failure: tried again later when the error is
// retryable, failed when it is fatal or the attempts are used up. It reports
// whether the message was delivered.
func (n *Notifier) deliverPrivate(ctx context.Context, nt store.Message) bool {
	now := n.clock()
	if !nt.RetryAt.IsZero() && now.Before(nt.RetryAt) {
		return false
	}
	err := n.sendPrivate(ctx, nt)
	if err == nil {
		return true
	}
	n.privateFailed(ctx, nt, now, err)
	return false
}

// errNotDue stands for a private delivery whose next attempt has not come.
var errNotDue = errors.New("the next attempt has not come")

// privateFailed keeps a failed private delivery and logs it: tried again
// later, given up, or failed for good.
func (n *Notifier) privateFailed(ctx context.Context, nt store.Message, now time.Time, err error) {
	failed := nt.Attempts + 1
	next, stop := privatePolicy.Next(now, failed, err)
	if _, serr := n.store.PrivateDeliveryFailed(ctx, nt.ID, next); serr != nil {
		n.log.Error("could not keep the failed private delivery; it is tried at the next tick", "message", nt.ID, "error", serr)
	}
	switch {
	case stop == nil:
		n.log.Warn("private delivery failed, trying again", "message", nt.ID, "attempt", failed,
			"of", privatePolicy.Attempts, "next", next.In(n.loc).Format("15:04"), "error", err)
	case retry.Retryable(err):
		n.log.Error("private delivery given up", "message", nt.ID, "attempts", failed, "error", err)
	default:
		n.log.Error("private delivery failed", "message", nt.ID, "fatal", true, "error", err)
	}
}

func (n *Notifier) sendPrivate(ctx context.Context, nt store.Message) error {
	text := nt.Spoken
	if nt.Details != "" {
		text += "\n\n" + nt.Details
	}
	var recipients []string
	if nt.Receiver == store.ReceiverHouse {
		for id := range n.persons {
			recipients = append(recipients, id)
		}
	} else {
		recipients = []string{nt.Receiver}
	}

	var used []string
	if len(recipients) == 0 {
		return retry.Fatal(errors.New("no private channel: no persons"))
	}
	var errs []error
	for _, id := range recipients {
		via, err := n.sendTo(ctx, id, text)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		used = append(used, via)
	}
	if len(used) == 0 {
		return retry.Join(errs...)
	}
	if len(errs) > 0 {
		n.log.Warn("private delivery partly failed", "message", nt.ID, "via", strings.Join(used, ","), "error", errors.Join(errs...))
	}
	n.tel.RecordMessageDelivery(ctx, used[0], 1)
	return n.store.MarkDelivered(ctx, nt.ID, strings.Join(dedupe(used), ","))
}

// sendTo sends text to the first of the person's channels that works
// (docs/spec/channels.md): a notify entity (notify.send_message), or a
// legacy notify service such as notify.mobile_app_anna.
func (n *Notifier) sendTo(ctx context.Context, personID, text string) (string, error) {
	p, ok := n.persons[personID]
	if !ok {
		return "", retry.Fatal(fmt.Errorf("unknown person"))
	}
	if len(p.Channels) == 0 {
		return "", retry.Fatal(errors.New("no private channel configured"))
	}
	// A channel that is a notify entity is sent to as one; the others are
	// legacy notify services.
	entities := map[string]bool{}
	if states, err := n.ha.GetStates(ctx, haws.StateFilter{EntityIDs: p.Channels}); err == nil {
		for _, s := range states {
			if strings.HasPrefix(s.EntityID, "notify.") {
				entities[s.EntityID] = true
			}
		}
	}
	var errs []error
	for _, ch := range p.Channels {
		var err error
		if entities[ch] {
			err = n.ha.CallService(ctx, "notify", "send_message", map[string]any{"title": "Mira", "message": text},
				&haws.Target{EntityID: []string{ch}})
		} else {
			err = n.ha.CallService(ctx, "notify", strings.TrimPrefix(ch, "notify."), map[string]any{"title": "Mira", "message": text}, nil)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ch, err))
			continue
		}
		return ch, nil
	}
	return "", retry.Join(errs...)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// langOf is the language of what is being said now: the one ctx carries,
// otherwise the one last spoken.
func (n *Notifier) langOf(ctx context.Context) i18n.Lang {
	if l, ok := i18n.FromContext(ctx); ok {
		return l
	}
	if n.langs != nil {
		return n.langs.Last(ctx)
	}
	return i18n.Default
}

// inArea carries the language last spoken in an area, for what is said
// there.
func (n *Notifier) inArea(ctx context.Context, area string) context.Context {
	if n.langs == nil {
		return ctx
	}
	return i18n.WithLang(ctx, n.langs.ForArea(ctx, area))
}

// forMessage carries the language for a message asked for: its area's,
// otherwise its receiver's.
func (n *Notifier) forMessage(ctx context.Context, m store.Message) context.Context {
	switch {
	case n.langs == nil:
		return ctx
	case m.AreaID != "":
		return n.inArea(ctx, m.AreaID)
	case m.Receiver != "" && m.Receiver != store.ReceiverHouse:
		return i18n.WithLang(ctx, n.langs.ForPerson(ctx, m.Receiver))
	}
	return ctx
}
