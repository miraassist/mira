package notifier

import (
	"context"
	"github.com/sker65/mira/internal/i18n"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/store"
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
}

func (f *fakeHA) CallService(_ context.Context, domain, service string, data map[string]any, target *haws.Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{domain, service, data, target})
	return f.callErr
}

func (f *fakeHA) States(context.Context) ([]haws.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []haws.State
	for _, s := range f.states {
		out = append(out, s)
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
	f.mu.Unlock()
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

type fakeTelegram struct {
	mu   sync.Mutex
	sent map[int64][]string
	all  []string
}

func (f *fakeTelegram) Send(_ context.Context, chatID int64, text string) error {
	f.mu.Lock()
	f.sent[chatID] = append(f.sent[chatID], text)
	f.mu.Unlock()
	return nil
}

func (f *fakeTelegram) SendToAll(_ context.Context, text string) error {
	f.mu.Lock()
	f.all = append(f.all, text)
	f.mu.Unlock()
	return nil
}

type env struct {
	n   *Notifier
	st  *store.Store
	ha  *fakeHA
	tg  *fakeTelegram
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
		Presence: map[string]string{"bad": "binary_sensor.bad_motion", "kueche": "binary_sensor.kueche_motion"},
	}
	cfg.Persons = map[string]config.Person{
		"stefan": {Entity: "person.stefan", TelegramUserID: 111},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	e := &env{t: t, now: at("03:00"), ha: &fakeHA{states: map[string]haws.State{}}, tg: &fakeTelegram{sent: map[int64][]string{}}}
	e.ha.set("assist_satellite.bad", "idle", map[string]any{"supported_features": float64(3)})
	e.ha.set("assist_satellite.kueche", "idle", map[string]any{"supported_features": float64(1)})
	e.ha.set("person.stefan", "home", nil)

	var err error
	e.st, err = store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return e.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	e.n = New(Options{Config: cfg, Store: e.st, HA: e.ha, Telegram: e.tg, Clock: func() time.Time { return e.now },
		Location: time.UTC, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	e.n.speakPause = 0
	return e
}

func (e *env) notice(id string, importance int, mutate func(*store.Notice)) {
	e.t.Helper()
	n := store.Notice{ID: id, Source: store.SourceHATrigger, Importance: importance,
		Spoken: "Hinweis " + id, Details: "Details " + id, ExpiresAt: e.now.Add(48 * time.Hour)}
	if mutate != nil {
		mutate(&n)
	}
	if _, _, err := e.st.PutNotice(context.Background(), n); err != nil {
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
	n, err := e.st.GetNotice(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return n.State
}

// The plan's acceptance case: a fault at night, the first motion in the
// bathroom after quiet hours leads to exactly one announcement.
func TestNightFaultIsAnnouncedOnceOnFirstMorningMotion(t *testing.T) {
	e := newEnv(t, nil)
	e.notice("inverter", 2, func(n *store.Notice) { n.Spoken = "Der Wechselrichter meldet seit drei Uhr einen Fehler" })

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
	if e.state("inverter") != store.NoticeDelivered {
		t.Error("notice not marked delivered")
	}
	a, err := e.st.RecentAnnouncement(context.Background(), "dev-bad", 2*time.Minute)
	if err != nil || len(a.NoticeIDs) != 1 || a.NoticeIDs[0] != "inverter" {
		t.Fatalf("announcement for follow-up = %+v, %v", a, err)
	}

	// More motion right after, here and next door: nothing more to say.
	e.now = at("06:46")
	if out := e.presence("bad"); len(out.Announced) != 0 {
		t.Fatalf("second motion announced again: %+v", out)
	}
	if out := e.presence("kueche"); len(out.Announced) != 0 {
		t.Fatalf("kitchen repeated the notice: %+v", out)
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
		{"unknown area", nil, "keller", "area not configured", ""},
		{"nothing pending", func(e *env) { e.st.SetNoticeState(context.Background(), "n1", store.NoticeAcked) }, "bad", "nothing pending", ""},
		{"below min importance", func(e *env) {
			e.st.SetNoticeState(context.Background(), "n1", store.NoticeAcked)
			e.notice("low", 0, nil)
		}, "bad", "nothing pending", ""},
		{"start_conversation when supported", nil, "bad", "announce", "assist_satellite.start_conversation"},
		{"announce when not", nil, "kueche", "announce", "assist_satellite.announce"},
		{"expired notice", func(e *env) {
			e.now = at("12:00")
			e.st.ExpireNotices(context.Background())
			e.now = at("07:00")
			e.st.SetNoticeState(context.Background(), "n1", store.NoticeExpired)
		}, "bad", "nothing pending", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, nil)
			e.notice("n1", 2, nil)
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
	e.notice("a", 2, nil)
	e.presence("bad")
	e.now = at("07:10")
	e.notice("b", 2, nil)
	if out := e.presence("bad"); out.Skipped != "cooldown" {
		t.Fatalf("within cooldown: %+v", out)
	}
	if out := e.presence("kueche"); len(out.Announced) != 1 {
		t.Fatalf("other area has no cooldown: %+v", out)
	}
	e.now = at("07:31")
	e.notice("c", 2, nil)
	if out := e.presence("bad"); len(out.Announced) != 1 || out.Announced[0] != "c" {
		t.Fatalf("after cooldown: %+v", out)
	}
}

func TestBundlingTakesTheMostImportantThree(t *testing.T) {
	e := newEnv(t, nil)
	e.notice("n1", 1, nil)
	e.notice("n2", 3, nil)
	e.notice("n3", 2, nil)
	e.notice("n4", 1, nil)
	e.notice("n5", 2, nil)
	e.now = at("12:00")
	out := e.presence("bad")
	if got := strings.Join(out.Announced, ","); got != "n2,n3,n5" {
		t.Fatalf("announced %s", got)
	}
	text := e.ha.calls[0].Data["start_message"].(string)
	if text != "Ich habe drei Hinweise. Hinweis n2. Außerdem: Hinweis n3. Und: Hinweis n5." {
		t.Errorf("text = %q", text)
	}
	if e.state("n1") != store.NoticePending || e.state("n4") != store.NoticePending {
		t.Error("the rest must stay pending for later")
	}
}

func TestMediaPlayerSatelliteUsesTTS(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Satellites["bad"] = "media_player.echo_bad"
		c.Notifier.TTSEntity = "tts.piper"
	})
	e.notice("n1", 2, nil)
	e.now = at("08:00")
	e.presence("bad")
	c := e.ha.calls[0]
	if c.Domain+"."+c.Service != "tts.speak" || c.Target.EntityID[0] != "tts.piper" || c.Data["media_player_entity_id"] != "media_player.echo_bad" {
		t.Fatalf("call = %+v", c)
	}
}

func TestConfidentialNotices(t *testing.T) {
	confidential := func(n *store.Notice) { n.Confidentiality = store.ConfidentialityConfidential }
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
			e.notice("secret", 2, func(n *store.Notice) { confidential(n); n.Audience = "stefan" })
			e.now = at("08:00")
			out := e.presence("bad")
			spoken := len(out.Announced) == 1
			if spoken != c.wantVoice {
				t.Fatalf("spoken = %v, want %v (%+v)", spoken, c.wantVoice, out)
			}
			if !c.wantVoice && len(e.tg.sent[111]) != 1 {
				t.Fatalf("not sent privately: %+v", e.tg.sent)
			}
		})
	}
}

func TestConfidentialNeverGoesOutOnTickWithoutWaitingForMotion(t *testing.T) {
	e := newEnv(t, nil) // default policy: never
	e.notice("secret", 1, func(n *store.Notice) { n.Confidentiality = store.ConfidentialityConfidential })
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("sent during quiet hours: %v", sent)
	}
	e.now = at("06:31")
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent[111]) != 1 {
		t.Fatalf("sent = %v, telegram = %v", sent, e.tg.sent)
	}
	if !strings.Contains(e.tg.sent[111][0], "Details secret") {
		t.Error("private message lacks the details")
	}
}

