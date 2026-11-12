package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/inbox"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/threads"
)

// docs/spec/messages.md, step 3: the inbox as a kind.

func inboxKindApp(t *testing.T) *app {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{inbox.Target: nil})
	if len(problems) > 0 {
		t.Fatalf("skills: %+v", problems)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &app{store: st, log: quiet, lang: i18n.DE,
		threads: threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: loaded, Logger: quiet})}
}

func askMail(t *testing.T, a *app, person, line string) store.Thread {
	t.Helper()
	ctx := context.Background()
	q := inbox.Question{Person: person, Doc: inbox.Doc{Key: "mail:x:" + line, Title: line, Ref: "Mail"}, Summary: line,
		Plan: line + "\n- [ ] Termin", Text: line, Deadline: time.Now().Add(24 * time.Hour)}
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
		if m.Kind == store.KindPostfach {
			out = append(out, m)
		}
	}
	return out
}

func TestMS12_TheFirstMailStartsItsThreadAndTheMessage(t *testing.T) {
	a := inboxKindApp(t)
	th := askMail(t, a, "anna", "Elternbrief der Grundschule: zwei Termine")
	if th.Target != inbox.Target || th.Person != "anna" {
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
