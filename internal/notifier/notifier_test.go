package notifier

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/store"
)

type call struct {
	Domain, Service string
	Data            map[string]any
	Target          *haws.Target
}

type fakeHA struct {
	mu     sync.Mutex
	calls  []call
	states map[string]haws.State
	// callErr, when set, is what service calls return.
	callErr error
	// private records what went to notify targets (a person's channels),
	// apart from calls: those are what was spoken or switched.
	private *fakePrivate
	// changed is closed by the next set: what WaitState waits on.
	changed chan struct{}
	// listening, when set, holds start_conversation as the satellite does
	// until the reply: one receive when it starts, one send to end it.
	listening chan struct{}
}

func (f *fakeHA) CallService(_ context.Context, domain, service string, data map[string]any, target *haws.Target) error {
	if service == "start_conversation" && f.listening != nil {
		f.listening <- struct{}{} // the satellite speaks and listens until told
		<-f.listening
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if domain == "notify" && f.private != nil {
		channel := "notify." + service
		if service == "send_message" && target != nil && len(target.EntityID) > 0 {
			channel = target.EntityID[0]
		}
		f.private.add(channel, data["message"].(string))
		return nil
	}
	f.calls = append(f.calls, call{domain, service, data, target})
	return f.callErr
}

// GetStates filters as Home Assistant does: only the entities asked for.
func (f *fakeHA) GetStates(_ context.Context, filter haws.StateFilter) ([]haws.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []haws.State
	for _, s := range f.states {
		domain, _, _ := strings.Cut(s.EntityID, ".")
		if slices.Contains(filter.EntityIDs, s.EntityID) || slices.Contains(filter.Domains, domain) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeHA) Entities(context.Context) ([]haws.Entity, error) {
	return []haws.Entity{
		{EntityID: "assist_satellite.bad", DeviceID: "dev-bad"},
		{EntityID: "assist_satellite.kueche", DeviceID: "dev-kueche"},
		{EntityID: "media_player.kueche", DeviceID: "dev-kueche"},
	}, nil
}

func (f *fakeHA) set(entity, state string, attrs map[string]any) {
	f.mu.Lock()
	f.states[entity] = haws.State{EntityID: entity, State: state, Attributes: attrs}
	if f.changed != nil {
		close(f.changed)
		f.changed = nil
	}
	f.mu.Unlock()
}

// WaitState is Home Assistant's subscription, faked: every set wakes it.
func (f *fakeHA) WaitState(ctx context.Context, entity string, limit time.Duration, ok func(string) bool) bool {
	deadline := time.After(limit)
	for {
		f.mu.Lock()
		state := f.states[entity].State
		if f.changed == nil {
			f.changed = make(chan struct{})
		}
		changed := f.changed
		f.mu.Unlock()
		if ok(state) {
			return true
		}
		select {
		case <-changed:
		case <-deadline:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

func (f *fakeHA) servicesCalled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Domain+"."+c.Service)
	}
	return out
}

// fakePrivate is what reached a person's channels, by notify target.
type fakePrivate struct {
	mu   sync.Mutex
	sent map[string][]string
}

func (f *fakePrivate) add(channel, text string) {
	f.mu.Lock()
	f.sent[channel] = append(f.sent[channel], text)
	f.mu.Unlock()
}

type env struct {
	n   *Notifier
	st  *store.Store
	ha  *fakeHA
	tg  *fakePrivate
	now time.Time
	t   *testing.T
}

func at(hhmm string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04", "2026-10-14 "+hhmm)
	return t
}

func newEnv(t *testing.T, mutate func(*config.Config)) *env {
	t.Helper()
	cfg := config.Default()
	cfg.Satellites = map[string]string{"bad": "assist_satellite.bad", "kueche": "assist_satellite.kueche"}
	cfg.Notifier.MorningTrigger = config.MorningTrigger{
		Areas:    []string{"bad", "kueche"},
		Presence: map[string]config.Entities{"bad": {"binary_sensor.bad_motion"}, "kueche": {"binary_sensor.kueche_motion"}},
	}
	cfg.Persons = map[string]config.Person{
		"ben": {Entity: "person.ben", Channels: []string{"notify.ben"}},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	e := &env{t: t, now: at("03:00"), tg: &fakePrivate{sent: map[string][]string{}}}
	e.ha = &fakeHA{states: map[string]haws.State{}, private: e.tg}
	e.ha.set("assist_satellite.bad", "idle", map[string]any{"supported_features": float64(3)})
	e.ha.set("assist_satellite.kueche", "idle", map[string]any{"supported_features": float64(1)})
	e.ha.set("person.ben", "home", nil)

	var err error
	e.st, err = store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return e.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	// The registries as Mira keeps them: the fake's devices and entities.
	entities, _ := e.ha.Entities(context.Background())
	snap := structure.Build(structure.Registry{Entities: entities})
	e.n = New(Options{Config: cfg, Store: e.st, HA: e.ha, Clock: func() time.Time { return e.now },
		Location: time.UTC, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Structure: func() structure.Snapshot { return snap }})
	e.n.speakPause = 0
	return e
}

func (e *env) message(id string, importance int, mutate func(*store.Message)) {
	e.t.Helper()
	n := store.Message{ID: id, Source: store.SourceHATrigger, Importance: importance,
		Spoken: "Hinweis " + id, Details: "Details " + id, ExpiresAt: e.now.Add(48 * time.Hour)}
	if mutate != nil {
		mutate(&n)
	}
	if _, _, err := e.st.PutMessage(context.Background(), n); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) presence(area string) Outcome {
	e.t.Helper()
	out, err := e.n.OnPresence(context.Background(), area)
	if err != nil {
		e.t.Fatalf("OnPresence(%s): %v", area, err)
	}
	return out
}

func (e *env) tick() []string {
	e.t.Helper()
	sent, err := e.n.Tick(context.Background())
	if err != nil {
		e.t.Fatalf("Tick: %v", err)
	}
	return sent
}

func (e *env) state(id string) string {
	n, err := e.st.GetMessage(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return n.State
}

// The plan's acceptance case: a fault at night, the first motion in the
// bathroom after quiet hours leads to exactly one announcement.
func TestNightFaultIsAnnouncedOnceOnFirstMorningMotion(t *testing.T) {
	e := newEnv(t, nil)
	e.message("inverter", 2, func(n *store.Message) { n.Spoken = "Der Wechselrichter meldet seit drei Uhr einen Fehler" })

	if out := e.presence("bad"); out.Skipped != "quiet hours" {
		t.Fatalf("03:00 motion: %+v", out)
	}
	e.now = at("06:45")
	out := e.presence("bad")
	if len(out.Announced) != 1 {
		t.Fatalf("06:45 motion: %+v", out)
	}
	calls := e.ha.calls
	if len(calls) != 1 || calls[0].Service != "start_conversation" || calls[0].Target.EntityID[0] != "assist_satellite.bad" {
		t.Fatalf("calls = %+v", calls)
	}
	if got := calls[0].Data["start_message"]; got != "Guten Morgen. Der Wechselrichter meldet seit drei Uhr einen Fehler." {
		t.Errorf("spoken %q", got)
	}
	if e.state("inverter") != store.MessageDelivered {
		t.Error("message not marked delivered")
	}
	a, err := e.st.RecentAnnouncement(context.Background(), "dev-bad", 2*time.Minute)
	if err != nil || len(a.MessageIDs) != 1 || a.MessageIDs[0] != "inverter" {
		t.Fatalf("announcement for follow-up = %+v, %v", a, err)
	}

	// More motion right after, here and next door: nothing more to say.
	e.now = at("06:46")
	if out := e.presence("bad"); len(out.Announced) != 0 {
		t.Fatalf("second motion announced again: %+v", out)
	}
	if out := e.presence("kueche"); len(out.Announced) != 0 {
		t.Fatalf("kitchen repeated the message: %+v", out)
	}
	if n := len(e.ha.calls); n != 1 {
		t.Fatalf("%d service calls, want exactly one announcement", n)
	}
}

func TestPresenceRules(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(e *env)
		area    string
		want    string // Skipped reason, or "announce"
		service string
	}{
		{"quiet hours wrap midnight", func(e *env) { e.now = at("23:10") }, "bad", "quiet hours", ""},
		{"quiet mode", func(e *env) { e.st.SetBool(context.Background(), store.KeyQuietMode, true) }, "bad", "quiet mode", ""},
		{"unknown area", nil, "keller", "no announcing on entry", ""},
		{"nothing pending", func(e *env) { e.st.SetMessageState(context.Background(), "n1", store.MessageAcked) }, "bad", "nothing pending", ""},
		{"below min importance", func(e *env) {
			e.st.SetMessageState(context.Background(), "n1", store.MessageAcked)
			e.message("low", 0, nil)
		}, "bad", "nothing pending", ""},
		{"start_conversation when supported", nil, "bad", "announce", "assist_satellite.start_conversation"},
		{"announce when not", nil, "kueche", "announce", "assist_satellite.announce"},
		{"expired message", func(e *env) {
			e.now = at("12:00")
			e.st.ExpireMessages(context.Background())
			e.now = at("07:00")
			e.st.SetMessageState(context.Background(), "n1", store.MessageExpired)
		}, "bad", "nothing pending", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, nil)
			e.message("n1", 2, nil)
			e.now = at("07:00")
			if c.setup != nil {
				c.setup(e)
			}
			out := e.presence(c.area)
			if c.want == "announce" {
				if len(out.Announced) != 1 {
					t.Fatalf("not announced: %+v", out)
				}
				if got := e.ha.servicesCalled(); len(got) != 1 || got[0] != c.service {
					t.Fatalf("services = %v, want %s", got, c.service)
				}
				return
			}
			if out.Skipped != c.want || len(e.ha.calls) != 0 {
				t.Fatalf("outcome = %+v, calls %v, want skip %q", out, e.ha.servicesCalled(), c.want)
			}
		})
	}
}

func TestCooldownPerArea(t *testing.T) {
	e := newEnv(t, nil)
	e.now = at("07:00")
	e.message("a", 2, nil)
	e.presence("bad")
	e.now = at("07:10")
	e.message("b", 2, nil)
	if out := e.presence("bad"); out.Skipped != "cooldown" {
		t.Fatalf("within cooldown: %+v", out)
	}
	if out := e.presence("kueche"); len(out.Announced) != 1 {
		t.Fatalf("other area has no cooldown: %+v", out)
	}
	e.now = at("07:31")
	e.message("c", 2, nil)
	if out := e.presence("bad"); len(out.Announced) != 1 || out.Announced[0] != "c" {
		t.Fatalf("after cooldown: %+v", out)
	}
}

func TestBundlingTakesTheMostImportantThree(t *testing.T) {
	e := newEnv(t, nil)
	e.message("n1", 1, nil)
	e.message("n2", 3, nil)
	e.message("n3", 2, nil)
	e.message("n4", 1, nil)
	e.message("n5", 2, nil)
	e.now = at("12:00")
	out := e.presence("bad")
	if got := strings.Join(out.Announced, ","); got != "n2,n3,n5" {
		t.Fatalf("announced %s", got)
	}
	text := e.ha.calls[0].Data["start_message"].(string)
	if text != "Ich habe drei Nachrichten. Hinweis n2. Außerdem: Hinweis n3. Und: Hinweis n5." {
		t.Errorf("text = %q", text)
	}
	if e.state("n1") != store.MessagePending || e.state("n4") != store.MessagePending {
		t.Error("the rest must stay pending for later")
	}
}

func TestMediaPlayerSatelliteUsesTTS(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Satellites["bad"] = "media_player.echo_bad"
		c.Notifier.TTSEntity = "tts.piper"
	})
	e.message("n1", 2, nil)
	e.now = at("08:00")
	e.presence("bad")
	c := e.ha.calls[0]
	if c.Domain+"."+c.Service != "tts.speak" || c.Target.EntityID[0] != "tts.piper" || c.Data["media_player_entity_id"] != "media_player.echo_bad" {
		t.Fatalf("call = %+v", c)
	}
}

