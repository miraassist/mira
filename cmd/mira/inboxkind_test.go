package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/inbox"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/threads"
)

// docs/spec/messages.md, step 3: the inbox's items as kinds (since the
// inbox's step 10, decision 33: appointment and todo).

func inboxKindApp(t *testing.T) *app {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"termin": nil, "todo": nil})
	if len(problems) > 0 {
		t.Fatalf("skills: %+v", problems)
	}
	takers, _ := kindTakers(loaded)
	store.SetTakers(takers)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &app{store: st, log: quiet, lang: i18n.DE,
		threads: threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: loaded, Logger: quiet})}
}

func askMail(t *testing.T, a *app, person, line string) store.Thread {
	t.Helper()
	ctx := context.Background()
	q := inbox.Question{Person: person, Doc: inbox.Doc{Key: "mail:x:" + line, Title: line, Ref: "Mail"},
		Kind: store.KindAppointment, Line: line, Fields: map[string]string{"title": line, "proposals": "2026-12-03 19:30–20:30"},
		Deadline: time.Now().Add(24 * time.Hour)}
	if err := a.inboxAsk(ctx, q); err != nil {
		t.Fatal(err)
	}
	open, err := a.store.OpenThreads(ctx, "person:"+person)
	if err != nil {
		t.Fatal(err)
	}
	for _, th := range open {
		if th.Request == line {
			return th
		}
	}
	t.Fatalf("no thread for %q", line)
	return store.Thread{}
}

func postfachMessages(t *testing.T, a *app, person string) []store.Message {
	t.Helper()
	all, err := a.store.ListMessages(context.Background(), store.MessageFilter{
		States: []string{store.MessagePending}, Receiver: person})
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Message
	for _, m := range all {
		if m.Kind == store.KindAppointment {
			out = append(out, m)
		}
	}
	return out
}

func TestMS12_TheFirstMailStartsItsThreadAndTheMessage(t *testing.T) {
	a := inboxKindApp(t)
	th := askMail(t, a, "anna", "Elternbrief der Grundschule: zwei Termine")
	if th.Target != "termin" || th.Person != "anna" || th.State != store.ThreadWaiting || th.Source == "" {
		t.Fatalf("thread = %+v", th)
	}
	ms := postfachMessages(t, a, "anna")
	if len(ms) != 1 || len(ms[0].Entries) != 1 || ms[0].Entries[0].ThreadID != th.ID ||
		ms[0].Entries[0].Text != "Elternbrief der Grundschule: zwei Termine" {
		t.Fatalf("messages = %+v", ms)
	}
}

func TestMS13_AFurtherMailWhileTheMessageIsUnheard(t *testing.T) {
	a := inboxKindApp(t)
	first := askMail(t, a, "anna", "Elternbrief")
	second := askMail(t, a, "anna", "Terminvorschläge der Praxis")
	if first.ID == second.ID {
		t.Fatal("the second mail has no thread of its own")
	}
	ms := postfachMessages(t, a, "anna")
	if len(ms) != 1 || len(ms[0].Entries) != 2 || ms[0].Entries[1].ThreadID != second.ID {
		t.Fatalf("messages = %+v", ms)
	}
}

func TestMS14_AFurtherMailAfterTheMessageWasHeard(t *testing.T) {
	a := inboxKindApp(t)
	first := askMail(t, a, "anna", "Elternbrief")
	heard := postfachMessages(t, a, "anna")[0]
	if err := a.store.MarkDelivered(context.Background(), heard.ID, "voice:kueche"); err != nil {
		t.Fatal(err)
	}
	second := askMail(t, a, "anna", "Terminvorschläge der Praxis")
	if got, _ := a.store.GetThread(context.Background(), first.ID); got.State == store.ThreadExpired {
		t.Fatal("the first thread ended")
	}
	ms := postfachMessages(t, a, "anna")
	if len(ms) != 1 || ms[0].ID == heard.ID || len(ms[0].Entries) != 1 || ms[0].Entries[0].ThreadID != second.ID {
		t.Fatalf("messages = %+v", ms)
	}
}

