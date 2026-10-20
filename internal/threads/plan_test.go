package threads

import (
	"context"
	"strings"
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
	text, foreign := e.m.Context(ctx, sess, nil)
	if !foreign || !strings.Contains(text, "1 von 3") || !strings.Contains(text, "Jetzt (Schritt 2): Mineralwasser") ||
		!strings.Contains(text, "Ablauf mit Plan") || !strings.Contains(text, "Mach aus dem Rezept einen Plan.") {
		t.Errorf("context: %v %q", foreign, text)
	}
	other, err := e.st.UpsertSession(ctx, store.Session{Key: "voice:c", Channel: string(config.ChannelVoice), AreaID: "kueche"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := e.m.Context(ctx, other, nil); text != "" {
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
	if text, foreign := e.m.Context(ctx, sess, nil); foreign || !strings.Contains(text, "ohne Schritte") {
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