func TestConfidentialMessages(t *testing.T) {
	confidential := func(n *store.Message) { n.Confidentiality = store.ConfidentialityConfidential }
	cases := []struct {
		name      string
		policy    string
		anna      string
		wantVoice bool
	}{
		{"never: private even when alone", config.ConfidentialVoiceNever, "not_home", false},
		{"when_alone and alone: spoken", config.ConfidentialVoiceWhenAlone, "not_home", true},
		{"when_alone with a second person home: private", config.ConfidentialVoiceWhenAlone, "home", false},
		{"when_alone with a second person unknown: private", config.ConfidentialVoiceWhenAlone, "unknown", false},
		{"always: spoken", config.ConfidentialVoiceAlways, "home", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, func(cfg *config.Config) {
				cfg.Notifier.ConfidentialVoice = c.policy
				cfg.Persons["anna"] = config.Person{Entity: "person.anna"}
			})
			e.ha.set("person.anna", c.anna, nil)
			e.message("secret", 2, func(n *store.Message) { confidential(n); n.Receiver = "ben" })
			e.now = at("08:00")
			out := e.presence("bad")
			spoken := len(out.Announced) == 1
			if spoken != c.wantVoice {
				t.Fatalf("spoken = %v, want %v (%+v)", spoken, c.wantVoice, out)
			}
			if !c.wantVoice && len(e.tg.sent["notify.ben"]) != 1 {
				t.Fatalf("not sent privately: %+v", e.tg.sent)
			}
		})
	}
}

