package heard

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// docs/spec/streaming.md, step 10.

type memKV struct {
	mu sync.Mutex
	m  map[string]string
}

var errMissing = errors.New("not found")

func (k *memKV) GetKV(_ context.Context, key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	if !ok {
		return "", errMissing
	}
	return v, nil
}

func (k *memKV) SetKV(_ context.Context, key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = value
	return nil
}

// speakerFake is a speaker a test switches; the tracker watches it.
type speakerFake struct {
	mu      sync.Mutex
	changed func(string, time.Time)
	watched chan struct{}
}

func (s *speakerFake) watch(_ context.Context, _ string, changed func(string, time.Time)) (func(), error) {
	s.mu.Lock()
	s.changed = changed
	w := s.watched
	s.mu.Unlock()
	close(w)
	return func() {}, nil
}

func (s *speakerFake) expect() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watched = make(chan struct{})
	return s.watched
}

func (s *speakerFake) set(state string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changed(state, at)
}

func setup(t *testing.T, speaker string) (*Tracker, *memKV, *speakerFake, *bytes.Buffer) {
	t.Helper()
	kv := &memKV{m: map[string]string{}}
	sp := &speakerFake{}
	var log bytes.Buffer
	tr := New(Options{Speaker: func(string) string { return speaker }, Watch: sp.watch, KV: kv, NotFound: errMissing,
		Logger: slog.New(slog.NewTextHandler(&log, nil))})
	return tr, kv, sp, &log
}

// play runs one answer through the tracker: it plays from t0 for played
// seconds, then the speaker stops.
func play(t *testing.T, tr *Tracker, sp *speakerFake, key, text string, played float64) {
	t.Helper()
	watched := sp.expect()
	t0 := time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC)
	tr.Spoke(key, "show", text, t0.Add(-5*time.Second))
	<-watched
	sp.set("playing", t0)
	sp.set("idle", t0.Add(time.Duration(played*float64(time.Second))))
	waitFor(t, func() bool { return idle(tr) })
}

func idle(tr *Tracker) bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return len(tr.tracks) == 0
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 100 && !ok(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
}

// story is 956 characters of sentences, as the Show's story of row 11.
func story() string {
	var b strings.Builder
	for i := 0; b.Len() < 940; i++ {
		b.WriteString("Der kleine Igel lief über die Wiese und fand einen glänzenden Stein. ")
	}
	return strings.TrimSpace(b.String())
}

func TestST19_AnAnswerInterruptedWhileSpokenIsNotedWhereItWasCut(t *testing.T) {
	tr, _, sp, log := setup(t, "media_player.show_speaker")
	play(t, tr, sp, "voice:a", story(), 6.84)
	quote, ok := tr.Take("voice:a")
	if !ok {
		t.Fatalf("no note: %s", log.String())
	}
	// (6.84 - 2.4) s × 18.6 = 82 characters: the first sentence (69).
	if quote != "Der kleine Igel lief über die Wiese und fand einen glänzenden Stein." {
		t.Errorf("heard up to %q", quote)
	}
	if _, again := tr.Take("voice:a"); again {
		t.Error("the note was given twice")
	}
}

func TestST20_AnAnswerPlayedToTheEndHasNoNoteAndMeasuresTheRate(t *testing.T) {
	tr, kv, sp, _ := setup(t, "media_player.show_speaker")
	text := strings.Repeat("Ein Satz von etwa dreißig Zeichen. ", 9) // 300 characters
	play(t, tr, sp, "voice:b", text, 2.4+300/18.6)
	if _, ok := tr.Take("voice:b"); ok {
		t.Error("a note for an answer played to the end")
	}
	if raw := kv.m[keyPrefix+"show"]; !strings.Contains(raw, `"n":1`) {
		t.Errorf("not measured: %q", raw)
	}
	// Three answers far apart make the line: a slower satellite.
	play(t, tr, sp, "voice:b", strings.Repeat("x", 30), 1+30/10.0)
	play(t, tr, sp, "voice:b", strings.Repeat("x", 600), 1+600/10.0)
	play(t, tr, sp, "voice:b", strings.Repeat("x", 200), 1+200/10.0)
	fixed, perChar := tr.line(context.Background(), "show")
	if fixed < 0 || perChar < 1/40.0 || perChar > 1/5.0 {
		t.Errorf("line %v + %v", fixed, perChar)
	}
}

func TestST21_WithoutAMeasuredLineTheStartValueApplies(t *testing.T) {
	tr, _, _, _ := setup(t, "media_player.show_speaker")
	if fixed, perChar := tr.line(context.Background(), "show"); fixed != startFixed || perChar != startPerChar {
		t.Errorf("line %v + %v", fixed, perChar)
	}
}

func TestST22_ASatelliteWhoseSpeakerIsNotKnownGetsNoNote(t *testing.T) {
	tr, _, _, log := setup(t, "")
	tr.Spoke("voice:c", "echo", story(), time.Now())
	tr.Spoke("voice:c", "echo", story(), time.Now())
	if _, ok := tr.Take("voice:c"); ok {
		t.Error("a note without a speaker")
	}
	if n := strings.Count(log.String(), "its speaker is not known"); n != 1 || !strings.Contains(log.String(), "device=echo") {
		t.Errorf("log lines: %d\n%s", n, log.String())
	}
}

// Music that played before the turn is not the answer; nothing is noted.
func TestST19_PlayingSinceBeforeTheTurnIsNotTheAnswer(t *testing.T) {
	tr, _, sp, _ := setup(t, "media_player.show_speaker")
	began := time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC)
	watched := sp.expect()
	tr.Spoke("voice:d", "show", story(), began)
	<-watched
	sp.set("playing", began.Add(-time.Hour))
	sp.set("idle", began.Add(5*time.Second))
	time.Sleep(30 * time.Millisecond)
	if _, ok := tr.Take("voice:d"); ok {
		t.Error("a note from music")
	}
}

func TestLastSentence(t *testing.T) {
	for _, c := range []struct {
		s    string
		n    int
		want string
	}{
		{"Eins. Zwei! Drei?", 100, "Drei?"},
		{"Eins. Zwei! Drei?", 11, "Zwei!"},
		{"Eins. Zwei! Drei?", 4, ""},
		{"Um 3.5 Uhr. Dann", 100, "Um 3.5 Uhr."},
	} {
		if got := lastSentence(c.s, c.n); got != c.want {
			t.Errorf("lastSentence(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
	if got := spokenText("[ruhig] Es war  einmal."); got != "Es war einmal." {
		t.Errorf("spokenText = %q", got)
	}
}
