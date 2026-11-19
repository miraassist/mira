package notifier

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/miraassist/mira/internal/store"
)

// The delivery record (docs/spec/telegram-history.md, step 3).

type recorded struct{ key, record, ack string }

type fakeRecord struct {
	mu   sync.Mutex
	got  []recorded
	fail error
}

func (f *fakeRecord) record(_ context.Context, key, record, ack string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.got = append(f.got, recorded{key, record, ack})
	return nil
}

// withChat gives Ben's channel a chat's history and the notifier the record.
func withChat(t *testing.T, e *env, history bool) *fakeRecord {
	t.Helper()
	f := &fakeRecord{}
	e.n.record = f.record
	if history {
		if _, err := e.st.UpsertSession(context.Background(), store.Session{Key: "telegram:notify.ben", Channel: "telegram",
			ConversationID: "notify.ben"}, 0); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// TC-08 A delivery to a chat is recorded in the chat's history
func TestTC08_ADeliveryToAChatIsRecordedInTheChatsHistory(t *testing.T) {
	e := newEnv(t, nil)
	f := withChat(t, e, true)
	e.message("posts", 1, private("ben"))
	e.now = at("08:00")
	if sent := e.tick(); len(sent) != 1 {
		t.Fatalf("sent = %v", sent)
	}
	if len(f.got) != 1 {
		t.Fatalf("records: %+v", f.got)
	}
	r := f.got[0]
	if r.key != "telegram:notify.ben" {
		t.Errorf("key = %q", r.key)
	}
	if !strings.Contains(r.record, "Von Mira selbst in diesen Chat geschickt") || !strings.Contains(r.record, "Nachricht posts") ||
		!strings.Contains(r.record, "Art "+store.KindPrivate) {
		t.Errorf("record = %q", r.record)
	}
	if r.ack != e.tg.sent["notify.ben"][0] {
		t.Errorf("ack = %q, sent %q", r.ack, e.tg.sent["notify.ben"][0])
	}
}

// TC-09 A delivery to another channel is not recorded
func TestTC09_ADeliveryToAnotherChannelIsNotRecorded(t *testing.T) {
	e := newEnv(t, nil)
	f := withChat(t, e, false)
	e.message("posts", 1, private("ben"))
	e.now = at("08:00")
	if sent := e.tick(); len(sent) != 1 {
		t.Fatalf("sent = %v", sent)
	}
	if len(f.got) != 0 {
		t.Errorf("records: %+v", f.got)
	}
}

// TC-10 A failed record leaves the delivery delivered
func TestTC10_AFailedRecordLeavesTheDeliveryDelivered(t *testing.T) {
	e := newEnv(t, nil)
	f := withChat(t, e, true)
	f.fail = errors.New("disk full")
	e.message("posts", 1, private("ben"))
	e.now = at("08:00")
	e.tick()
	if st := e.state("posts"); st != store.MessageDelivered {
		t.Errorf("state = %q, want delivered", st)
	}
	log := e.logged()
	if !strings.Contains(log, "could not write the delivery record") || !strings.Contains(log, "message=posts") ||
		!strings.Contains(log, "session=telegram:notify.ben") {
		t.Errorf("log: %s", log)
	}
}

// TC-11 A failed send writes no record
func TestTC11_AFailedSendWritesNoRecord(t *testing.T) {
	e := newEnv(t, nil)
	f := withChat(t, e, true)
	e.tg.fail = map[string]error{"notify.ben": errors.New("telegram down")}
	e.message("posts", 1, private("ben"))
	e.now = at("08:00")
	e.tick()
	if len(f.got) != 0 {
		t.Errorf("records: %+v", f.got)
	}
}
