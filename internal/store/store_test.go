package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// testClock is a manually advanced clock, so TTL and expiry behaviour is
// tested without sleeping.
type testClock struct{ t time.Time }

func (c *testClock) Now() time.Time      { return c.t }
func (c *testClock) Add(d time.Duration) { c.t = c.t.Add(d) }

func newStore(t *testing.T) (*Store, *testClock) {
	t.Helper()
	clk := &testClock{t: time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)}
	s, err := Open(context.Background(), Options{
		Path:  filepath.Join(t.TempDir(), "test.db"),
		Clock: clk.Now,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, clk
}

func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "m.db")
	s1, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	first, _ := s1.AppliedMigrations(ctx)
	s1.Close()

	s2, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer s2.Close()
	second, _ := s2.AppliedMigrations(ctx)

	if len(first) != len(migrations) || len(second) != len(migrations) {
		t.Fatalf("applied %d then %d, want %d both times", len(first), len(second), len(migrations))
	}
}

func TestSessionRoundTripAndExpiry(t *testing.T) {
	ctx := context.Background()
	s, clk := newStore(t)

	sess, err := s.UpsertSession(ctx, Session{
		Key: "voice:abc", Channel: "voice", ConversationID: "abc", DeviceID: "dev1", AreaID: "kueche",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if sess.AreaID != "kueche" {
		t.Errorf("area = %q", sess.AreaID)
	}

	if err := s.AppendMessages(ctx, "voice:abc",
		Message{Role: "user", Content: "Licht an"},
		Message{Role: "assistant", Content: "Erledigt."},
	); err != nil {
		t.Fatalf("AppendMessages: %v", err)
	}
	if err := s.AppendMessages(ctx, "voice:abc", Message{Role: "user", Content: "Danke"}); err != nil {
		t.Fatalf("AppendMessages second batch: %v", err)
	}
	msgs, err := s.Messages(ctx, "voice:abc")
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	for i, m := range msgs {
		if m.Seq != i+1 {
			t.Errorf("message %d has seq %d, want %d", i, m.Seq, i+1)
		}
	}

	clk.Add(10 * time.Minute)
	n, err := s.PurgeExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("PurgeExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("purged %d sessions, want 1", n)
	}
	if _, err := s.GetSession(ctx, "voice:abc"); err != ErrNotFound {
		t.Errorf("session survived purge: %v", err)
	}
	if msgs, _ := s.Messages(ctx, "voice:abc"); len(msgs) != 0 {
		t.Errorf("transcript survived the cascade: %d messages", len(msgs))
	}
}

func TestFactsFullTextSearch(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)

	for _, f := range []Fact{
		{ID: "1", Text: "Der Wechselrichter steht im Keller", Tags: []string{"haus", "pv"}},
		{ID: "2", Text: "Stefan trinkt morgens Kaffee", Tags: []string{"person"}},
		{ID: "3", Text: "Die Katze heisst Mimi", Tags: []string{"person"}},
	} {
		if _, err := s.SaveFact(ctx, f); err != nil {
			t.Fatalf("SaveFact %s: %v", f.ID, err)
		}
	}

	got, err := s.SearchFacts(ctx, "Wechselrichter", 10)
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("search returned %+v", got)
	}

	// FTS5 syntax characters in user input must not blow up the query.
	if _, err := s.SearchFacts(ctx, `Kaffee OR "unbalanced (`, 10); err != nil {
		t.Fatalf("search with syntax characters: %v", err)
	}

	if err := s.DeleteFact(ctx, "1"); err != nil {
		t.Fatalf("DeleteFact: %v", err)
	}
	got, _ = s.SearchFacts(ctx, "Wechselrichter", 10)
	if len(got) != 0 {
		t.Errorf("deleted fact still in the index: %+v", got)
	}
}