func TestPersonalNoticeForUntrackedRecipientIsSpoken(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Persons["anna"] = config.Person{Entity: "person.anna", TelegramUserID: 222} })
	e.ha.set("person.anna", "unknown", nil)
	e.notice("for-anna", 1, func(n *store.Notice) { n.Audience = "anna" })
	e.now = at("08:00")
	if out := e.presence("bad"); len(out.Announced) != 1 {
		t.Fatalf("a person without tracker is not known to be away: %+v", out)
	}
}

func TestPersonalNoticeWaitsWhileRecipientIsAway(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Persons["anna"] = config.Person{Entity: "person.anna", TelegramUserID: 222} })
	e.ha.set("person.anna", "not_home", nil)
	e.notice("for-anna", 1, func(n *store.Notice) { n.Audience = "anna" })
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
	e.notice("important", 2, nil)
	e.notice("minor", 1, nil)
	e.now = at("08:59")
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("escalated before 09:00: %v", sent)
	}
	e.now = at("09:00")
	sent := e.tick()
	if len(sent) != 1 || sent[0] != "important" || len(e.tg.sent[111]) != 1 {
		t.Fatalf("escalation = %v, telegram %v", sent, e.tg.sent)
	}
	n, _ := e.st.GetNotice(context.Background(), "important")
	if n.State != store.NoticeDelivered || n.DeliveredVia != "telegram" {
		t.Errorf("notice after escalation = %+v", n)
	}
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("escalated twice: %v", sent)
	}
}