func TestConfidentialNeverGoesOutOnTickWithoutWaitingForMotion(t *testing.T) {
	e := newEnv(t, nil) // default policy: never
	e.message("secret", 1, func(n *store.Message) { n.Confidentiality = store.ConfidentialityConfidential })
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("sent during quiet hours: %v", sent)
	}
	e.now = at("06:31")
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("sent = %v, telegram = %v", sent, e.tg.sent)
	}
	if !strings.Contains(e.tg.sent["notify.ben"][0], "Details secret") {
		t.Error("private message lacks the details")
	}
}

func TestPersonalMessageForUntrackedRecipientIsSpoken(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Persons["anna"] = config.Person{Entity: "person.anna", Channels: []string{"notify.anna"}}
	})
	e.ha.set("person.anna", "unknown", nil)
	e.message("for-anna", 1, func(n *store.Message) { n.Receiver = "anna" })
	e.now = at("08:00")
	if out := e.presence("bad"); len(out.Announced) != 1 {
		t.Fatalf("a person without tracker is not known to be away: %+v", out)
	}
}

func TestPersonalMessageWaitsWhileRecipientIsAway(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Persons["anna"] = config.Person{Entity: "person.anna", Channels: []string{"notify.anna"}}
	})
	e.ha.set("person.anna", "not_home", nil)
	e.message("for-anna", 1, func(n *store.Message) { n.Receiver = "anna" })
	e.now = at("08:00")
	out := e.presence("bad")
	if len(out.Announced)+len(out.Private) != 0 {
		t.Fatalf("delivered while anna is away: %+v", out)
	}
	e.ha.set("person.anna", "home", nil)
	e.now = at("08:40")
	if out := e.presence("bad"); len(out.Announced) != 1 {
		t.Fatalf("not delivered once anna is home: %+v", out)
	}
}

