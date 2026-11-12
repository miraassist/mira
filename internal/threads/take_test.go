package threads

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// docs/spec/messages.md, Kinds of messages: taking up a thread, what still
// waits, threads of the house.

func postfachEnv(t *testing.T) *env {
	t.Helper()
	e := setup(t)
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"postfach": nil})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("skill postfach: %+v", problems)
	}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: loaded, Clock: func() time.Time { return e.now }})
	return e
}

// mail starts a person's postfach thread and puts its entry into the
// person's postfach message.
func (e *env) mail(t *testing.T, person, line string) store.Thread {
	t.Helper()
	ctx := context.Background()
	th, err := e.m.StartFor(ctx, "postfach", person, line, line+"\n- [ ] Termin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.st.PutMessage(ctx, store.Message{ID: "m-" + th.ID, Kind: store.KindPostfach, Receiver: person,
		Spoken: line, ThreadID: th.ID}); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Minute)
	return th
}

func (e *env) annasTurn(t *testing.T) tools.Invocation {
	t.Helper()
	inv, _ := e.turn(t, "telegram:anna")
	inv.Person, inv.Place = "anna", "person:anna"
	return inv
}

func TestMS26_ThreadTakeMakesAThreadTheTurnsOwn(t *testing.T) {
	e := postfachEnv(t)
	ctx := context.Background()
	praxis := e.mail(t, "anna", "Terminvorschläge der Praxis")
	e.mail(t, "anna", "Elternbrief")
	inv := e.annasTurn(t)
	out, err := e.m.Take(ctx, inv, praxis.ID)
	if err != nil || !strings.Contains(out, "Terminvorschläge der Praxis") {
		t.Fatalf("take: %q %v", out, err)
	}
	if out, err := e.m.PlanNote(ctx, inv, "check", "", 1, "eingetragen"); err != nil || !strings.Contains(out, "1 von 1") {
		t.Fatalf("check: %q %v", out, err)
	}
	if got, _ := e.st.GetThread(ctx, praxis.ID); planOf(got).DoneAt == nil {
		t.Fatal("the older thread was not the one ticked")
	}
}

func TestMS27_ThreadTakeRefusesAThreadThatIsNotTheAskers(t *testing.T) {
	e := postfachEnv(t)
	bens := e.mail(t, "ben", "Chorprobe")
	e.mail(t, "anna", "Elternbrief")
	_, err := e.m.Take(context.Background(), e.annasTurn(t), bens.ID)
	if err == nil || !strings.Contains(err.Error(), "Offen für dich") || !strings.Contains(err.Error(), "Elternbrief") ||
		strings.Contains(err.Error(), "Chorprobe") {
		t.Fatalf("err = %v", err)
	}
}

func TestMS28_WhenAThreadEndsTheToolNamesWhatIsStillOpen(t *testing.T) {
	e := postfachEnv(t)
	ctx := context.Background()
	brief := e.mail(t, "anna", "Elternbrief")
	e.mail(t, "anna", "Terminvorschläge der Praxis")
	inv := e.annasTurn(t)
	e.m.Take(ctx, inv, brief.ID)
	out, err := e.m.PlanNote(ctx, inv, "check", "", 1, "eingetragen")
	if err != nil || !strings.Contains(out, "Noch offen: Terminvorschläge der Praxis") || strings.Contains(out, "Noch offen: Elternbrief") {
		t.Fatalf("done: %q %v", out, err)
	}
}

func TestMS29_TheLineWartetNochInEveryTurn(t *testing.T) {
	e := postfachEnv(t)
	ctx := context.Background()
	e.mail(t, "anna", "Elternbrief")
	e.mail(t, "anna", "Praxis")
	e.st.PutMessage(ctx, store.Message{ID: "d1", Kind: store.KindDevice, Spoken: "Hue Bridge"})
	e.st.PutMessage(ctx, store.Message{ID: "b1", Kind: store.KindPostfach, Receiver: "ben", Spoken: "Chorprobe"})
	text, _ := e.m.Context(ctx, store.Session{Key: "voice:x"}, "person:anna", nil, "")
	if !strings.Contains(text, "Wartet noch (Einzelheiten mit messages_read): 1 × device, 2 × postfach") {
		t.Fatalf("context:\n%s", text)
	}
}

