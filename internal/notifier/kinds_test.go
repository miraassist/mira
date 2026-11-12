package notifier

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/store"
)

// docs/spec/messages.md, Kinds of messages.

func (e *env) thread(id, place string) {
	e.t.Helper()
	if err := e.st.PutThread(context.Background(), &store.Thread{ID: id, Target: "postfach", Place: place,
		State: store.ThreadCollecting, ExpiresAt: e.now.Add(72 * 3600e9)}); err != nil {
		e.t.Fatal(err)
	}
}

func TestMS08_MessagesReadNamesEachMessagesKindSkillAndThread(t *testing.T) {
	e := indicateEnv(t)
	e.thread("t1", "person:anna")
	e.message("h1", 2, nil)
	e.message("p1", 2, func(m *store.Message) {
		m.Kind, m.Receiver, m.ThreadID, m.Spoken = store.KindPostfach, store.ReceiverHouse, "t1", "Elternbrief, 2 Termine"
	})
	r, err := e.n.ReadOut(context.Background(), "kueche", "", "")
	if err != nil || !strings.Contains(r.Text, "Hinweis h1") || strings.Contains(r.Text, "Elternbrief") {
		t.Fatalf("text = %q, %v", r.Text, err)
	}
	if len(r.Leads) != 1 || r.Leads[0] != (Lead{Kind: store.KindPostfach, Skill: "postfach", Thread: "t1", Line: "Elternbrief, 2 Termine"}) {
		t.Fatalf("leads = %+v", r.Leads)
	}
	if e.state("p1") != store.MessageDelivered {
		t.Error("the named message is not heard")
	}
}

func TestMS09_ASimpleMessageWithSeveralEntriesIsWordedAsOneText(t *testing.T) {
	e := indicateEnv(t)
	var asked string
	e.n.composer = func(_ context.Context, instruction string, _ json.RawMessage) (string, error) {
		asked = instruction
		return `{"text":"Ben lässt ausrichten, es wird später, und der Gasverbrauch ist hoch.","ids":["a"]}`, nil
	}
	e.message("a", 2, func(m *store.Message) { m.Spoken = "Ben lässt dir ausrichten: Es wird später." })
	e.message("b", 2, func(m *store.Message) { m.Spoken = "Der Gasverbrauch ist hoch." })
	r, err := e.n.ReadOut(context.Background(), "kueche", "", "")
	if err != nil || r.Text != "Ben lässt ausrichten, es wird später, und der Gasverbrauch ist hoch." {
		t.Fatalf("text = %q, %v", r.Text, err)
	}
	if !strings.Contains(asked, "Es wird später") || !strings.Contains(asked, "Gasverbrauch") {
		t.Errorf("instruction: %s", asked)
	}
}

func TestMS11_AMessageOfAnEndedThreadIsNotReadOut(t *testing.T) {
	e := indicateEnv(t)
	e.thread("t1", "person:anna")
	e.message("p1", 2, func(m *store.Message) {
		m.Kind, m.ThreadID, m.Spoken = store.KindPostfach, "t1", "Elternbrief"
	})
	// The thread ended, and taking its entries out failed.
	th, _ := e.st.GetThread(context.Background(), "t1")
	th.State = store.ThreadDone
	if err := e.st.PutThread(context.Background(), &th); err != nil {
		t.Fatal(err)
	}
	r, err := e.n.ReadOut(context.Background(), "kueche", "", "")
	if err != nil || len(r.Leads) != 0 || r.Text != "" {
		t.Fatalf("read = %+v, %v", r, err)
	}
	if m, _ := e.st.GetMessage(context.Background(), "p1"); m.State != store.MessageAcked {
		t.Errorf("state %s", m.State)
	}
	if !strings.Contains(e.logged(), "an entry of a thread that has ended") || !strings.Contains(e.logged(), "thread=t1") {
		t.Errorf("log:\n%s", e.logged())
	}
}
