package threads

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// fakeFinisher asks once, then proposes; Commit says done.
type fakeFinisher struct {
	mu     sync.Mutex
	works  []store.Thread
	asked  bool
	async  bool
	delay  time.Duration
	commit string
}

func (f *fakeFinisher) Async() bool { return f.async }
func (f *fakeFinisher) Work(_ context.Context, th store.Thread) (Result, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.works = append(f.works, th)
	if !f.asked {
		f.asked = true
		return Result{Question: "Ab wann ist es ungewöhnlich?"}, nil
	}
	return Result{Proposal: "Vorschlag: nachts melden. Soll ich?", Content: json.RawMessage(`{"draft":1}`)}, nil
}
func (f *fakeFinisher) Commit(_ context.Context, th store.Thread) (string, error) {
	f.commit = string(th.Content)
	return "Angelegt.", nil
}

type env struct {
	m    *Manager
	st   *store.Store
	fin  *fakeFinisher
	sent chan store.Message
	now  time.Time
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{fin: &fakeFinisher{async: true}, sent: make(chan store.Message, 8), now: time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)}
	var err error
	e.st, err = store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return e.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	aufgabe := skills.Skill{Name: "aufgabe", Description: "Aufgabe einrichten", Body: "Sammle die Felder.",
		Target: &skills.Target{Confirm: true, Finish: skills.Finish{Name: "fake"}, Fields: []skills.TargetField{
			{Name: "watch", Description: "was", Check: "known"},
			{Name: "measure", Description: "ab wann"},
		}}}
	merken := skills.Skill{Name: "merken", Description: "merken", Body: "Frag nach.",
		Target: &skills.Target{Finish: skills.Finish{Name: "memory.save"}, Fields: []skills.TargetField{{Name: "fact", Description: "was"}}}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{aufgabe, merken},
		Checks: map[string]Check{"known": func(_ context.Context, v string) (string, error) {
			if !strings.Contains(v, "licht") {
				return "", errors.New("kenne ich nicht")
			}
			return v + " (light.schlafzimmer)", nil
		}},
		Finishers: map[string]Finisher{"fake": e.fin, "memory.save": MemorySave(nil, i18n.DE)},
		Deliver: func(_ context.Context, n store.Message, area string) error {
			if area != "wohnzimmer" {
				t.Errorf("delivered to area %q", area)
			}
			if _, _, err := e.st.PutMessage(context.Background(), n); err != nil {
				t.Error(err)
			}
			e.sent <- n
			return nil
		},
		InlineWait: 30 * time.Millisecond,
		Clock:      func() time.Time { return e.now }})
	return e
}