func TestEscalation(t *testing.T) {
	e := newEnv(t, nil)
	e.message("important", 2, nil)
	e.message("minor", 1, nil)
	e.now = at("08:59")
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("escalated before 09:00: %v", sent)
	}
	e.now = at("09:00")
	sent := e.tick()
	if len(sent) != 1 || sent[0] != "important" || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("escalation = %v, telegram %v", sent, e.tg.sent)
	}
	n, _ := e.st.GetMessage(context.Background(), "important")
	if n.State != store.MessageDelivered || n.DeliveredVia != "notify.ben" {
		t.Errorf("message after escalation = %+v", n)
	}
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("escalated twice: %v", sent)
	}
}

func TestEscalationGivesDaytimeMessagesAGracePeriod(t *testing.T) {
	e := newEnv(t, nil)
	e.now = at("14:00")
	e.message("afternoon", 2, nil)
	e.now = at("14:10")
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("escalated within grace: %v", sent)
	}
	e.now = at("14:31")
	if sent := e.tick(); len(sent) != 1 {
		t.Fatalf("not escalated after grace: %v", sent)
	}
}

// A person's channels are notify targets: a notify entity (such as a
// Telegram chat of HA's integration) is sent with notify.send_message, a
// legacy service is called directly; the first that works is used.
func TestPrivateGoesToTheFirstWorkingChannel(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Persons = map[string]config.Person{"ben": {Entity: "person.ben",
			Channels: []string{"notify.telegram_ben", "notify.mobile_app_phone"}}}
	})
	e.ha.set("notify.telegram_ben", "unknown", nil)
	e.message("n", 2, nil)
	e.now = at("09:30")
	e.tick()
	if got := e.tg.sent["notify.telegram_ben"]; len(got) != 1 || len(e.tg.sent["notify.mobile_app_phone"]) != 0 {
		t.Fatalf("sent = %v", e.tg.sent)
	}

	// Without persons there is no private channel; the message waits.
	e2 := newEnv(t, func(c *config.Config) { c.Persons = nil })
	e2.message("n", 2, nil)
	e2.now = at("09:30")
	e2.tick()
	if len(e2.tg.sent) != 0 {
		t.Fatalf("sent without persons: %v", e2.tg.sent)
	}
}