func TestEscalationGivesDaytimeNoticesAGracePeriod(t *testing.T) {
	e := newEnv(t, nil)
	e.now = at("14:00")
	e.notice("afternoon", 2, nil)
	e.now = at("14:10")
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("escalated within grace: %v", sent)
	}
	e.now = at("14:31")
	if sent := e.tick(); len(sent) != 1 {
		t.Fatalf("not escalated after grace: %v", sent)
	}
}

func TestPushFallbackAndHouseholdWithoutPersons(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Persons = map[string]config.Person{"stefan": {Entity: "person.stefan", Notify: "notify.mobile_app_pixel"}}
	})
	e.notice("n", 2, nil)
	e.now = at("09:30")
	e.tick()
	if got := e.ha.servicesCalled(); len(got) != 1 || got[0] != "notify.mobile_app_pixel" {
		t.Fatalf("services = %v", got)
	}

	e2 := newEnv(t, func(c *config.Config) { c.Persons = nil })
	e2.notice("n", 2, nil)
	e2.now = at("09:30")
	e2.tick()
	if len(e2.tg.all) != 1 {
		t.Fatalf("without persons the household gets it on Telegram: %v", e2.tg.all)
	}
}

func TestCompose(t *testing.T) {
	one := []store.Notice{{Spoken: "Die Waschmaschine ist fertig"}}
	if got := Compose(i18n.DE, one, at("15:00"), config.ClockTime{Hour: 6, Minute: 30}); got != "Die Waschmaschine ist fertig." {
		t.Errorf("got %q", got)
	}
	two := []store.Notice{{Spoken: "A."}, {Spoken: "B!"}}
	if got := Compose(i18n.DE, two, at("07:00"), config.ClockTime{Hour: 6, Minute: 30}); got != "Guten Morgen. Ich habe zwei Hinweise. A. Und: B!" {
		t.Errorf("got %q", got)
	}
}

func TestUrgentNoticeGoesOutAtOnceEvenAtNight(t *testing.T) {
	e := newEnv(t, nil) // 03:00, inside quiet hours
	e.notice("leak", 3, nil)
	sent := e.tick()
	if len(sent) != 1 || len(e.tg.sent[111]) != 1 {
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
		t.Errorf("urgent notice sent twice: %v", again)
	}
}

func TestUrgentConfidentialNoticeIsNotSpoken(t *testing.T) {
	e := newEnv(t, nil)
	e.notice("secret-leak", 3, func(n *store.Notice) { n.Confidentiality = store.ConfidentialityConfidential })
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent[111]) != 1 {
		t.Fatalf("sent = %v", sent)
	}
	if len(e.ha.calls) != 0 {
		t.Errorf("confidential urgent notice was spoken: %+v", e.ha.calls)
	}
}

func TestUrgentRuleCanBeTurnedOff(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Notifier.Urgent.MinImportance = 0 })
	e.notice("leak", 3, nil)
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("sent during quiet hours without the urgent rule: %v", sent)
	}
}
