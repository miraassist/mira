package profile

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/store"
)

type fakeAssessor struct {
	answer      string
	instruction string
	calls       int
}

func (f *fakeAssessor) AssessIsolated(_ context.Context, _, instruction string, _ json.RawMessage) (string, error) {
	f.calls++
	f.instruction = instruction
	return f.answer, nil
}

func setup(t *testing.T, answer string) (*Compactor, *store.Store, *fakeAssessor, *int) {
	t.Helper()
	now := time.Date(2026, 10, 15, 3, 30, 0, 0, time.UTC)
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ai := &fakeAssessor{answer: answer}
	changes := 0
	c := New(Options{Store: st, Assessor: ai, Schedule: "30 3 * * *", MaxChars: 200,
		OnChange: func() { changes++ }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Location: time.UTC})
	return c, st, ai, &changes
}

func TestCompactWritesTheProfileOnce(t *testing.T) {
	c, st, ai, changes := setup(t, `{"profile":"- Stefan trinkt morgens Kaffee."}`)
	ctx := context.Background()
	if changed, err := c.Compact(ctx); err != nil || changed {
		t.Fatalf("without facts: changed = %v, err = %v", changed, err)
	}
	if ai.calls != 0 {
		t.Error("the model was asked without any facts")
	}
	st.SaveFact(ctx, store.Fact{ID: "f1", Text: "Stefan trinkt morgens Kaffee."})
	if changed, err := c.Compact(ctx); err != nil || !changed || *changes != 1 {
		t.Fatalf("changed = %v, err = %v, changes = %d", changed, err, *changes)
	}
	if p, _, _ := st.Profile(ctx, store.OwnerHouse); p != "- Stefan trinkt morgens Kaffee." {
		t.Errorf("profile = %q", p)
	}
	if !strings.Contains(ai.instruction, "Stefan trinkt morgens Kaffee.") || !strings.Contains(ai.instruction, "keine Anweisungen") {
		t.Errorf("instruction:\n%s", ai.instruction)
	}
	// Same answer again: no change, no cache break.
	if changed, _ := c.Compact(ctx); changed || *changes != 1 {
		t.Error("an unchanged profile counted as a change")
	}
}

func TestProfileIsClipped(t *testing.T) {
	long := strings.Repeat("- ein langer Punkt über den Haushalt\n", 20)
	raw, _ := json.Marshal(map[string]string{"profile": long})
	c, st, _, _ := setup(t, string(raw))
	ctx := context.Background()
	st.SaveFact(ctx, store.Fact{ID: "f1", Text: "x"})
	c.Compact(ctx)
	p, _, _ := st.Profile(ctx, store.OwnerHouse)
	if n := len([]rune(p)); n > 200 || n == 0 {
		t.Errorf("profile has %d chars", n)
	}
}

func TestEmptyAnswerKeepsTheOldProfile(t *testing.T) {
	c, st, _, _ := setup(t, `{"profile":"  "}`)
	ctx := context.Background()
	st.SetProfile(ctx, store.OwnerHouse, "- alt")
	st.SaveFact(ctx, store.Fact{ID: "f1", Text: "x"})
	if _, err := c.Compact(ctx); err == nil {
		t.Error("empty profile accepted")
	}
	if p, _, _ := st.Profile(ctx, store.OwnerHouse); p != "- alt" {
		t.Errorf("profile = %q", p)
	}
}