func TestNoticeDedupeKeepsOneOpenRow(t *testing.T) {
	ctx := context.Background()
	s, clk := newStore(t)
	exp := clk.Now().Add(12 * time.Hour)

	first, created, err := s.PutNotice(ctx, Notice{
		ID: "n1", Source: SourceHATrigger, DedupeKey: "inverter:fault",
		Importance: 2, Spoken: "Der Wechselrichter meldet einen Fehler.", ExpiresAt: exp,
	})
	if err != nil || !created {
		t.Fatalf("first put: created=%v err=%v", created, err)
	}

	// The sensor flaps: same dedupe key, new id offered.
	second, created, err := s.PutNotice(ctx, Notice{
		ID: "n2", Source: SourceHATrigger, DedupeKey: "inverter:fault",
		Importance: 3, Spoken: "Der Wechselrichter meldet weiterhin einen Fehler.", ExpiresAt: exp,
	})
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
	if created {
		t.Error("flapping sensor created a second notice")
	}
	if second.ID != first.ID {
		t.Errorf("id changed from %s to %s", first.ID, second.ID)
	}
	if second.Importance != 3 {
		t.Errorf("importance not updated: %d", second.Importance)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("created_at was overwritten")
	}

	all, _ := s.ListNotices(ctx, NoticeFilter{States: []string{NoticePending}})
	if len(all) != 1 {
		t.Fatalf("got %d pending notices, want 1", len(all))
	}

	// Once acked, the key is free again and a new event opens a fresh notice.
	if err := s.SetNoticeState(ctx, first.ID, NoticeAcked); err != nil {
		t.Fatalf("SetNoticeState: %v", err)
	}
	_, created, err = s.PutNotice(ctx, Notice{
		ID: "n3", Source: SourceHATrigger, DedupeKey: "inverter:fault",
		Importance: 2, Spoken: "Erneut ein Fehler.", ExpiresAt: exp,
	})
	if err != nil || !created {
		t.Fatalf("put after ack: created=%v err=%v", created, err)
	}
}

