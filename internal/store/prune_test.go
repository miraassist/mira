package store

import (
	"context"
	"testing"
	"time"
)

// Long finished rows go; what is open, or finished lately, stays.
func TestPruneFinishedKeepsWhatIsOpen(t *testing.T) {
	s, clock := newStore(t)
	ctx := context.Background()
	put := func(id string) {
		if _, _, err := s.PutMessage(ctx, Message{ID: id, Source: SourceExternal, DedupeKey: id, Importance: 1, Spoken: id}); err != nil {
			t.Fatal(err)
		}
	}
	put("old-heard")
	put("old-open")
	if err := s.SetMessageState(ctx, "old-heard", MessageAcked); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAnnouncement(ctx, Announcement{ID: "a1", DeviceID: "d", AreaID: "kueche", Channel: "voice"}); err != nil {
		t.Fatal(err)
	}
	clock.Add(100 * 24 * time.Hour)
	put("new-heard")
	_ = s.SetMessageState(ctx, "new-heard", MessageAcked)
	p, err := s.PruneFinished(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Messages != 1 || p.Announcements != 1 {
		t.Errorf("pruned %+v", p)
	}
	for id, want := range map[string]bool{"old-heard": false, "old-open": true, "new-heard": true} {
		if _, err := s.GetMessage(ctx, id); (err == nil) != want {
			t.Errorf("%s kept %v, want %v", id, err == nil, want)
		}
	}
}