// Without a model: each message a sentence, an older one with its time.
func TestComposeFixed(t *testing.T) {
	e := newEnv(t, nil)
	e.now = at("15:00")
	one := []store.Message{{Spoken: "Die Waschmaschine ist fertig", CreatedAt: at("14:30")}}
	if got := e.n.compose(context.Background(), one); got != "Die Waschmaschine ist fertig." {
		t.Errorf("got %q", got)
	}
	old := []store.Message{{Spoken: "Die Waschmaschine ist fertig", CreatedAt: at("13:05"), Source: store.SourceExternal}}
	if got := e.n.compose(context.Background(), old); got != "Um 13:05: Die Waschmaschine ist fertig." {
		t.Errorf("got %q", got)
	}
	e.now = at("07:00")
	two := []store.Message{{Spoken: "A.", CreatedAt: at("03:13"), Source: store.SourceExternal}, {Spoken: "B!", CreatedAt: at("06:55")}}
	if got := e.n.compose(context.Background(), two); got != "Guten Morgen. Ich habe zwei Nachrichten. Heute Nacht um 3:13: A. Und: B!" {
		t.Errorf("got %q", got)
	}
}

// With a model: one announcement for all messages; one left out, and the
// fixed form is used instead.
func TestComposeWithModel(t *testing.T) {
	e := newEnv(t, nil)
	e.now = at("15:00")
	var asked string
	answer := `{"text":"Die Waschmaschine ist seit eins fertig, und morgen kommt der Wertstoff.","ids":["a","b"]}`
	e.n.composer = func(_ context.Context, instruction string, _ json.RawMessage) (string, error) {
		asked = instruction
		return answer, nil
	}
	two := []store.Message{{ID: "a", Spoken: "Die Waschmaschine ist fertig", CreatedAt: at("13:05")},
		{ID: "b", Spoken: "Morgen wird der Wertstoff abgeholt", CreatedAt: at("14:00")}}
	if got := e.n.compose(context.Background(), two); got != "Die Waschmaschine ist seit eins fertig, und morgen kommt der Wertstoff." {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(asked, "[a] heute 13:05") || !strings.Contains(asked, "Jetzt ist es 15:00 Uhr") {
		t.Errorf("instruction: %s", asked)
	}
	answer = `{"text":"Nur die Waschmaschine.","ids":["a"]}`
	if got := e.n.compose(context.Background(), two); !strings.HasPrefix(got, "Ich habe zwei Nachrichten.") {
		t.Errorf("a left-out message must fall back to the fixed form: %q", got)
	}
}

func TestUrgentMessageGoesOutAtOnceEvenAtNight(t *testing.T) {
	e := newEnv(t, nil) // 03:00, inside quiet hours
	e.message("leak", 3, nil)
	sent := e.tick()
	if len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("sent = %v, telegram = %v", sent, e.tg.sent)
	}
	var spoken []string
	for _, c := range e.ha.calls {
		spoken = append(spoken, c.Target.EntityID[0]+" "+c.Service)
	}
	want := []string{"assist_satellite.bad start_conversation", "assist_satellite.kueche announce"}
	if strings.Join(spoken, ",") != strings.Join(want, ",") {
		t.Errorf("spoken on = %v, want %v", spoken, want)
	}
	if again := e.tick(); len(again) != 0 {
		t.Errorf("urgent message sent twice: %v", again)
	}
}