func TestNoticeDeliveryAndExpiry(t *testing.T) {
	ctx := context.Background()
	s, clk := newStore(t)

	if _, _, err := s.PutNotice(ctx, Notice{
		ID: "n1", Source: SourceExternal, Importance: 2, Spoken: "Hallo",
		ExpiresAt: clk.Now().Add(1 * time.Hour),
	}); err != nil {
		t.Fatalf("PutNotice: %v", err)
	}
	if n, _ := s.CountPendingNotices(ctx); n != 1 {
		t.Fatalf("pending = %d, want 1", n)
	}
	if err := s.MarkDelivered(ctx, "n1", "voice"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if err := s.MarkDelivered(ctx, "n1", "voice"); err != ErrNotFound {
		t.Errorf("delivering twice should not succeed, got %v", err)
	}
	if n, _ := s.CountPendingNotices(ctx); n != 0 {
		t.Errorf("pending after delivery = %d, want 0", n)
	}

	clk.Add(2 * time.Hour)
	n, err := s.ExpireNotices(ctx)
	if err != nil {
		t.Fatalf("ExpireNotices: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired %d, want 1", n)
	}
	got, _ := s.GetNotice(ctx, "n1")
	if got.State != NoticeExpired {
		t.Errorf("state = %q, want expired", got.State)
	}
}

func TestListNoticesOrdersByImportanceThenAge(t *testing.T) {
	ctx := context.Background()
	s, clk := newStore(t)
	exp := clk.Now().Add(time.Hour)

	mustPut := func(id string, imp int) {
		t.Helper()
		if _, _, err := s.PutNotice(ctx, Notice{ID: id, Source: SourceExternal, Importance: imp, Spoken: id, ExpiresAt: exp}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
		clk.Add(time.Second)
	}
	mustPut("low", 0)
	mustPut("high-old", 3)
	mustPut("high-new", 3)
	mustPut("mid", 1)

	got, err := s.ListNotices(ctx, NoticeFilter{States: []string{NoticePending}, MinImportance: 1})
	if err != nil {
		t.Fatalf("ListNotices: %v", err)
	}
	want := []string{"high-old", "high-new", "mid"}
	if len(got) != len(want) {
		t.Fatalf("got %d notices, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("position %d = %s, want %s", i, got[i].ID, want[i])
		}
	}
}

func TestAnnouncementFollowUpWindow(t *testing.T) {
	ctx := context.Background()
	s, clk := newStore(t)

	if err := s.RecordAnnouncement(ctx, Announcement{
		ID: "a1", DeviceID: "sat-bad", AreaID: "bad", Channel: "voice", NoticeIDs: []string{"n1", "n2"},
	}); err != nil {
		t.Fatalf("RecordAnnouncement: %v", err)
	}

	clk.Add(90 * time.Second)
	a, err := s.RecentAnnouncement(ctx, "sat-bad", 2*time.Minute)
	if err != nil {
		t.Fatalf("within window: %v", err)
	}
	if len(a.NoticeIDs) != 2 {
		t.Errorf("notice ids = %v", a.NoticeIDs)
	}

	clk.Add(2 * time.Minute)
	if _, err := s.RecentAnnouncement(ctx, "sat-bad", 2*time.Minute); err != ErrNotFound {
		t.Errorf("outside window should be ErrNotFound, got %v", err)
	}
}

func TestConfirmationIsSingleUse(t *testing.T) {
	ctx := context.Background()
	s, clk := newStore(t)

	if err := s.PutConfirmation(ctx, Confirmation{
		ID: "c1", SessionKey: "voice:x", ToolName: "ha__lock", Arguments: `{"entity_id":"lock.front"}`,
	}, 5*time.Minute); err != nil {
		t.Fatalf("PutConfirmation: %v", err)
	}
	got, err := s.TakeConfirmation(ctx, "voice:x")
	if err != nil {
		t.Fatalf("TakeConfirmation: %v", err)
	}
	if got.ToolName != "ha__lock" {
		t.Errorf("tool = %q", got.ToolName)
	}
	if _, err := s.TakeConfirmation(ctx, "voice:x"); err != ErrNotFound {
		t.Errorf("a confirmation must authorise exactly one call, got %v", err)
	}

	// An expired confirmation cannot be used either.
	if err := s.PutConfirmation(ctx, Confirmation{ID: "c2", SessionKey: "voice:x", ToolName: "t"}, time.Minute); err != nil {
		t.Fatalf("PutConfirmation: %v", err)
	}
	clk.Add(2 * time.Minute)
	if _, err := s.TakeConfirmation(ctx, "voice:x"); err != ErrNotFound {
		t.Errorf("expired confirmation was accepted: %v", err)
	}
}

func TestKVFlags(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)

	if v, err := s.GetBool(ctx, KeyQuietMode, false); err != nil || v {
		t.Fatalf("default quiet mode = %v (%v), want false", v, err)
	}
	if err := s.SetBool(ctx, KeyQuietMode, true); err != nil {
		t.Fatalf("SetBool: %v", err)
	}
	if v, _ := s.GetBool(ctx, KeyQuietMode, false); !v {
		t.Error("quiet mode did not persist")
	}
	if err := s.SetInt64(ctx, KeyTelegramOffset, 4711); err != nil {
		t.Fatalf("SetInt64: %v", err)
	}
	if v, _ := s.GetInt64(ctx, KeyTelegramOffset, 0); v != 4711 {
		t.Errorf("offset = %d", v)
	}
}

func TestProfileRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)

	if text, _, err := s.Profile(ctx); err != nil || text != "" {
		t.Fatalf("empty profile = %q (%v)", text, err)
	}
	if err := s.SetProfile(ctx, "Mag Kaffee."); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	text, updated, err := s.Profile(ctx)
	if err != nil || text != "Mag Kaffee." {
		t.Fatalf("profile = %q (%v)", text, err)
	}
	if updated.IsZero() {
		t.Error("updated_at not set")
	}
}
