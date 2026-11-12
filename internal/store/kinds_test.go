package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// docs/spec/messages.md, Kinds of messages.

func TestMS07_AMessageWithoutAKindIsRefused(t *testing.T) {
	s, _ := newStore(t)
	_, _, err := s.PutMessage(context.Background(), Message{ID: "m1", Spoken: "Der Müll muss raus"})
	if err == nil || !strings.Contains(err.Error(), "needs a kind") || !strings.Contains(err.Error(), "Der Müll muss raus") {
		t.Fatalf("err = %v", err)
	}
}

func TestMS21_AFurtherSimpleTextWhileTheMessageIsUnheard(t *testing.T) {
	ctx := context.Background()
	s, clk := newStore(t)
	first, created, err := s.PutMessage(ctx, Message{ID: "m1", Kind: KindSimple, Receiver: "anna", Importance: 1,
		Spoken: "Es wird später.", Sender: "person:ben"})
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	clk.Add(time.Minute)
	second, created, err := s.PutMessage(ctx, Message{ID: "m2", Kind: KindSimple, Receiver: "anna", Importance: 2,
		Spoken: "Der Gasverbrauch ist hoch.", Source: SourceTask})
	if err != nil || created {
		t.Fatalf("second: created=%v err=%v", created, err)
	}
	if second.ID != first.ID || len(second.Entries) != 2 || second.Importance != 2 ||
		second.Spoken != "Es wird später.\nDer Gasverbrauch ist hoch." {
		t.Fatalf("message = %+v", second)
	}
	all, _ := s.ListMessages(ctx, MessageFilter{States: []string{MessagePending}, Receiver: "anna"})
	if len(all) != 1 {
		t.Fatalf("%d unheard simple messages for Anna", len(all))
	}
	// Heard, then more comes: a new message (decision 4).
	if err := s.MarkDelivered(ctx, first.ID, "voice:kueche"); err != nil {
		t.Fatal(err)
	}
	third, created, err := s.PutMessage(ctx, Message{ID: "m3", Kind: KindSimple, Receiver: "anna", Spoken: "Post ist da."})
	if err != nil || !created || third.ID == first.ID {
		t.Fatalf("after heard: created=%v id=%s err=%v", created, third.ID, err)
	}
}

func TestMS22_AnEntryWithTheSameKeyReplacesTheOneBefore(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	s.PutMessage(ctx, Message{ID: "m1", Kind: KindSimple, Spoken: "Es wird später."})
	s.PutMessage(ctx, Message{ID: "g1", Kind: KindSimple, DedupeKey: "gas", Spoken: "Gas hoch."})
	m, created, err := s.PutMessage(ctx, Message{ID: "g2", Kind: KindSimple, DedupeKey: "gas", Spoken: "Gas sehr hoch."})
	if err != nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	gas := ""
	for _, e := range m.Entries {
		if e.ID == "g1" {
			gas = e.Text
		}
	}
	if len(m.Entries) != 2 || gas != "Gas sehr hoch." {
		t.Fatalf("entries = %+v", m.Entries)
	}
}

