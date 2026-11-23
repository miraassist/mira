// Package heard learns how far a spoken answer was heard
// (docs/spec/streaming.md, step 10): after a voice turn it watches the
// satellite's speaker, measures how long the answer played, and when it
// played clearly shorter than the answer takes, keeps a note for the
// conversation's next turn. Answers played to the end measure the satellite's
// straight line (a fixed part plus a rate).
package heard

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/miraassist/mira/internal/clock"
)

// The start value until a satellite has measurements (row 12).
const (
	startFixed   = 2.4        // seconds
	startPerChar = 1.0 / 18.6 // seconds per character
)

// The bounds of step 3: at least this share of the line's time counts as
// played to the end; shorter by more than both cut and gap is interrupted.
const (
	completeShare = 0.85
	cutGap        = 1.5 // seconds
)

// Safety nets (Errors): no playing at all, or playing far too long.
const (
	waitPlaying = 30 * time.Second
	extraPlay   = 60 * time.Second
)

// keyPrefix is the kv family of the satellites' lines (docs/database.md).
const keyPrefix = "speech_line."

// KV is the store's key-value table.
type KV interface {
	GetKV(ctx context.Context, key string) (string, error)
	SetKV(ctx context.Context, key, value string) error
}

// Watch subscribes to one entity's state; changed gets its state and since
// when it has it. It must not block.
type Watch func(ctx context.Context, entity string, changed func(state string, since time.Time)) (stop func(), err error)

// Options wire the tracker.
type Options struct {
	// Speaker is the media player of a satellite's device, "" for none.
	Speaker func(device string) string
	Watch   Watch
	KV      KV
	// NotFound is the store's error for a key it does not have.
	NotFound error
	// Clock is the time of a speaker's changes and what the watch's limit
	// waits on; nil is the real one.
	Clock  clock.Clock
	Logger *slog.Logger
}

// Tracker keeps the notes and runs one watch per satellite.
type Tracker struct {
	o      Options
	mu     sync.Mutex
	notes  map[string]string // conversation → last heard sentence ("" for none)
	tracks map[string]*context.CancelFunc
	warned map[string]bool
	// changed is closed and made anew whenever a watch ends, for tests.
	changed chan struct{}
}

// New makes a tracker.
func New(o Options) *Tracker {
	o.Clock = clock.Or(o.Clock)
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Tracker{o: o, notes: map[string]string{}, tracks: map[string]*context.CancelFunc{}, warned: map[string]bool{},
		changed: make(chan struct{})}
}

// Take gives the note of a conversation once: the last sentence the user
// heard of its last answer ("" for not one whole sentence), and whether
// that answer was interrupted.
func (t *Tracker) Take(key string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	q, ok := t.notes[key]
	delete(t.notes, key)
	return q, ok
}

// Spoke starts watching the satellite's speaker for an answer a voice turn
// has spoken there; a newer answer at the same satellite ends the older
// watch. ctx is not kept: the watch runs on its own.
func (t *Tracker) Spoke(key, device, text string, began time.Time) {
	if device == "" || strings.TrimSpace(text) == "" {
		return
	}
	speaker := t.o.Speaker(device)
	t.mu.Lock()
	if speaker == "" {
		if !t.warned[device] {
			t.warned[device] = true
			t.o.Logger.Info("no note of an interrupted answer at this satellite: its speaker is not known", "device", device)
		}
		t.mu.Unlock()
		return
	}
	if older := t.tracks[device]; older != nil {
		(*older)()
	}
	ctx, cancel := context.WithCancel(context.Background())
	ref := &cancel
	t.tracks[device] = ref
	t.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			t.mu.Lock()
			if t.tracks[device] == ref {
				delete(t.tracks, device)
			}
			close(t.changed)
			t.changed = make(chan struct{})
			t.mu.Unlock()
		}()
		t.track(ctx, key, device, speaker, text, began)
	}()
}

type change struct {
	state string
	since time.Time
}

// track waits for the answer to play and to stop (step 1), then judges it.
func (t *Tracker) track(ctx context.Context, key, device, speaker, text string, began time.Time) {
	changes := make(chan change, 16)
	stop, err := t.o.Watch(ctx, speaker, func(state string, since time.Time) {
		select {
		case changes <- change{state, since}:
		default:
		}
	})
	if err != nil {
		t.o.Logger.Warn("could not watch the satellite's speaker; no note of an interrupted answer", "speaker", speaker, "error", err)
		return
	}
	defer stop()
	spoken := spokenText(text)
	chars := utf8.RuneCountInString(spoken)
	fixed, perChar := t.line(ctx, device)
	var playing time.Time
	limit := t.o.Clock.NewTimer(waitPlaying)
	defer limit.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-limit.C():
			if playing.IsZero() {
				t.o.Logger.Info("no note of an interrupted answer: the speaker did not play it", "speaker", speaker,
					"session", key, "waited_s", waitPlaying.Seconds())
			} else {
				t.o.Logger.Info("no note of an interrupted answer: the speaker plays far longer than the answer", "speaker", speaker,
					"session", key, "playing_s", t.o.Clock.Since(playing).Seconds())
			}
			return
		case c := <-changes:
			if c.state == "playing" {
				// Playing since before the turn began is something else
				// (music): not this answer.
				if playing.IsZero() && !c.since.Before(began) {
					playing = c.since
					limit.Reset(time.Duration((2*(fixed+float64(chars)*perChar))*float64(time.Second)) + extraPlay)
				}
				continue
			}
			if playing.IsZero() {
				continue
			}
			t.judge(ctx, key, device, speaker, spoken, chars, c.since.Sub(playing).Seconds(), fixed, perChar)
			return
		}
	}
}