// turn opens a voice turn in the living room, as the engine does.
func (e *env) turn(t *testing.T, key string) (tools.Invocation, store.Session) {
	t.Helper()
	sess, err := e.st.UpsertSession(context.Background(), store.Session{Key: key, Channel: string(config.ChannelVoice),
		ConversationID: key, AreaID: "wohnzimmer"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tools.Invocation{SessionKey: key, Channel: config.ChannelVoice}, sess
}

func (e *env) next(t *testing.T) store.Message {
	t.Helper()
	select {
	case n := <-e.sent:
		return n
	case <-time.After(3 * time.Second):
		t.Fatal("nothing delivered")
	}
	return store.Message{}
}

// Slow work: the turn says Mira comes back, and the result is announced as
// a message of the thread; replies to it continue the thread.
func TestThreadOverSeveralTurns(t *testing.T) {
	e := setup(t)
	e.fin.delay = 200 * time.Millisecond
	ctx := context.Background()

	// Turn 1: the wish; one field is clear, the check resolves it.
	inv, _ := e.turn(t, "voice:a")
	out, err := e.m.Start(ctx, inv, "aufgabe", "Sag Bescheid, wenn das Licht zu ungewöhnlicher Zeit angeht")
	if err != nil || !strings.Contains(out, "Sammle die Felder.") || !strings.Contains(out, "measure – ab wann: (fehlt)") {
		t.Fatalf("start: %q %v", out, err)
	}
	if out, _ := e.m.Fill(ctx, inv, map[string]string{"watch": "Rasenmäher"}); !strings.Contains(out, "kenne ich nicht") {
		t.Errorf("check not applied: %s", out)
	}
	e.m.Fill(ctx, inv, map[string]string{"watch": "licht im schlafzimmer"})
	out, _ = e.m.Fill(ctx, inv, map[string]string{"measure": "- Mira soll es selbst beurteilen"})
	if !strings.Contains(out, "im Hintergrund") {
		t.Fatalf("work not started: %s", out)
	}
	// The work asks back, as a message of the thread.
	q := e.next(t)
	if q.ThreadID == "" || q.Spoken != "Ab wann ist es ungewöhnlich?" {
		t.Fatalf("question: %+v", q)
	}
	if e.fin.works[0].Fields["watch"] != "licht im schlafzimmer (light.schlafzimmer)" {
		t.Errorf("checked value not kept: %+v", e.fin.works[0].Fields)
	}

	// Turn 2, a new HA conversation answering the announcement: it joins
	// the thread by the code, and the model sees it.
	inv2, sess2 := e.turn(t, "voice:b")
	ctxText := e.m.Context(ctx, sess2, []string{q.ID})
	if !strings.Contains(ctxText, "Laufender Vorgang") || !strings.Contains(ctxText, "Ab wann ist es ungewöhnlich?") ||
		!strings.Contains(ctxText, "zu ungewöhnlicher Zeit") {
		t.Fatalf("context:\n%s", ctxText)
	}
	e.m.Fill(ctx, inv2, map[string]string{"measure": "zwischen 0 und 6 Uhr"})
	p := e.next(t)
	if !strings.Contains(p.Spoken, "Soll ich?") || p.ThreadID != q.ThreadID {
		t.Fatalf("proposal: %+v", p)
	}
	if old, _ := e.st.GetMessage(ctx, q.ID); old.State != store.MessageExpired {
		t.Errorf("the answered question is still open: %s", old.State)
	}

	// Turn 3 answers the proposal: yes.
	inv3, sess3 := e.turn(t, "voice:c")
	if text := e.m.Context(ctx, sess3, []string{p.ID}); !strings.Contains(text, "thread_confirm") {
		t.Errorf("no confirm hint:\n%s", text)
	}
	out, err = e.m.Confirm(ctx, inv3)
	if err != nil || out != "Angelegt." || e.fin.commit != `{"draft":1}` {
		t.Fatalf("confirm: %q %v %q", out, err, e.fin.commit)
	}
	th, _ := e.st.GetThread(ctx, q.ThreadID)
	if th.State != store.ThreadDone {
		t.Errorf("state %s", th.State)
	}
	if n, _ := e.st.GetMessage(ctx, p.ID); n.State != store.MessageExpired {
		t.Errorf("the proposal stays open after the end: %s", n.State)
	}
}

func TestWaitingThreadShownAtItsPlaceAndExpires(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	e.m.Start(ctx, inv, "aufgabe", "Achte auf das Licht")

	// A later turn in the same room, not answering an announcement: the
	// model sees the waiting thread and may continue or leave it.
	_, sess := e.turn(t, "voice:b")
	if text := e.m.Context(ctx, sess, nil); !strings.Contains(text, "Hier wartet ein Vorgang") {
		t.Errorf("waiting thread not shown:\n%s", text)
	}
	// Elsewhere it is not shown.
	other, _ := e.st.UpsertSession(ctx, store.Session{Key: "voice:k", Channel: "voice", ConversationID: "k", AreaID: "kueche"}, time.Hour)
	if text := e.m.Context(ctx, other, nil); text != "" {
		t.Errorf("shown in another room:\n%s", text)
	}

	e.now = e.now.Add(16 * time.Minute)
	e.m.expire(ctx)
	if text := e.m.Context(ctx, sess, nil); text != "" {
		t.Errorf("expired thread still shown:\n%s", text)
	}
}

func TestCancelAndRemember(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	e.m.Start(ctx, inv, "aufgabe", "Achte auf das Licht")
	if out, err := e.m.Cancel(ctx, inv); err != nil || !strings.Contains(out, "abgebrochen") {
		t.Fatalf("cancel: %q %v", out, err)
	}
	if _, err := e.m.Confirm(ctx, inv); err == nil {
		t.Error("confirmed a cancelled thread")
	}

	e.m.opts.Finishers["memory.save"] = MemorySave(e.st, i18n.DE)
	e.m.Start(ctx, inv, "merken", "Merk dir mal was")
	out, err := e.m.Fill(ctx, inv, map[string]string{"fact": "Ungewöhnlich ist Licht zwischen 0 und 6 Uhr."})
	if err != nil || !strings.Contains(out, "Gemerkt") {
		t.Fatalf("remember: %q %v", out, err)
	}
	facts, _ := e.st.AllFacts(ctx)
	if len(facts) != 1 || facts[0].Text != "Ungewöhnlich ist Licht zwischen 0 und 6 Uhr." {
		t.Errorf("facts: %+v", facts)
	}
}

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

// Fast work: the result is said in the same turn, nothing is announced.
func TestFastWorkAnswersInTheTurn(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	e.m.Start(ctx, inv, "aufgabe", "Achte auf das Licht")
	out, _ := e.m.Fill(ctx, inv, map[string]string{"watch": "licht", "measure": "- nichts Relatives"})
	if out != "Frag den Nutzer: Ab wann ist es ungewöhnlich?" {
		t.Fatalf("not in the turn: %q", out)
	}
	out, _ = e.m.Fill(ctx, inv, map[string]string{"measure": "0 bis 6 Uhr"})
	if !strings.Contains(out, "Soll ich?") {
		t.Fatalf("proposal not in the turn: %q", out)
	}
	select {
	case n := <-e.sent:
		t.Errorf("announced although answered in the turn: %+v", n)
	case <-time.After(100 * time.Millisecond):
	}
	if out, err := e.m.Confirm(ctx, inv); err != nil || out != "Angelegt." {
		t.Errorf("confirm: %q %v", out, err)
	}
}
