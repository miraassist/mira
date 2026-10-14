// Package notifier decides when, where and how pending notices reach a
// person. It is deterministic code: the wording was produced when the notice
// was created, so delivery never waits for the model.
//
// Voice delivery happens on presence: the first motion in a configured area
// outside quiet hours and outside the area's cooldown announces up to
// MaxItemsPerAnnouncement notices on that area's satellite. The morning rule
// of the plan is exactly that: quiet hours end, someone walks into the
// bathroom. Notices that must not be spoken right then go to a private
// channel instead, and important ones that no satellite delivered in time
// escalate to one.
package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sker65/lares/internal/config"
	haws "github.com/sker65/lares/internal/ha/ws"
	"github.com/sker65/lares/internal/store"
	"github.com/sker65/lares/internal/telemetry"
)

// HA is the part of the Home Assistant client the notifier needs.
type HA interface {
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
	States(ctx context.Context) ([]haws.State, error)
	Entities(ctx context.Context) ([]haws.Entity, error)
}

// Telegram sends private messages.
type Telegram interface {
	Send(ctx context.Context, chatID int64, text string) error
	SendToAll(ctx context.Context, text string) error
}

type Options struct {
	Config    config.Config
	Store     *store.Store
	HA        HA
	Telegram  Telegram // nil when Telegram is off
	Clock     func() time.Time
	Location  *time.Location
	Logger    *slog.Logger
	Telemetry *telemetry.Provider
}

type Notifier struct {
	cfg      config.Notifier
	sats     map[string]string
	persons  map[string]config.Person
	store    *store.Store
	ha       HA
	telegram Telegram
	clock    func() time.Time
	loc      *time.Location
	log      *slog.Logger
	tel      *telemetry.Provider
	areas    map[string]bool

	// mu serialises decisions, so two motion events a second apart cannot
	// both announce the same notices.
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
		cfg:      opts.Config.Notifier,
		sats:     opts.Config.Satellites,
		persons:  opts.Config.Persons,
		store:    opts.Store,
		ha:       opts.HA,
		telegram: opts.Telegram,
		clock:    opts.Clock,
		loc:      opts.Location,
		log:      opts.Logger.With("component", "notifier"),
		tel:      opts.Telemetry,
		areas:    map[string]bool{},
		deviceOf: map[string]string{},
	}
	for _, a := range opts.Config.Notifier.MorningTrigger.Areas {
		n.areas[a] = true
	}
	return n
}

// Outcome of OnPresence, for logs and tests.
type Outcome struct {
	Skipped   string   // why nothing was announced, if nothing was
	Announced []string // notice ids spoken
	Private   []string // notice ids sent privately instead
}

