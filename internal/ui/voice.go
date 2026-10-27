package ui

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/miraassist/mira/internal/speakerid"
)

// Voices is the speaker-id service as the page needs it: who has a voice,
// and enrolling one (docs/spec/sprecher.md). *speakerid.Client is one.
type Voices interface {
	Profiles(ctx context.Context) (map[string]int, error)
	Now(ctx context.Context) (float64, error)
	HeardSince(ctx context.Context, since float64) ([]speakerid.Heard, float64, error)
	Assign(ctx context.Context, id, person string) error
	Delete(ctx context.Context, person string) error
}

// enrollFor is how long enrolling shows what the satellites hear of the
// sentences it asks for: long enough to say them all.
const enrollFor = 3 * time.Minute

// enrollSentences are what enrolling asks to say, after the wake word:
// harmless requests Mira answers in a moment. Only utterances that are one
// of them show on the page, so enrolling cannot be used to read along with
// what the house says (docs/spec/sprecher.md).
var enrollSentences = map[string][]string{
	"de": {
		"Wie spät ist es gerade?",
		"Welcher Wochentag ist heute?",
		"Wie wird das Wetter morgen?",
		"Wie warm ist es draußen?",
		"Wann geht heute die Sonne unter?",
		"Wie viele Tage hat der Februar?",
		"Erzähl mir einen kurzen Witz.",
		"Welches Datum haben wir heute?",
	},
	"en": {
		"What time is it right now?",
		"Which day of the week is it today?",
		"What will the weather be like tomorrow?",
		"How warm is it outside?",
		"When does the sun set today?",
		"How many days does February have?",
		"Tell me a short joke.",
		"What is the date today?",
	},
}

// enrollSay is how many of them one enrolling asks for.
const enrollSay = 4

// enrolling is when each person's enrolling started, on the service's clock,
// when it ends on Mira's, and the sentences it asked for. What the
// satellites heard is shown only from that start, and only those sentences:
// nothing else said in the house is the enroller's to read.
type enrolling struct {
	mu   sync.Mutex
	open map[string]enrollWindow
}

type enrollWindow struct {
	since     float64
	until     time.Time
	sentences []string
}

func (e *enrolling) start(person string, since float64, sentences []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.open == nil {
		e.open = map[string]enrollWindow{}
	}
	e.open[person] = enrollWindow{since: since, until: time.Now().Add(enrollFor), sentences: sentences}
}

// window is the person's enrolling and how long it stays open.
func (e *enrolling) window(person string) (enrollWindow, time.Duration, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	w, ok := e.open[person]
	if !ok || time.Now().After(w.until) {
		delete(e.open, person)
		return enrollWindow{}, 0, false
	}
	return w, time.Until(w.until), true
}

// pickSentences are enrollSay sentences of the language, in random order.
func pickSentences(lang string) []string {
	all, ok := enrollSentences[lang]
	if !ok {
		all = enrollSentences["en"]
	}
	picked := slices.Clone(all)
	rand.Shuffle(len(picked), func(i, j int) { picked[i], picked[j] = picked[j], picked[i] })
	return picked[:enrollSay]
}

// saysOneOf: the transcript is one of the sentences, as speech recognition
// writes it. Most of the sentence's words are there, and hardly anything
// else, so a sentence said inside a longer one does not show the rest.
func saysOneOf(transcript string, sentences []string) bool {
	heard := words(transcript)
	for _, s := range sentences {
		want := words(s)
		found := 0
		for _, w := range want {
			if slices.Contains(heard, w) {
				found++
			}
		}
		if len(want) > 0 && found*10 >= len(want)*7 && len(heard) <= len(want)+2 {
			return true
		}
	}
	return false
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// enrollable is what the person may see and take while enrolling: what was
// heard since the start that is one of the sentences asked for, and not
// someone else's voice, as enrolled or as the service hears it.
func (h *Handler) enrollable(heard []speakerid.Heard, person string, w enrollWindow) []speakerid.Heard {
	out := []speakerid.Heard{}
	for _, u := range heard {
		if !saysOneOf(u.Transcript, w.sentences) {
			continue
		}
		if u.Assigned != "" && u.Assigned != person {
			continue
		}
		if u.Person != "" && u.Person != person && h.opts.VoiceThreshold != nil && u.Score >= h.opts.VoiceThreshold(u.Person) {
			continue
		}
		out = append(out, u)
	}
	return out
}

func (e *enrolling) stop(person string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.open, person)
}