func TestMS23_TwoRemindersStayTwo(t *testing.T) {
	ctx := context.Background()
	s, clk := newStore(t)
	for i, at := range []time.Duration{7 * time.Hour, 18 * time.Hour} {
		if _, err := s.ScheduleMessage(ctx, Message{ID: []string{"r1", "r2"}[i], Kind: KindReminder, Receiver: "anna",
			Spoken: "Erinnerung", DeliverAt: clk.Now().Add(at)}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.ListMessages(ctx, MessageFilter{States: []string{MessageScheduled}})
	if len(all) != 2 {
		t.Fatalf("%d reminders", len(all))
	}
}

func TestMS24_AnUrgentMessageIsNotAddedToASimpleOne(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	s.PutMessage(ctx, Message{ID: "m1", Kind: KindSimple, Spoken: "Post ist da."})
	u, created, err := s.PutMessage(ctx, Message{ID: "u1", Kind: KindUrgent, Importance: 3, Spoken: "Wasser im Keller!"})
	if err != nil || !created || u.ID != "u1" || len(u.Entries) != 1 {
		t.Fatalf("urgent = %+v created=%v err=%v", u, created, err)
	}
	if m, _ := s.GetMessage(ctx, "m1"); len(m.Entries) != 1 {
		t.Fatalf("simple got %d entries", len(m.Entries))
	}
}

func TestMS25_APrivateMessageIsNotAddedToASimpleOne(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	s.PutMessage(ctx, Message{ID: "m1", Kind: KindSimple, Receiver: "anna", Spoken: "Post ist da."})
	p, created, err := s.PutMessage(ctx, Message{ID: "p1", Kind: KindPrivate, Receiver: "anna", Spoken: "Dein Kontostand."})
	if err != nil || !created || !p.Private() {
		t.Fatalf("private = %+v created=%v err=%v", p, created, err)
	}
	// The kind says it; a field against it is the caller's error.
	if _, _, err := s.PutMessage(ctx, Message{ID: "x", Kind: KindSimple, Confidentiality: ConfidentialityPrivate,
		Spoken: "x"}); err == nil {
		t.Fatal("a simple message that is private was taken")
	}
}

func TestMS30_AnEndedThreadsEntriesLeaveTheMessage(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	s.PutMessage(ctx, Message{ID: "e1", Kind: KindPostfach, Receiver: "anna", Spoken: "Elternbrief", ThreadID: "t1"})
	m, _, _ := s.PutMessage(ctx, Message{ID: "e2", Kind: KindPostfach, Receiver: "anna", Spoken: "Praxis", ThreadID: "t2"})
	if len(m.Entries) != 2 || m.ThreadID != "" {
		t.Fatalf("message = %+v", m)
	}
	if n, err := s.WithdrawThreadMessages(ctx, "t1"); err != nil || n != 1 {
		t.Fatalf("withdrawn %d, %v", n, err)
	}
	m, _ = s.GetMessage(ctx, "e1")
	if m.State != MessagePending || len(m.Entries) != 1 || m.ThreadID != "t2" || m.Spoken != "Praxis" {
		t.Fatalf("after t1: %+v", m)
	}
	s.WithdrawThreadMessages(ctx, "t2")
	if m, _ = s.GetMessage(ctx, "e1"); m.State != MessageExpired {
		t.Fatalf("after t2: state %s", m.State)
	}
}

// Done is per entry (decision 18): the last one ends the message.
func TestAckEntryTakesOneOut(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	s.PutMessage(ctx, Message{ID: "a", Kind: KindSimple, Spoken: "A."})
	s.PutMessage(ctx, Message{ID: "b", Kind: KindSimple, Spoken: "B."})
	if err := s.AckEntry(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	m, _ := s.GetMessage(ctx, "a")
	if m.State != MessagePending || m.Spoken != "A." {
		t.Fatalf("after b: %+v", m)
	}
	s.AckEntry(ctx, "a")
	if m, _ = s.GetMessage(ctx, "a"); m.State != MessageAcked {
		t.Fatalf("after a: %s", m.State)
	}
	if err := s.AckEntry(ctx, "gibtsnicht"); err != ErrNotFound {
		t.Fatalf("unknown: %v", err)
	}
}

func TestDropOldFormMessages(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO messages(id, source, dedupe_key, importance, receiver, spoken, details,
created_at, updated_at, expires_at, state, delivered_via, delivered_at, confidentiality) VALUES
('old', 'x', '', 1, 'house', 'Alt', '', '', '', '', 'pending', '', '', 'normal')`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.DropOldFormMessages(ctx); err != nil || n != 1 {
		t.Fatalf("dropped %d, %v", n, err)
	}
}
