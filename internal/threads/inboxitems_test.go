package threads

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
)

// docs/spec/inbox.md, section 8, and docs/spec/threads.md, Started by code:
// an inbox item becomes a thread of the skill that takes its kind, with its
// fields filled; it acts on the person's yes only.

type calledAction struct {
	action string
	data   map[string]any
}

type itemActions struct{ calls []calledAction }

func (f *itemActions) Has(context.Context, string) (bool, error) { return true, nil }
func (f *itemActions) Call(_ context.Context, action string, data map[string]any) error {
	f.calls = append(f.calls, calledAction{action, data})
	return nil
}
func (f *itemActions) Place(context.Context, string, string) map[string]string { return nil }

func itemsEnv(t *testing.T) (*env, *itemActions) {
	t.Helper()
	e := setup(t)
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"termin": nil, "todo": nil})
	if len(problems) > 0 || len(loaded) != 2 {
		t.Fatalf("skills: %+v", problems)
	}
	acts := &itemActions{}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Targets: loaded, Actions: acts, Clock: func() time.Time { return e.now }})
	return e, acts
}

// item starts an item's thread and puts its entry, as the service's
// inboxAsk does.
func (e *env) item(t *testing.T, target, kind, line string, fields map[string]string, deadline time.Time) store.Thread {
	t.Helper()
	ctx := context.Background()
	th, err := e.m.StartAt(ctx, Begin{Target: target, Person: "anna", Request: line, Fields: fields, Deadline: deadline,
		Source: "paperless:5444"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.st.PutMessage(ctx, store.Message{ID: "m-" + th.ID, Kind: kind, Receiver: "anna", Spoken: line,
		ThreadID: th.ID, Importance: 1}); err != nil {
		t.Fatal(err)
	}
	return th
}

func (e *env) entriesOpen(t *testing.T, threadID string) int {
	t.Helper()
	ms, err := e.st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}, Receiver: "anna"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range ms {
		for _, en := range m.Entries {
			if en.ThreadID == threadID {
				n++
			}
		}
	}
	return n
}

func TestIN46_AnAppointmentStartsAThreadOfTerminWaitingForAYes(t *testing.T) {
	e, acts := itemsEnv(t)
	start := e.now.Add(48 * time.Hour)
	th := e.item(t, "termin", store.KindAppointment, "Elternbrief: Elternabend",
		map[string]string{"title": "Elternabend", "proposals": "2026-10-17 19:30–20:30 · Aula", "description": "aus Paperless 5444"}, start)
	if th.Target != "termin" || th.State != store.ThreadWaiting || th.Fields["title"] != "Elternabend" || th.Source != "paperless:5444" ||
		!th.ExpiresAt.Equal(start) {
		t.Fatalf("thread = %+v", th)
	}
	// Complete, it proposes and does not finish by itself.
	ctx := context.Background()
	inv := e.annasTurn(t)
	if _, err := e.m.Take(ctx, inv, th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Fill(ctx, inv, map[string]string{"start": "2026-10-17 19:30", "end": "2026-10-17 20:30", "location": "Aula",
		"calendar": "standard"}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.st.GetThread(ctx, th.ID)
	if got.State != store.ThreadProposed || len(acts.calls) != 0 {
		t.Fatalf("state %s, calls %+v", got.State, acts.calls)
	}
	// The fields are marked as read from a foreign document.
	text, _ := e.m.Context(ctx, store.Session{Key: inv.SessionKey, ThreadID: th.ID}, "person:anna", nil, "")
	if !strings.Contains(text, "fremden Dokument") {
		t.Fatalf("context:\n%s", text)
	}
	// Unknown fields and a missing source are refused.
	if _, err := e.m.StartAt(ctx, Begin{Target: "termin", Person: "anna", Fields: map[string]string{"betrag": "1"}, Source: "x"}); err == nil {
		t.Fatal("an unknown field was taken")
	}
	if _, err := e.m.StartAt(ctx, Begin{Target: "termin", Person: "anna", Fields: map[string]string{"title": "x"}}); err == nil {
		t.Fatal("a thread without a source was started")
	}
}

func TestIN47_AToDoStartsAThreadOfTodo(t *testing.T) {
	e, _ := itemsEnv(t)
	end := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	th := e.item(t, "todo", store.KindTodo, "Steuerberater: Steuererklärung bis Fr 16.10.",
		map[string]string{"item": "Steuererklärung abgeben", "due": "2026-10-16"}, end)
	if th.Target != "todo" || th.State != store.ThreadWaiting || th.Fields["due"] != "2026-10-16" || !th.ExpiresAt.Equal(end) {
		t.Fatalf("thread = %+v", th)
	}
}

func TestIN50_TheYesRunsTheFinishOfTermin(t *testing.T) {
	e, acts := itemsEnv(t)
	ctx := context.Background()
	th := e.item(t, "termin", store.KindAppointment, "Elternbrief: Elternabend",
		map[string]string{"title": "Elternabend", "proposals": "2026-12-03 19:30–20:30 · Aula"}, e.now.Add(48*time.Hour))
	inv := e.annasTurn(t)
	e.m.Take(ctx, inv, th.ID)
	if _, err := e.m.Fill(ctx, inv, map[string]string{"start": "2026-12-03 19:30", "end": "2026-12-03 20:30", "location": "Aula",
		"calendar": "standard"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Confirm(ctx, inv); err != nil {
		t.Fatal(err)
	}
	if len(acts.calls) != 1 || acts.calls[0].action != "calendar.create" || acts.calls[0].data["title"] != "Elternabend" ||
		acts.calls[0].data["start"] != "2026-12-03 19:30" || acts.calls[0].data["person"] != "anna" {
		t.Fatalf("calls = %+v", acts.calls)
	}
	got, _ := e.st.GetThread(ctx, th.ID)
	if got.State != store.ThreadDone || e.entriesOpen(t, th.ID) != 0 {
		t.Fatalf("state %s, entries %d", got.State, e.entriesOpen(t, th.ID))
	}
}

func TestIN51_TheYesRunsTheFinishOfTodo(t *testing.T) {
	e, acts := itemsEnv(t)
	ctx := context.Background()
	th := e.item(t, "todo", store.KindTodo, "Steuerberater: Steuererklärung",
		map[string]string{"item": "Steuererklärung abgeben", "due": "2026-11-16"}, e.now.Add(48*time.Hour))
	inv := e.annasTurn(t)
	e.m.Take(ctx, inv, th.ID)
	if _, err := e.m.Fill(ctx, inv, map[string]string{"list": "standard"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Confirm(ctx, inv); err != nil {
		t.Fatal(err)
	}
	if len(acts.calls) != 1 || acts.calls[0].action != "todo.create" || acts.calls[0].data["item"] != "Steuererklärung abgeben" ||
		acts.calls[0].data["due_date"] != "2026-11-16" || acts.calls[0].data["person"] != "anna" {
		t.Fatalf("calls = %+v", acts.calls)
	}
	if got, _ := e.st.GetThread(ctx, th.ID); got.State != store.ThreadDone || e.entriesOpen(t, th.ID) != 0 {
		t.Fatalf("thread = %+v", got)
	}
}

func TestIN53_NoAnswerUntilTheExpiry(t *testing.T) {
	e, acts := itemsEnv(t)
	ctx := context.Background()
	end := e.now.Add(26 * time.Hour)
	th := e.item(t, "todo", store.KindTodo, "Steuerberater: Steuererklärung",
		map[string]string{"item": "Steuererklärung abgeben", "due": "2026-10-16"}, end)
	e.now = end.Add(time.Minute)
	e.m.expire(ctx)
	got, _ := e.st.GetThread(ctx, th.ID)
	if got.State != store.ThreadExpired || e.entriesOpen(t, th.ID) != 0 || len(acts.calls) != 0 {
		t.Fatalf("state %s, entries %d, calls %d", got.State, e.entriesOpen(t, th.ID), len(acts.calls))
	}
}

func TestIN58_JaConfirmsAnAppointmentAndAToDoOneAfterAnother(t *testing.T) {
	e, acts := itemsEnv(t)
	ctx := context.Background()
	appt := e.item(t, "termin", store.KindAppointment, "Elternbrief: Elternabend Do 3.12. 19:30, Aula",
		map[string]string{"title": "Elternabend", "proposals": "2026-12-03 19:30–20:30 · Aula"}, e.now.Add(48*time.Hour))
	todo := e.item(t, "todo", store.KindTodo, "Elternbrief: Rückmeldung Ausflug bis Fr 28.11.",
		map[string]string{"item": "Rückmeldung Ausflug", "due": "2026-11-28"}, e.now.Add(48*time.Hour))
	inv := e.annasTurn(t)
	for _, step := range []struct {
		id     string
		fields map[string]string
	}{
		{appt.ID, map[string]string{"start": "2026-12-03 19:30", "end": "2026-12-03 20:30", "location": "Aula", "calendar": "standard"}},
		{todo.ID, map[string]string{"list": "standard"}},
	} {
		if _, err := e.m.Take(ctx, inv, step.id); err != nil {
			t.Fatal(err)
		}
		if _, err := e.m.Fill(ctx, inv, step.fields); err != nil {
			t.Fatal(err)
		}
		if _, err := e.m.Confirm(ctx, inv); err != nil {
			t.Fatal(err)
		}
	}
	if len(acts.calls) != 2 || acts.calls[0].action != "calendar.create" || acts.calls[1].action != "todo.create" {
		t.Fatalf("calls = %+v", acts.calls)
	}
}

// Without thread_take, the answer's turn finds the waiting thread at its
// place; linking it ends its starter's expiry, and filling it then proposes
// without a conflict (seen in the inbox's eval, 2026-11-15).
func TestAnItemsThreadFilledWithoutTakingItUp(t *testing.T) {
	e, acts := itemsEnv(t)
	ctx := context.Background()
	th := e.item(t, "termin", store.KindAppointment, "Elternbrief: Elternabend",
		map[string]string{"title": "Elternabend", "proposals": "2026-12-03 19:30–20:30 · Aula"}, e.now.Add(48*time.Hour))
	inv := e.annasTurn(t)
	if _, err := e.m.Fill(ctx, inv, map[string]string{"start": "2026-12-03 19:30", "end": "2026-12-03 20:30",
		"calendar": "standard"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.GetThread(ctx, th.ID); got.State != store.ThreadProposed {
		t.Fatalf("state %s", got.State)
	}
	if _, err := e.m.Confirm(ctx, inv); err != nil || len(acts.calls) != 1 {
		t.Fatalf("confirm: %v, calls %+v", err, acts.calls)
	}
}

// docs/spec/inbox.md, decision 34: thread_confirm with an id and fields
// takes up, fills and says yes in one call, per thread, side by side.
func TestThreadConfirmWithIDAndFieldsInOneCall(t *testing.T) {
	e, acts := itemsEnv(t)
	ctx := context.Background()
	appt := e.item(t, "termin", store.KindAppointment, "Elternbrief: Elternabend",
		map[string]string{"title": "Elternabend", "proposals": "2026-12-03 19:30–20:30 · Aula"}, e.now.Add(48*time.Hour))
	todo := e.item(t, "todo", store.KindTodo, "Elternbrief: Rückmeldung",
		map[string]string{"item": "Rückmeldung Ausflug", "due": "2026-11-28"}, e.now.Add(48*time.Hour))
	inv := e.annasTurn(t)
	if _, err := e.m.ConfirmWith(ctx, inv, appt.ID, map[string]string{"start": "2026-12-03 19:30", "end": "2026-12-03 20:30",
		"calendar": "standard"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.ConfirmWith(ctx, inv, todo.ID, map[string]string{"list": "standard"}); err != nil {
		t.Fatal(err)
	}
	if len(acts.calls) != 2 || acts.calls[0].action != "calendar.create" || acts.calls[1].action != "todo.create" {
		t.Fatalf("calls = %+v", acts.calls)
	}
	// A thread that is not the asker's is refused.
	ben := e.item(t, "todo", store.KindTodo, "x", map[string]string{"item": "x"}, e.now.Add(time.Hour))
	if _, err := e.st.GetThread(ctx, ben.ID); err != nil {
		t.Fatal(err)
	}
	other := e.annasTurn(t)
	other.Person, other.Place = "ben", "person:ben"
	if _, err := e.m.ConfirmWith(ctx, other, ben.ID, map[string]string{"list": "standard"}); err == nil {
		t.Fatal("another person's thread was confirmed")
	}
}