// docs/spec/inbox.md, step 10: each item is an entry of the message of its
// kind; no message of kind postfach.
func TestIN48_EachItemIsAnEntryOfTheMessageOfItsKind(t *testing.T) {
	a := inboxKindApp(t)
	ctx := context.Background()
	doc := inbox.Doc{Key: "paperless:5444", Title: "Elternbrief", Ref: "Paperless 5444"}
	deadline := time.Now().Add(48 * time.Hour)
	if err := a.inboxAsk(ctx, inbox.Question{Person: "anna", Doc: doc, Kind: store.KindAppointment, Line: "Elternbrief: Elternabend",
		Fields: map[string]string{"title": "Elternabend", "proposals": "2026-12-03 19:30–20:30"}, Deadline: deadline}); err != nil {
		t.Fatal(err)
	}
	if err := a.inboxAsk(ctx, inbox.Question{Person: "anna", Doc: doc, Kind: store.KindTodo, Line: "Elternbrief: Rückmeldung",
		Fields: map[string]string{"item": "Rückmeldung Ausflug", "due": "2026-11-28"}, Deadline: deadline}); err != nil {
		t.Fatal(err)
	}
	all, _ := a.store.ListMessages(ctx, store.MessageFilter{States: []string{store.MessagePending}, Receiver: "anna"})
	kinds := map[string]int{}
	for _, m := range all {
		kinds[m.Kind] += len(m.Entries)
	}
	if kinds[store.KindAppointment] != 1 || kinds[store.KindTodo] != 1 || kinds["postfach"] != 0 || len(kinds) != 2 {
		t.Fatalf("entries by kind = %v", kinds)
	}
	if !a.threads.OpenFrom(ctx, "paperless:5444") {
		t.Fatal("the document's threads are not found by their source")
	}
	if got := a.inboxKinds(); len(got) != 2 || len(got[store.KindTodo]) == 0 {
		t.Fatalf("kinds = %v", got)
	}
}

// docs/spec/inbox.md, step 10: the kinds' instructions offer what will be
// done.
func TestIN54_TheKindsInstructionsOfferWhatWillBeDone(t *testing.T) {
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"termin": nil, "todo": nil})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	for _, sk := range loaded {
		text := sk.Section(i18n.DE, skills.SectionAnnounce)
		if text == "" || strings.Contains(strings.ToLower(text), "durchgehen") && !strings.Contains(text, "nie „durchgehen“") {
			t.Errorf("%s: %q", sk.Name, text)
		}
	}
}

// docs/spec/messages.md, decision 33: two switched-on skills taking the same
// kind make it nobody's, and each is a problem.
func TestIN57_TwoSwitchedOnSkillsTakingTheSameKind(t *testing.T) {
	takers, clash := kindTakers([]skills.Skill{{Name: "todo", Takes: "todo"}, {Name: "aufgaben", Takes: "todo"},
		{Name: "termin", Takes: "appointment"}})
	if takers["todo"] != "" || takers["appointment"] != "termin" || len(clash) != 2 ||
		clash[0] != (takeClash{"todo", "aufgaben", "todo"}) || clash[1] != (takeClash{"todo", "todo", "aufgaben"}) {
		t.Fatalf("takers %v, clash %+v", takers, clash)
	}
	p := skills.NewProblem("aufgaben", skills.KindTakesTwice, "todo", "todo")
	if text := p.Text(i18n.DE); !strings.Contains(text, "todo") || !strings.Contains(text, "Schalte einen der beiden aus") {
		t.Fatalf("problem = %q", text)
	}
	// With aufgaben switched off, todo takes the kind again.
	takers, clash = kindTakers([]skills.Skill{{Name: "todo", Takes: "todo"}})
	if takers["todo"] != "todo" || len(clash) != 0 {
		t.Fatalf("takers %v", takers)
	}
}
