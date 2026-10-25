package ui

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

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

// enrollFor is how long enrolling shows what the satellites hear: long
// enough for a few sentences, short enough that nobody reads along with the
// house for long.
const enrollFor = 3 * time.Minute

// enrolling is when each person's enrolling started, on the service's clock,
// and when it ends on Mira's. What the satellites heard is shown only from
// that start: what was said before is not the enroller's to read.
type enrolling struct {
	mu   sync.Mutex
	open map[string]enrollWindow
}

type enrollWindow struct {
	since float64
	until time.Time
}

func (e *enrolling) start(person string, since float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.open == nil {
		e.open = map[string]enrollWindow{}
	}
	e.open[person] = enrollWindow{since: since, until: time.Now().Add(enrollFor)}
}

// since is where the person's enrolling began, if it is still open.
func (e *enrolling) since(person string) (float64, bool) {
	since, _, ok := e.window(person)
	return since, ok
}

// window is where the person's enrolling began and how long it stays open.
func (e *enrolling) window(person string) (float64, time.Duration, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	w, ok := e.open[person]
	if !ok || time.Now().After(w.until) {
		delete(e.open, person)
		return 0, 0, false
	}
	return w.since, time.Until(w.until), true
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
	_, left, open := h.enroll.window(person)
	writeJSON(w, http.StatusOK, map[string]any{"samples": profiles[person], "enrolling": open, "left": int(left.Seconds())})
}

// enrollStart starts enrolling: from now on the page shows what the
// satellites hear, for enrollFor.
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
	h.enroll.start(person, now)
	writeJSON(w, http.StatusOK, map[string]any{"seconds": int(enrollFor.Seconds())})
}

func (h *Handler) enrollStop(w http.ResponseWriter, r *http.Request) {
	person, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	h.enroll.stop(person)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// heard is what the satellites heard since enrolling started.
func (h *Handler) heard(w http.ResponseWriter, r *http.Request) {
	person, ok := h.voiceTarget(w, r)
	if !ok {
		return
	}
	since, left, open := h.enroll.window(person)
	if !open {
		writeError(w, http.StatusConflict, "not enrolling")
		return
	}
	heard, _, err := h.opts.Voices.HeardSince(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if heard == nil {
		heard = []speakerid.Heard{}
	}
	// left: how much longer, so the page follows Mira's window and not a clock of its own.
	writeJSON(w, http.StatusOK, map[string]any{"heard": heard, "left": int(left.Seconds())})
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
	since, open := h.enroll.since(person)
	if !open {
		writeError(w, http.StatusConflict, "not enrolling")
		return
	}
	// Only what was heard while enrolling: no other utterance's id gets in.
	heard, _, err := h.opts.Voices.HeardSince(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !slices.ContainsFunc(heard, func(u speakerid.Heard) bool { return u.ID == body.ID }) {
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