// judge is steps 2 and 3: played to the end measures, cut short notes.
func (t *Tracker) judge(ctx context.Context, key, device, speaker, spoken string, chars int, played, fixed, perChar float64) {
	expected := fixed + float64(chars)*perChar
	switch {
	case played >= completeShare*expected:
		t.measure(ctx, device, chars, played)
		t.o.Logger.Info("answer played to the end", "speaker", speaker, "chars", chars, "played_s", round(played), "expected_s", round(expected))
	case expected-played > cutGap:
		heard := int((played - fixed) / perChar)
		quote := lastSentence(spoken, heard)
		t.mu.Lock()
		t.notes[key] = quote
		t.mu.Unlock()
		t.o.Logger.Info("answer interrupted while spoken", "session", key, "speaker", speaker, "chars", chars,
			"played_s", round(played), "expected_s", round(expected), "heard_chars", max(heard, 0), "heard_up_to", cut(quote, 60))
	default:
		t.o.Logger.Info("answer neither played to the end nor clearly cut: no note, no measurement", "speaker", speaker,
			"chars", chars, "played_s", round(played), "expected_s", round(expected))
	}
}

// sums are a satellite's straight-line fit over (characters, seconds).
type sums struct {
	N   float64 `json:"n"`
	SX  float64 `json:"sx"`
	SY  float64 `json:"sy"`
	SXX float64 `json:"sxx"`
	SXY float64 `json:"sxy"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// line is the satellite's fixed part (s) and seconds per character, or
// the start value (step 5).
func (t *Tracker) line(ctx context.Context, device string) (float64, float64) {
	s, ok := t.sums(ctx, device)
	if !ok || s.N < 3 || s.Max-s.Min < 100 {
		return startFixed, startPerChar
	}
	den := s.N*s.SXX - s.SX*s.SX
	if den <= 0 {
		return startFixed, startPerChar
	}
	b := (s.N*s.SXY - s.SX*s.SY) / den
	a := (s.SY - b*s.SX) / s.N
	if a < 0 || b <= 0 || 1/b < 5 || 1/b > 40 {
		t.o.Logger.Info("the measured line of the satellite's speaker is not plausible; the start value applies", "device", device,
			"fixed_s", round(a), "chars_per_s", round(safeInv(b)))
		return startFixed, startPerChar
	}
	return a, b
}

func (t *Tracker) sums(ctx context.Context, device string) (sums, bool) {
	key := keyPrefix + device
	raw, err := t.o.KV.GetKV(ctx, key)
	if err != nil {
		if !errors.Is(err, t.o.NotFound) {
			t.o.Logger.Warn("could not read the satellite's speech line; the start value applies", "key", key, "error", err)
		}
		return sums{}, false
	}
	var s sums
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.o.Logger.Warn("the satellite's speech line does not parse; the start value applies", "key", key, "value", cut(raw, 60), "error", err)
		return sums{}, false
	}
	return s, true
}

// measure adds an answer played to the end; past 50 pairs the older ones
// weigh less.
func (t *Tracker) measure(ctx context.Context, device string, chars int, played float64) {
	s, ok := t.sums(ctx, device)
	x := float64(chars)
	if !ok || s.N == 0 {
		s = sums{Min: x, Max: x}
	}
	if s.N >= 50 {
		f := 49 / s.N
		s.N, s.SX, s.SY, s.SXX, s.SXY = s.N*f, s.SX*f, s.SY*f, s.SXX*f, s.SXY*f
	}
	s.N++
	s.SX += x
	s.SY += played
	s.SXX += x * x
	s.SXY += x * played
	s.Min, s.Max = min(s.Min, x), max(s.Max, x)
	raw, _ := json.Marshal(s)
	key := keyPrefix + device
	if err := t.o.KV.SetKV(ctx, key, string(raw)); err != nil {
		t.o.Logger.Warn("could not keep the satellite's speech line; this answer's measurement is lost", "key", key, "error", err)
	}
}

// reTag is a style tag in brackets ("[ruhig]"): not spoken.
var reTag = regexp.MustCompile(`\[[^\]]*\]`)

// spokenText is what the satellite says of an answer.
func spokenText(s string) string {
	return strings.Join(strings.Fields(reTag.ReplaceAllString(s, " ")), " ")
}

// lastSentence is the last sentence that ends within the first n characters
// of s; "" when none does.
func lastSentence(s string, n int) string {
	r := []rune(s)
	if n > len(r) {
		n = len(r)
	}
	end, start := -1, 0
	for i := 0; i < n; i++ {
		if !strings.ContainsRune(".!?…", r[i]) || (i+1 < len(r) && r[i+1] != ' ') {
			continue
		}
		if end >= 0 {
			start = end + 1
		}
		end = i
	}
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(string(r[start : end+1]))
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func round(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

func safeInv(f float64) float64 {
	if f == 0 {
		return 0
	}
	return 1 / f
}