func TestUrgentConfidentialMessageIsNotSpoken(t *testing.T) {
	e := newEnv(t, nil)
	e.message("secret-leak", 3, func(n *store.Message) { n.Confidentiality = store.ConfidentialityConfidential })
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("sent = %v", sent)
	}
	if len(e.ha.calls) != 0 {
		t.Errorf("confidential urgent message was spoken: %+v", e.ha.calls)
	}
}

func TestUrgentRuleCanBeTurnedOff(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Notifier.Urgent.MinImportance = 0 })
	e.message("leak", 3, nil)
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("sent during quiet hours without the urgent rule: %v", sent)
	}
}

// The reply to an announcement made with start_conversation comes while the
// call still runs (it returns once the satellite stopped listening): it
// finds what was announced, and the notifier does not keep it waiting.
func TestTheReplyFindsTheAnnouncementAndDoesNotWait(t *testing.T) {
	e := newEnv(t, nil)
	e.message("inverter", 2, func(n *store.Message) { n.Spoken = "Der Wechselrichter meldet einen Fehler" })
	e.now = at("06:45")
	e.ha.listening = make(chan struct{})
	done := make(chan Outcome, 1)
	go func() { done <- e.presence("bad") }()
	<-e.ha.listening // speaking and listening now
	if a, err := e.st.RecentAnnouncement(context.Background(), "dev-bad", 2*time.Minute); err != nil || len(a.MessageIDs) != 1 {
		t.Fatalf("no announcement to follow up on while it listens: %+v %v", a, err)
	}
	afterword := make(chan string, 1)
	go func() { afterword <- e.n.Afterword(context.Background(), "bad") }()
	select {
	case <-afterword:
	case <-time.After(time.Second):
		t.Fatal("the reply waits for the notifier")
	}
	e.ha.listening <- struct{}{}
	if out := <-done; len(out.Announced) != 1 || e.state("inverter") != store.MessageDelivered {
		t.Errorf("outcome %+v, state %s", out, e.state("inverter"))
	}
}