// me is who looks at the page: their person (if Home Assistant links one to
// their login), its name, whether they are an admin, and whether there is
// speaker recognition. Everyone may ask.
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	v := h.viewer(r)
	out := map[string]any{"admin": v.admin, "voice": h.opts.Voices != nil}
	if v.person != "" {
		out["person"], out["name"] = v.person, h.personName(v.person)
	}
	writeJSON(w, http.StatusOK, out)
}

// voiceTarget is the person of /v1/ui/voice/{person}, if the viewer may
// manage that person's voice: their own, or anyone's for an admin. It has
// written the error when not.
func (h *Handler) voiceTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.opts.Voices == nil {
		writeError(w, http.StatusNotFound, "no speaker recognition (speaker_id in mira.yaml)")
		return "", false
	}
	person := r.PathValue("person")
	if !slices.ContainsFunc(h.people(), func(p PersonInfo) bool { return p.Key == person }) {
		writeError(w, http.StatusNotFound, "no such person")
		return "", false
	}
	if v := h.viewer(r); !v.admin && v.person != person {
		writeError(w, http.StatusForbidden, "only your own voice, or as an admin")
		return "", false
	}
	return person, true
}

// voices are the samples each person has; for the admins' list.
func (h *Handler) voices(w http.ResponseWriter, r *http.Request) {
	if h.opts.Voices == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	profiles, err := h.opts.Voices.Profiles(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "samples": profiles})
}

// voice is one person's samples, and whether they are enrolling right now.
func (h *Handler) voice(w http.ResponseWriter, r *http.Request) {
	person, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	profiles, err := h.opts.Voices.Profiles(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	win, left, open := h.enroll.window(person)
	writeJSON(w, http.StatusOK, map[string]any{"samples": profiles[person], "enrolling": open, "left": int(left.Seconds()),
		"sentences": nonNilStrings(win.sentences)})
}

// enrollStart starts enrolling: for enrollFor the page shows what the
// satellites hear of the sentences it asks for (?lang=de|en).
func (h *Handler) enrollStart(w http.ResponseWriter, r *http.Request) {
	person, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	now, err := h.opts.Voices.Now(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sentences := pickSentences(r.URL.Query().Get("lang"))
	h.enroll.start(person, now, sentences)
	writeJSON(w, http.StatusOK, map[string]any{"seconds": int(enrollFor.Seconds()), "sentences": sentences})
}

func (h *Handler) enrollStop(w http.ResponseWriter, r *http.Request) {
	person, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	h.enroll.stop(person)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// heard is what the satellites heard of the sentences since enrolling
// started.
func (h *Handler) heard(w http.ResponseWriter, r *http.Request) {
	person, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	win, left, open := h.enroll.window(person)
	if !open {
		writeError(w, http.StatusConflict, "not enrolling")
		return
	}
	heard, _, err := h.opts.Voices.HeardSince(r.Context(), win.since)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// left: how much longer, so the page follows Mira's window and not a clock of its own.
	writeJSON(w, http.StatusOK, map[string]any{"heard": h.enrollable(heard, person, win), "left": int(left.Seconds())})
}

// assign enrolls one of the utterances heard while enrolling for the person.
func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	person, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &body); err != nil || body.ID == "" {
		writeError(w, http.StatusBadRequest, "id missing")
		return
	}
	win, _, open := h.enroll.window(person)
	if !open {
		writeError(w, http.StatusConflict, "not enrolling")
		return
	}
	// Only what the page may show: no other utterance's id gets in.
	heard, _, err := h.opts.Voices.HeardSince(r.Context(), win.since)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !slices.ContainsFunc(h.enrollable(heard, person, win), func(u speakerid.Heard) bool { return u.ID == body.ID }) {
		writeError(w, http.StatusNotFound, "not heard while enrolling")
		return
	}
	if err := h.opts.Voices.Assign(r.Context(), body.ID, person); err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, speakerid.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// forgetVoice deletes the person's voice.
func (h *Handler) forgetVoice(w http.ResponseWriter, r *http.Request) {
	person, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	if err := h.opts.Voices.Delete(r.Context(), person); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.enroll.stop(person)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
