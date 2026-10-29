package threads

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
)

const recipe = `# Pfannkuchen

Zutaten: 400 g Mehl, 3 Eier, 750 ml Milch

- [ ] Teig rühren, 30 Minuten ruhen lassen
- [ ] Mineralwasser unterrühren
- [ ] Pfanne mit Butter heiß werden lassen`

// planEnv is the thread test setup with a plan target "kochen".
func planEnv(t *testing.T) *env {
	t.Helper()
	e := setup(t)
	kochen := skills.Skill{Name: "kochen", Description: "Kochen nach Rezept", Body: "Mach aus dem Rezept einen Plan.",
		Target: &skills.Target{Plan: true}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{kochen}, Clock: func() time.Time { return e.now }})
	return e
}

func (e *env) state(t *testing.T, id string) string {
	t.Helper()
	th, err := e.st.GetThread(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return th.State
}

// A plan outlives the conversation: a later one at the same place sees the
// step before, the current and the next one; another room sees nothing.
func TestPlanAcrossConversations(t *testing.T) {
	e := planEnv(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	out, err := e.m.Start(ctx, inv, "kochen", "Pfannkuchen backen")
	if err != nil || !strings.Contains(out, "Mach aus dem Rezept einen Plan.") || !strings.Contains(out, "Noch kein Plan") {
		t.Fatalf("start: %q %v", out, err)
	}
	inv.Foreign = true // the recipe came from a web page
	out, err = e.m.Plan(ctx, inv, "write", recipe, 0)
	if err != nil || !strings.Contains(out, "0 von 3") || !strings.Contains(out, "Jetzt (Schritt 1): Teig rühren") ||
		!strings.Contains(out, "Danach (Schritt 2): Mineralwasser") || !strings.Contains(out, "fremdem Text") {
		t.Fatalf("write: %q %v", out, err)
	}
	out, _ = e.m.Plan(ctx, inv, "check", "", 1)
	if !strings.Contains(out, "Zuletzt erledigt (Schritt 1): Teig") || !strings.Contains(out, "Jetzt (Schritt 2)") {
		t.Errorf("check: %q", out)
	}

	// 40 minutes later, a new conversation in the same room.
	e.now = e.now.Add(40 * time.Minute)
	_, sess := e.turn(t, "voice:b")
	text, foreign := e.m.Context(ctx, sess, placeOf(sess), nil, "")
	if !foreign || !strings.Contains(text, "1 von 3") || !strings.Contains(text, "Jetzt (Schritt 2): Mineralwasser") ||
		!strings.Contains(text, "Ablauf mit Plan") || !strings.Contains(text, "Mach aus dem Rezept einen Plan.") {
		t.Errorf("context: %v %q", foreign, text)
	}
	other, err := e.st.UpsertSession(ctx, store.Session{Key: "voice:c", Channel: string(config.ChannelVoice), AreaID: "kueche"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := e.m.Context(ctx, other, placeOf(other), nil, ""); text != "" {
		t.Errorf("another room sees the plan: %q", text)
	}

	// Ticking off kept the text as it was.
	out, _ = e.m.Plan(ctx, inv, "read", "", 0)
	if !strings.Contains(out, "- [x] Teig rühren, 30 Minuten ruhen lassen\n- [ ] Mineralwasser") || !strings.Contains(out, "Zutaten: 400 g Mehl") {
		t.Errorf("read: %q", out)
	}
	if _, err := e.m.Fill(ctx, inv, map[string]string{"x": "y"}); err == nil {
		t.Error("fields on a plan")
	}
	if _, err := e.m.Plan(ctx, inv, "check", "", 4); err == nil {
		t.Error("step 4 of 3")
	}
	if _, err := e.m.Plan(ctx, inv, "write", strings.Repeat("x", maxPlan+1), 0); err == nil {
		t.Error("plan too long")
	}
}

// Every step starts the time anew; without one the plan expires. Once all
// steps are done it stays open a while, then it is done.
func TestPlanLifecycle(t *testing.T) {
	e := planEnv(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	e.m.Start(ctx, inv, "kochen", "Pfannkuchen")
	e.m.Plan(ctx, inv, "write", recipe, 0)
	sess, _ := e.st.GetSession(ctx, "voice:a")
	id := sess.ThreadID

	e.now = e.now.Add(50 * time.Minute)
	e.m.Plan(ctx, inv, "check", "", 1)
	e.now = e.now.Add(50 * time.Minute)
	e.m.expire(ctx)
	if s := e.state(t, id); s != store.ThreadCollecting {
		t.Fatalf("expired although a step was done 50 minutes ago: %s", s)
	}

	// Starting again offers to go on rather than throwing it away.
	if out, _ := e.m.Start(ctx, inv, "kochen", "Pfannkuchen"); !strings.Contains(out, "weitermachen oder neu") {
		t.Errorf("second start: %q", out)
	}
	if s := e.state(t, id); s != store.ThreadCollecting {
		t.Errorf("second start ended the plan: %s", s)
	}

	e.m.Plan(ctx, inv, "check", "", 2)
	out, _ := e.m.Plan(ctx, inv, "check", "", 3)
	if !strings.Contains(out, "3 von 3") || !strings.Contains(out, "bleibt bis") {
		t.Errorf("all done: %q", out)
	}
	// "Wait, not done yet" within the grace.
	e.now = e.now.Add(20 * time.Minute)
	e.m.Plan(ctx, inv, "uncheck", "", 3)
	e.now = e.now.Add(40 * time.Minute)
	e.m.expire(ctx)
	if s := e.state(t, id); s != store.ThreadCollecting {
		t.Fatalf("reopened plan ended: %s", s)
	}
	e.m.Plan(ctx, inv, "check", "", 3)
	e.now = e.now.Add(29 * time.Minute)
	e.m.expire(ctx)
	if s := e.state(t, id); s != store.ThreadCollecting {
		t.Fatalf("ended within the grace: %s", s)
	}
	e.now = e.now.Add(2 * time.Minute)
	e.m.expire(ctx)
	if s := e.state(t, id); s != store.ThreadDone {
		t.Errorf("after the grace: %s", s)
	}

	// A plan left alone expires after an hour.
	inv2, _ := e.turn(t, "voice:z")
	e.m.Start(ctx, inv2, "kochen", "Suppe")
	e.m.Plan(ctx, inv2, "write", recipe, 0)
	sess2, _ := e.st.GetSession(ctx, "voice:z")
	e.now = e.now.Add(61 * time.Minute)
	e.m.expire(ctx)
	if s := e.state(t, sess2.ThreadID); s != store.ThreadExpired {
		t.Errorf("left alone: %s", s)
	}
}

// Without checkboxes the context holds only a reference; the model reads
// the plan when it needs it. A plan of Mira's own is not foreign.
func TestPlanWithoutSteps(t *testing.T) {
	e := planEnv(t)
	ctx := context.Background()
	inv, sess := e.turn(t, "voice:a")
	e.m.Start(ctx, inv, "kochen", "Menü planen")
	out, _ := e.m.Plan(ctx, inv, "write", "Vorspeise: Suppe\nHauptgang: Pfannkuchen", 0)
	if !strings.Contains(out, "ohne Schritte") || strings.Contains(out, "Hauptgang") {
		t.Errorf("reference: %q", out)
	}
	sess, _ = e.st.GetSession(ctx, sess.Key)
	if text, foreign := e.m.Context(ctx, sess, placeOf(sess), nil, ""); foreign || !strings.Contains(text, "ohne Schritte") {
		t.Errorf("context: %v %q", foreign, text)
	}
	var names []string
	for _, tool := range e.m.Tools() {
		names = append(names, tool.Name)
	}
	if !strings.Contains(strings.Join(names, " "), ToolPlan) {
		t.Errorf("tools: %v", names)
	}
}

// AG-83 Two runs change the same plan at the same time
func TestAG83_TwoRunsChangeTheSamePlanAtTheSameTime(t *testing.T) {
	e := planEnv(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	if _, err := e.m.Start(ctx, inv, "kochen", "Pfannkuchen backen"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		// Given a plan with the steps 1 and 2 open
		if _, err := e.m.Plan(ctx, inv, "write", recipe, 0); err != nil {
			t.Fatal(err)
		}
		// When one run ticks off step 1 and another step 2 at the same time
		var wg sync.WaitGroup
		for _, step := range []int{1, 2} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := e.m.Plan(ctx, inv, "check", "", step); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		// Then both steps are ticked off
		if out, _ := e.m.Plan(ctx, inv, "read", "", 0); strings.Count(out, "[x]") != 2 {
			t.Fatalf("round %d: %q", i, out)
		}
	}
}

// abEnv has the answering machine's plan target.
func abEnv(t *testing.T) *env {
	t.Helper()
	e := setup(t)
	ab := skills.Skill{Name: "anrufbeantworter", Description: "Anrufbeantworter", Body: "Abhören.",
		Target: &skills.Target{Plan: true}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: []skills.Skill{ab}, Clock: func() time.Time { return e.now }})
	return e
}

// Asking again rewrites the plan of the thread that runs; there is never a
// second one.
func TestAB31_AskingAgainKeepsTheOneThread(t *testing.T) {
	e := abEnv(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	id, err := e.m.SetPlan(ctx, inv, "anrufbeantworter", "Habe ich Nachrichten?", "# AB\n- [ ] Oma (id 0:13)")
	if err != nil {
		t.Fatal(err)
	}
	inv2, _ := e.turn(t, "voice:b")
	id2, err := e.m.SetPlan(ctx, inv2, "anrufbeantworter", "Habe ich Nachrichten?", "# AB\n- [ ] Ben (id 0:14)\n- [ ] Oma (id 0:13)")
	if err != nil || id2 != id {
		t.Fatalf("second thread %q (first %q): %v", id2, id, err)
	}
	open, _ := e.st.OpenThreads(ctx, "area:wohnzimmer")
	if len(open) != 1 || !strings.Contains(planOf(open[0]).Plan, "Ben (id 0:14)") {
		t.Errorf("open %+v", open)
	}
	if err := e.m.TickPlan(ctx, id, "(id 0:13)"); err != nil {
		t.Fatal(err)
	}
	th, _ := e.st.GetThread(ctx, id)
	if !strings.Contains(planOf(th).Plan, "- [x] Oma (id 0:13)") {
		t.Errorf("not ticked: %q", planOf(th).Plan)
	}
}

// The answer to an announcement that named its thread joins that thread:
// it has the plan in its context and the skill open. Who speaks does not
// change: the turn stays the turn it is.
func TestAB33_TheAnswerJoinsTheThreadWithoutBecomingThePerson(t *testing.T) {
	e := abEnv(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:anna")
	inv.Place = "person:anna"
	id, err := e.m.SetPlan(ctx, inv, "anrufbeantworter", "Habe ich Nachrichten?", "# AB\n- [x] Oma (id 0:13)\n- [ ] Ben (id 0:12)")
	if err != nil {
		t.Fatal(err)
	}
	// The answer: a new turn in the room, nobody recognised.
	_, answer := e.turn(t, "voice:new")
	text, _ := e.m.Context(ctx, answer, placeOf(answer), nil, id)
	if !strings.Contains(text, "Zuletzt erledigt (Schritt 1): Oma (id 0:13)") || !strings.Contains(text, "Jetzt (Schritt 2): Ben (id 0:12)") {
		t.Errorf("context %q", text)
	}
	sess, _ := e.st.GetSession(ctx, "voice:new")
	if sess.ThreadID != id || sess.Key != "voice:new" {
		t.Errorf("session %+v", sess)
	}
	if open, _ := e.st.SessionSkills(ctx, "voice:new"); len(open) != 1 || open[0] != "anrufbeantworter" {
		t.Errorf("skills %v", open)
	}
	// Without the announcement the room does not see Anna's thread.
	_, other := e.turn(t, "voice:other")
	if text, _ := e.m.Context(ctx, other, placeOf(other), nil, ""); text != "" {
		t.Errorf("another turn sees it: %q", text)
	}
}

// A short plan shows every open step in the context, so "weiter" can take
// all the rest; a long one keeps the window (AB-38).
func TestAB38_AShortPlanShowsEveryOpenStep(t *testing.T) {
	e := abEnv(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	plan := "# AB\n- [x] Oma (id 0:13)\n- [ ] Ben (id 0:12)\n- [ ] Papa (id 0:11)\n- [ ] Mama (id 0:10)"
	if _, err := e.m.SetPlan(ctx, inv, "anrufbeantworter", "x", plan); err != nil {
		t.Fatal(err)
	}
	_, sess := e.turn(t, "voice:b")
	text, _ := e.m.Context(ctx, sess, placeOf(sess), nil, "")
	for _, want := range []string{"Jetzt (Schritt 2): Ben", "Danach (Schritt 3): Papa", "Danach (Schritt 4): Mama"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
}

// Ending a thread tells whoever acts on its behalf (AB-39).
func TestAB39_EndingAThreadIsTold(t *testing.T) {
	e := abEnv(t)
	var ended []string
	e.m.opts.Ended = func(_ context.Context, th store.Thread) { ended = append(ended, th.ID) }
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	id, err := e.m.SetPlan(ctx, inv, "anrufbeantworter", "x", "# AB\n- [ ] Oma (id 0:13)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Cancel(ctx, inv); err != nil {
		t.Fatal(err)
	}
	if len(ended) != 1 || ended[0] != id {
		t.Errorf("ended %v", ended)
	}
}