// OnPresence handles motion in an area.
func (n *Notifier) OnPresence(ctx context.Context, area string) (Outcome, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.areas[area] {
		return Outcome{Skipped: "area not configured"}, nil
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

	var voice, private []store.Notice
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
			n.log.Error("private delivery failed", "notice", nt.ID, "error", err)
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
	if err := n.announce(ctx, area, voice); err != nil {
		return out, err
	}
	for _, nt := range voice {
		out.Announced = append(out.Announced, nt.ID)
	}
	return out, nil
}

// Tick runs the time-driven rules: expiry, confidential notices that are
// never spoken, and escalation. Call it about once a minute.
func (n *Notifier) Tick(ctx context.Context) ([]string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, err := n.store.ExpireNotices(ctx); err != nil {
		return nil, err
	}
	now := n.clock()
	if n.inQuietHours(now) {
		return nil, nil
	}
	pending, err := n.pending(ctx)
	if err != nil {
		return nil, err
	}
	esc := n.cfg.EscalateToTelegram
	local := now.In(n.loc)
	afterToday := local.Hour()*60+local.Minute() >= esc.After.MinutesOfDay()

	var sent []string
	for _, nt := range pending {
		neverSpoken := nt.Confidential() && n.cfg.ConfidentialVoice == config.ConfidentialVoiceNever
		escalate := nt.Importance >= esc.MinImportance && afterToday &&
			now.Sub(nt.CreatedAt) >= esc.Grace.Duration()
		if !neverSpoken && !escalate {
			continue
		}
		if err := n.sendPrivate(ctx, nt); err != nil {
			n.log.Error("private delivery failed", "notice", nt.ID, "error", err)
			continue
		}
		reason := "escalation"
		if neverSpoken {
			reason = "confidential"
		}
		n.log.Info("notice delivered privately", "notice", nt.ID, "reason", reason)
		sent = append(sent, nt.ID)
	}
	return sent, nil
}

func (n *Notifier) pending(ctx context.Context) ([]store.Notice, error) {
	return n.store.ListNotices(ctx, store.NoticeFilter{
		States: []string{store.NoticePending}, MinImportance: n.cfg.MinImportance,
	})
}

type route int

const (
	routeWait route = iota
	routeVoice
	routePrivate
)

// route decides what happens to a notice when someone is in a room with a
// satellite. Nobody knows who that someone is; the person entities only say
// who is home.
func (n *Notifier) route(nt store.Notice, home map[string]bool) route {
	if nt.Audience != store.AudienceHousehold {
		if p, ok := n.persons[nt.Audience]; ok && p.Entity != "" && !home[nt.Audience] {
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
		if nt.Audience != store.AudienceHousehold {
			alone = alone && home[nt.Audience]
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
		case "":
			home["?"+id] = true // unknown: assume someone might be there
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

// Compose turns notices into one spoken announcement.
func Compose(notices []store.Notice, now time.Time, quietEnd config.ClockTime) string {
	var b strings.Builder
	h := now.Hour()
	if h*60+now.Minute() >= quietEnd.MinutesOfDay() && h < 11 {
		b.WriteString("Guten Morgen. ")
	}
	if len(notices) == 1 {
		b.WriteString(sentence(notices[0].Spoken))
		return b.String()
	}
	fmt.Fprintf(&b, "Ich habe %s Hinweise. ", countWord(len(notices)))
	for i, nt := range notices {
		switch {
		case i == 0:
		case i == len(notices)-1:
			b.WriteString(" Und: ")
		default:
			b.WriteString(" Außerdem: ")
		}
		b.WriteString(sentence(nt.Spoken))
	}
	return b.String()
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s != "" && !strings.ContainsAny(s[len(s)-1:], ".!?") {
		s += "."
	}
	return s
}

func countWord(n int) string {
	words := []string{"null", "einen", "zwei", "drei", "vier", "fünf"}
	if n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}

func (n *Notifier) announce(ctx context.Context, area string, notices []store.Notice) error {
	sat := n.sats[area]
	text := Compose(notices, n.clock().In(n.loc), n.cfg.QuietHours.To)
	method, err := n.deliver(ctx, sat, text)
	if err != nil {
		return fmt.Errorf("announce in %s on %s: %w", area, sat, err)
	}
	ids := make([]string, len(notices))
	for i, nt := range notices {
		ids[i] = nt.ID
		if err := n.store.MarkDelivered(ctx, nt.ID, "voice:"+area); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	err = n.store.RecordAnnouncement(ctx, store.Announcement{
		ID: ulid.Make().String(), DeviceID: n.deviceID(ctx, sat), AreaID: area,
		Channel: "voice", NoticeIDs: ids,
	})
	if err != nil {
		return err
	}
	n.tel.RecordNoticeDelivery(ctx, "voice", len(ids))
	n.log.Info("announced", "area", area, "satellite", sat, "method", method, "notices", len(ids))
	return nil
}

// deliver speaks text on a satellite and reports the method used.
func (n *Notifier) deliver(ctx context.Context, sat, text string) (string, error) {
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
		// integration and finds the notices through the announcement record.
		return method, n.ha.CallService(ctx, "assist_satellite", "start_conversation",
			map[string]any{"start_message": text, "preannounce": true}, target)
	default:
		return "announce", n.ha.CallService(ctx, "assist_satellite", "announce",
			map[string]any{"message": text, "preannounce": true}, target)
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

// sendPrivate delivers a notice to its recipients over the first private
// channel that works for each of them.
func (n *Notifier) sendPrivate(ctx context.Context, nt store.Notice) error {
	text := nt.Spoken
	if nt.Details != "" {
		text += "\n\n" + nt.Details
	}
	var recipients []string
	if nt.Audience == store.AudienceHousehold {
		for id := range n.persons {
			recipients = append(recipients, id)
		}
	} else {
		recipients = []string{nt.Audience}
	}

	var used []string
	if len(recipients) == 0 {
		// No persons configured: everyone on the Telegram allowlist.
		if n.telegram == nil {
			return errors.New("no private channel: no persons configured and Telegram is off")
		}
		if err := n.telegram.SendToAll(ctx, text); err != nil {
			return err
		}
		used = append(used, "telegram")
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
	n.tel.RecordNoticeDelivery(ctx, used[0], 1)
	return n.store.MarkDelivered(ctx, nt.ID, strings.Join(dedupe(used), ","))
}

func (n *Notifier) sendTo(ctx context.Context, personID, text string) (string, error) {
	p, ok := n.persons[personID]
	if !ok {
		return "", fmt.Errorf("unknown person")
	}
	var errs []error
	for _, via := range n.cfg.PrivateVia {
		switch {
		case via == "telegram" && p.TelegramUserID != 0 && n.telegram != nil:
			if err := n.telegram.Send(ctx, p.TelegramUserID, text); err != nil {
				errs = append(errs, err)
				continue
			}
			return via, nil
		case via == "push" && p.Notify != "":
			service := strings.TrimPrefix(p.Notify, "notify.")
			if err := n.ha.CallService(ctx, "notify", service, map[string]any{"title": "Lares", "message": text}, nil); err != nil {
				errs = append(errs, err)
				continue
			}
			return via, nil
		}
	}
	if len(errs) == 0 {
		return "", errors.New("no private channel configured")
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