// A thread Mira's code starts for the house keeps its starter's expiry until
// a turn takes it up; then its type's timeout counts (decisions 11, 13).
func TestAThreadOfTheHouseKeepsItsDeadlineUntilTakenUp(t *testing.T) {
	e := postfachEnv(t)
	ctx := context.Background()
	deadline := e.now.Add(5 * 24 * time.Hour)
	th, err := e.m.StartAt(ctx, Begin{Target: "postfach", Request: "Haus", Plan: "Haus\n- [ ] a", Deadline: deadline})
	if err != nil || th.Place != store.PlaceHouse || !th.ExpiresAt.Equal(deadline) {
		t.Fatalf("thread = %+v %v", th, err)
	}
	if _, err := e.m.Take(ctx, e.annasTurn(t), th.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.st.GetThread(ctx, th.ID)
	// Taking up never shortens (decision 26): its 5 days stay.
	if !got.Deadline.IsZero() || !got.ExpiresAt.Equal(deadline) {
		t.Fatalf("taken up: deadline %v, expires %v", got.Deadline, got.ExpiresAt)
	}
}

func TestMS15_AMailsThreadExpiresWhenInboxReadSays(t *testing.T) {
	e := postfachEnv(t)
	ctx := context.Background()
	deadline := e.now.Add(26 * time.Hour) // tomorrow 12:00, the deadline to answer
	th, err := e.m.StartAt(ctx, Begin{Target: "postfach", Person: "anna", Request: "Rückmeldung zum Ausflug",
		Plan: "Rückmeldung zum Ausflug\n- [ ] Rückmeldung bis morgen 12:00", Deadline: deadline})
	if err != nil {
		t.Fatal(err)
	}
	e.st.PutMessage(ctx, store.Message{ID: "m1", Kind: store.KindPostfach, Receiver: "anna", Spoken: "Ausflug", ThreadID: th.ID})
	e.st.PutMessage(ctx, store.Message{ID: "m2", Kind: store.KindPostfach, Receiver: "anna", Spoken: "Elternbrief", ThreadID: "other"})
	e.now = deadline.Add(-time.Minute)
	e.m.expire(ctx)
	if got, _ := e.st.GetThread(ctx, th.ID); got.State == store.ThreadExpired {
		t.Fatal("expired before its deadline")
	}
	e.now = deadline.Add(time.Minute)
	e.m.expire(ctx)
	if got, _ := e.st.GetThread(ctx, th.ID); got.State != store.ThreadExpired || planOf(got).DoneAt != nil {
		t.Fatalf("thread = %s", got.State)
	}
	m, _ := e.st.GetMessage(ctx, "m1")
	if len(m.Entries) != 1 || m.Entries[0].ThreadID != "other" || m.State != store.MessagePending {
		t.Fatalf("message = %+v", m)
	}
}

func devicesEnv(t *testing.T) *env {
	t.Helper()
	e := setup(t)
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"geraet-einbinden": nil})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("skill geraet-einbinden: %+v", problems)
	}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: loaded, Clock: func() time.Time { return e.now }})
	return e
}

func TestMS33_AfterADayTheFoundDeviceIsGone(t *testing.T) {
	e := devicesEnv(t)
	ctx := context.Background()
	th, err := e.m.StartAt(ctx, Begin{Target: "geraet-einbinden", Request: "Hue Lampe",
		Plan: "# Neues Gerät\n- [ ] Einbinden", Deadline: e.now.Add(24 * time.Hour), Fields: map[string]string{"item": "device:d1"}})
	if err != nil {
		t.Fatal(err)
	}
	e.st.PutMessage(ctx, store.Message{ID: "m1", Kind: store.KindDevice, Receiver: store.ReceiverHouse, Spoken: "Hue Lampe", ThreadID: th.ID})
	e.now = e.now.Add(23 * time.Hour)
	e.m.expire(ctx)
	if got, _ := e.st.GetThread(ctx, th.ID); !store.ThreadOpen(got.State) {
		t.Fatal("gone before its day")
	}
	e.now = e.now.Add(2 * time.Hour)
	e.m.expire(ctx)
	if got, _ := e.st.GetThread(ctx, th.ID); got.State != store.ThreadExpired {
		t.Fatalf("thread = %s", got.State)
	}
	if m, _ := e.st.GetMessage(ctx, "m1"); m.State != store.MessageExpired {
		t.Fatalf("message = %s", m.State)
	}
}

func TestMS36_TakingUpNeverShortens(t *testing.T) {
	e := devicesEnv(t) // geraet-einbinden: 15 minutes
	ctx := context.Background()
	day := e.now.Add(24 * time.Hour)
	th, err := e.m.StartAt(ctx, Begin{Target: "geraet-einbinden", Request: "Hue Lampe",
		Plan: "# Neues Gerät\n- [ ] Einbinden\n- [ ] Raum", Deadline: day})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Take(ctx, e.annasTurn(t), th.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.GetThread(ctx, th.ID); !got.ExpiresAt.Equal(day) {
		t.Fatalf("taken up: expires %s, want %s", got.ExpiresAt, day)
	}
	// Two days later a step gives its type's timeout from then.
	e.now = e.now.Add(48 * time.Hour)
	if err := e.m.TickPlan(ctx, th.ID, "Einbinden"); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.GetThread(ctx, th.ID); !got.ExpiresAt.Equal(e.now.Add(15 * time.Minute)) {
		t.Fatalf("after a step: expires %s", got.ExpiresAt)
	}
}
