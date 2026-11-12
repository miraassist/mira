package store

import (
	"context"
	"testing"
)

// The note of Mira's own automations (docs/spec/hacs-default.md, decision 7).
func TestOwnAutomations(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if mine, err := s.IsOwnAutomation(ctx, "a"); err != nil || mine {
		t.Fatalf("empty: %v %v", mine, err)
	}
	if err := s.RecordOwnAutomation(ctx, "a", "task"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordOwnAutomation(ctx, "a", "once"); err != nil {
		t.Fatalf("recorded again: %v", err)
	}
	if mine, _ := s.IsOwnAutomation(ctx, "a"); !mine {
		t.Error("not noted")
	}
	if err := s.ForgetOwnAutomation(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if mine, _ := s.IsOwnAutomation(ctx, "a"); mine {
		t.Error("still noted")
	}

	if done, err := s.OwnAutomationsTakenOver(ctx); err != nil || done {
		t.Fatalf("taken over before: %v %v", done, err)
	}
	if err := s.TakeOverOwnAutomations(ctx, map[string]string{"b": "task", "c": "once"}); err != nil {
		t.Fatal(err)
	}
	if done, _ := s.OwnAutomationsTakenOver(ctx); !done {
		t.Error("not marked as taken over")
	}
	for _, id := range []string{"b", "c"} {
		if mine, _ := s.IsOwnAutomation(ctx, id); !mine {
			t.Errorf("%s not taken over", id)
		}
	}
}
