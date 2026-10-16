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

	"github.com/sker65/mira/internal/config"
	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/telemetry"
)

// HA is the part of the Home Assistant client the notifier needs.
type HA interface {
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
	States(ctx context.Context) ([]haws.State, error)
	Entities(ctx context.Context) ([]haws.Entity, error)
}

type Options struct {
	Config    config.Config
	Store     *store.Store
	HA        HA
	Clock     func() time.Time
	Location  *time.Location
	Logger    *slog.Logger
	Telemetry *telemetry.Provider
	// Composer words several messages as one announcement (compose.go).
	Composer Composer
}

type Notifier struct {
	// indicatorOn is the last state set per area; missing means unknown.
	indicatorOn map[string]bool
	lang        i18n.Lang
	cfg         config.Notifier
	sats        map[string]string
	persons     map[string]config.Person
	store       *store.Store
	ha          HA
	clock       func() time.Time
	loc         *time.Location
	log         *slog.Logger
	tel         *telemetry.Provider
	composer    Composer
	areas       map[string]bool

	// idlePoll is how often waitFor looks at a satellite.
	idlePoll time.Duration
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
	// deviceOf caches satellite entity -> device id, for follow-up linking.
	deviceOf map[string]string
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
		idlePoll:      500 * time.Millisecond,
		speakingIn:    map[string]bool{},
		speakingStart: 15 * time.Second,
		speakPause:    1500 * time.Millisecond,
		lang:          i18n.Of(opts.Config.Prompt.Language),
		cfg:           opts.Config.Notifier,
		sats:          opts.Config.Satellites,
		persons:       opts.Config.Persons,
		store:         opts.Store,
		ha:            opts.HA,
		clock:         opts.Clock,
		loc:           opts.Location,
		log:           opts.Logger.With("component", "notifier"),
		tel:           opts.Telemetry,
		composer:      opts.Composer,
		areas:         map[string]bool{},
		indicatorOn:   map[string]bool{},
		deviceOf:      map[string]string{},
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

// OnPresence handles motion in an area.
func (n *Notifier) OnPresence(ctx context.Context, area string) (Outcome, error) {
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
			voice = append(voice, nt)
		case routePrivate:
			private = append(private, nt)
		}
	}

	var out Outcome
	for _, nt := range private {
		if err := n.sendPrivate(ctx, nt); err != nil {
			n.log.Error("private delivery failed", "message", nt.ID, "error", err)
			continue
		}
		out.Private = append(out.Private, nt.ID)
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
	if err := n.announce(ctx, area, voice, true); err != nil {
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
		neverSpoken := nt.Confidential() && n.cfg.ConfidentialVoice == config.ConfidentialVoiceNever
		escalate := nt.Importance >= esc.MinImportance && afterToday &&
			now.Sub(nt.CreatedAt) >= esc.Grace.Duration()
		if !neverSpoken && !escalate {
			continue
		}
		if err := n.sendPrivate(ctx, nt); err != nil {
			n.log.Error("private delivery failed", "message", nt.ID, "error", err)
			continue
		}
		reason := "escalation"
		if neverSpoken {
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
	for _, nt := range pending {
		if nt.Importance < urgent.MinImportance {
			continue
		}
		privErr := n.sendPrivate(ctx, nt)
		if privErr != nil {
			n.log.Error("urgent private delivery failed", "message", nt.ID, "error", privErr)
		}
		spoken := false
		if urgent.Announce && !nt.Confidential() {
			spoken = n.announceEverywhere(ctx, nt)
		}
		if privErr == nil || spoken {
			n.log.Warn("urgent message delivered", "message", nt.ID, "spoken", spoken, "private", privErr == nil)
			sent = append(sent, nt.ID)
		}
	}
	return sent, nil
}

// announceEverywhere speaks one message on each satellite once.
func (n *Notifier) announceEverywhere(ctx context.Context, nt store.Message) bool {
	done := map[string]bool{}
	spoken := false
	for _, area := range sortedKeys(n.sats) {
		sat := n.sats[area]
		if done[sat] {
			continue
		}
		done[sat] = true
		if err := n.announce(ctx, area, []store.Message{nt}, true); err != nil {
			n.log.Error("urgent announcement failed", "area", area, "error", err)
			continue
		}
		spoken = true
	}
	return spoken
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (n *Notifier) pending(ctx context.Context) ([]store.Message, error) {
	return n.store.ListMessages(ctx, store.MessageFilter{
		States: []string{store.MessagePending}, MinImportance: n.cfg.MinImportance,
	})
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
	states, err := n.ha.States(ctx)
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
		prefixToday: "Um %s: ", prefixNight: "Heute Nacht um %s: ", prefixYesterday: "Gestern um %s: ", prefixDate: "Am %s um %s: "},
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
		prefixToday: "At %s: ", prefixNight: "Tonight at %s: ", prefixYesterday: "Yesterday at %s: ", prefixDate: "On %s at %s: "},
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
// for in that room does without it.
func (n *Notifier) announce(ctx context.Context, area string, messages []store.Message, preannounce bool) error {
	sat := n.sats[area]
	text := n.compose(ctx, messages)
	n.speaking(ctx, area)
	method, err := n.deliver(ctx, sat, text, preannounce)
	if err != nil {
		return fmt.Errorf("announce in %s on %s: %w", area, sat, err)
	}
	ids := make([]string, len(messages))
	for i, nt := range messages {
		ids[i] = nt.ID
		if err := n.store.MarkDelivered(ctx, nt.ID, "voice:"+area); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	err = n.store.RecordAnnouncement(ctx, store.Announcement{
		ID: ulid.Make().String(), DeviceID: n.deviceID(ctx, sat), AreaID: area,
		Channel: "voice", MessageIDs: ids,
	})
	if err != nil {
		return err
	}
	n.tel.RecordMessageDelivery(ctx, "voice", len(ids))
	n.log.Info("announced", "area", area, "satellite", sat, "method", method, "messages", len(ids))
	return nil
}

// deliver speaks text on a satellite and reports the method used.
func (n *Notifier) deliver(ctx context.Context, sat, text string, preannounce bool) (string, error) {
	if strings.HasPrefix(sat, "media_player.") || n.cfg.Delivery == "tts" {
		return "tts", n.ha.CallService(ctx, "tts", "speak",
			map[string]any{"media_player_entity_id": sat, "message": text},
			&haws.Target{EntityID: []string{n.cfg.TTSEntity}})
	}
	method := n.cfg.Delivery
	if method == "auto" {
		method = n.satelliteMethod(ctx, sat)
	}
	target := &haws.Target{EntityID: []string{sat}}
	switch method {
	case "start_conversation":
		// The satellite listens afterwards; the reply comes back through the
		// integration and finds the messages through the announcement record.
		err := n.ha.CallService(ctx, "assist_satellite", "start_conversation",
			map[string]any{"start_message": text, "preannounce": preannounce}, target)
		// The call returns only once the satellite has stopped listening,
		// after the reply: a timeout means it spoke and is still listening.
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			n.log.Info("start_conversation still listening, counted as spoken", "satellite", sat)
			return method, nil
		}
		return method, err
	default:
		return "announce", n.ha.CallService(ctx, "assist_satellite", "announce",
			map[string]any{"message": text, "preannounce": preannounce}, target)
	}
}

// Feature bits of assist_satellite entities.
const (
	featureAnnounce          = 1
	featureStartConversation = 2
)

func (n *Notifier) satelliteMethod(ctx context.Context, sat string) string {
	states, err := n.ha.States(ctx)
	if err != nil {
		return "announce"
	}
	for _, s := range states {
		if s.EntityID != sat {
			continue
		}
		if f, ok := s.Attributes["supported_features"].(float64); ok && int(f)&featureStartConversation != 0 {
			return "start_conversation"
		}
	}
	return "announce"
}

func (n *Notifier) deviceID(ctx context.Context, entityID string) string {
	if id, ok := n.deviceOf[entityID]; ok {
		return id
	}
	entities, err := n.ha.Entities(ctx)
	if err != nil {
		n.log.Warn("could not resolve the satellite's device; follow-up questions will not be linked", "error", err)
		return ""
	}
	for _, e := range entities {
		n.deviceOf[e.EntityID] = e.DeviceID
	}
	return n.deviceOf[entityID]
}

// sendPrivate delivers a message to its recipients over the first private
// channel that works for each of them.
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
		return errors.New("no private channel: no persons")
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
		return errors.Join(errs...)
	}
	n.tel.RecordMessageDelivery(ctx, used[0], 1)
	return n.store.MarkDelivered(ctx, nt.ID, strings.Join(dedupe(used), ","))
}

// sendTo sends text to the first of the person's channels that works
// (docs/spec/kanaele.md): a notify entity (notify.send_message), or a
// legacy notify service such as notify.mobile_app_anna.
func (n *Notifier) sendTo(ctx context.Context, personID, text string) (string, error) {
	p, ok := n.persons[personID]
	if !ok {
		return "", fmt.Errorf("unknown person")
	}
	if len(p.Channels) == 0 {
		return "", errors.New("no private channel configured")
	}
	entities := map[string]bool{}
	if states, err := n.ha.States(ctx); err == nil {
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
	return "", errors.Join(errs...)
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
