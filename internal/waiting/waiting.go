// Package waiting shows at a thread's satellite that Mira waits for
// something outside (docs/spec/waiting-indicator.md): on a Show a silent
// video of a ring that runs down, on an Echo a silent timer on its LED ring.
// Both last 60 s and start again on their end while the thread waits; both
// stop when the wait ends.
package waiting

import (
	"context"
	"embed"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
)

// VideoPath is where Home Assistant serves the videos to satellites: the
// integration's open page path, which passes them through from Mira's
// /ui/videos/ (as the sounds, docs/spec/satellite.md).
const VideoPath = "/api/mira/panel/ui/videos/"

// Colour is the Echo's ring while Mira waits (decision 2): turquoise.
const Colour = "#40E0D0"

//go:embed videos/*.mp4
var files embed.FS

// Video is the waiting video of a language ("wait-de"); false for another
// name.
func Video(name string) ([]byte, bool) {
	if name != "wait-de" && name != "wait-en" {
		return nil, false
	}
	data, err := files.ReadFile("videos/" + name + ".mp4")
	return data, err == nil
}

// HA is the part of Home Assistant's client the indicator uses.
type HA interface {
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
	CallServiceResponse(ctx context.Context, domain, service string, data map[string]any) (json.RawMessage, error)
	GetStates(ctx context.Context, f haws.StateFilter) ([]haws.State, error)
	WatchState(ctx context.Context, entity string, fn func(state string)) (func(), error)
}

type Options struct {
	Lang   i18n.Lang
	Logger *slog.Logger
	HA     HA
	// Device is the satellite's device of a thread ("" if none).
	Device func(ctx context.Context, thread string) string
	// Node is a device's ESPHome node name ("" if none).
	Node func(deviceID string) string
	// Satellite is a device's assist satellite entity ("" if none): the
	// wait shows once it is idle after the turn's answer.
	Satellite func(deviceID string) string
	// Idle is how long the wait waits for the satellite to be idle, a
	// safety net (30 s).
	Idle time.Duration
	// HasAction says whether Home Assistant has an action.
	HasAction func(ctx context.Context, domain, service string) bool
	// BaseURL is Home Assistant's address as satellites reach it.
	BaseURL func(ctx context.Context) string
}

var labels = map[i18n.Lang]string{i18n.DE: "Mira wartet", i18n.EN: "Mira is waiting"}

// Waiting starts and stops the indicator per thread.
type Waiting struct {
	o   Options
	log *slog.Logger

	mu   sync.Mutex
	runs map[string]*run
}

type kind int

const (
	video kind = iota + 1
	timer
)

type run struct {
	thread, node string
	kind         kind
	mu           sync.Mutex
	stopped      bool
	active       bool   // video: loading, playing or paused since the last start
	played       bool   // video: seen playing since the last start
	timerID      string // timer: the running timer's id
	seen         bool   // timer: seen in the satellite's timers since it started
	unwatch      []func()
	done         chan struct{} // closed when the run is stopped or ended
}

func New(o Options) *Waiting {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Idle == 0 {
		o.Idle = 30 * time.Second
	}
	return &Waiting{o: o, log: o.Logger.With("component", "waiting"), runs: map[string]*run{}}
}

func (w *Waiting) label() string {
	if l, ok := labels[w.o.Lang]; ok {
		return l
	}
	return labels[i18n.DE]
}

func (w *Waiting) lang() string {
	if w.o.Lang == i18n.EN {
		return "en"
	}
	return "de"
}

// Start shows the wait at the thread's satellite, if it can show it; a
// thread already waiting goes on as it is.
func (w *Waiting) Start(ctx context.Context, thread string) {
	if w == nil {
		return
	}
	// Called from a turn too: what it starts outlives the turn.
	ctx = context.WithoutCancel(ctx)
	w.mu.Lock()
	if _, ok := w.runs[thread]; ok {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	device := w.o.Device(ctx, thread)
	node := ""
	if device != "" {
		node = w.o.Node(device)
	}
	if node == "" {
		return
	}
	r := &run{thread: thread, node: node, done: make(chan struct{})}
	switch {
	case w.o.HasAction(ctx, "esphome", node+"_play_video"):
		r.kind = video
	case w.o.HasAction(ctx, "esphome", node+"_timer_start_silent"):
		r.kind = timer
	default:
		return // nothing to show it on (WA-04)
	}
	w.mu.Lock()
	if _, ok := w.runs[thread]; ok {
		w.mu.Unlock()
		return
	}
	w.runs[thread] = r
	w.mu.Unlock()
	sat := ""
	if w.o.Satellite != nil {
		sat = w.o.Satellite(device)
	}
	// Not while the satellite still answers the turn that opened the wait:
	// its ring and screen show the answer then, and the wait would have run
	// part of its minute unseen (seen 2026-11-10 at the Dot 2). The signal
	// is the satellite idle again.
	go func() {
		if sat != "" {
			w.untilIdle(ctx, r, sat)
		}
		if r.isStopped() {
			return
		}
		if r.kind == video {
			w.startVideo(ctx, r)
		} else {
			w.startTimer(ctx, r)
		}
	}()
}

// untilIdle waits until the satellite is idle, the run stops, or the
// safety net is over.
func (w *Waiting) untilIdle(ctx context.Context, r *run, sat string) {
	idle := make(chan struct{})
	var once sync.Once
	unwatch, err := w.o.HA.WatchState(ctx, sat, func(state string) {
		if state == "idle" {
			once.Do(func() { close(idle) })
		}
	})
	if err != nil {
		w.log.Warn("could not watch the satellite; the wait shows at once", "satellite", sat, "error", err)
		return
	}
	defer unwatch()
	t := time.NewTimer(w.o.Idle)
	defer t.Stop()
	select {
	case <-idle:
	case <-r.done:
	case <-t.C:
		w.log.Warn("the satellite did not get idle; the wait shows anyway", "satellite", sat, "after", w.o.Idle)
	}
}

// Stop ends the wait of a thread; nothing when it does not wait.
func (w *Waiting) Stop(ctx context.Context, thread string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	r := w.runs[thread]
	delete(w.runs, thread)
	w.mu.Unlock()
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.stopped {
		close(r.done)
	}
	r.stopped = true
	unwatch, playing, id := r.unwatch, r.active, r.timerID
	r.unwatch = nil
	r.mu.Unlock()
	for _, u := range unwatch {
		u()
	}
	switch {
	case r.kind == video && playing:
		if err := w.o.HA.CallService(ctx, "esphome", r.node+"_stop_video", nil, nil); err != nil {
			w.log.Warn("could not stop the wait video; it ends by itself", "satellite", r.node, "error", err)
		}
	case r.kind == timer && id != "":
		if err := w.o.HA.CallService(ctx, "esphome", r.node+"_timer_cancel", map[string]any{"id": id}, nil); err != nil {
			w.log.Warn("could not stop the wait timer; it ends by itself", "satellite", r.node, "error", err)
		}
	}
}

// end forgets a run that cannot go on (refused, failed), without stopping
// anything at the satellite.
func (w *Waiting) end(r *run) {
	w.mu.Lock()
	if w.runs[r.thread] == r {
		delete(w.runs, r.thread)
	}
	w.mu.Unlock()
	r.mu.Lock()
	if !r.stopped {
		close(r.done)
	}
	r.stopped = true
	unwatch := r.unwatch
	r.unwatch = nil
	r.mu.Unlock()
	for _, u := range unwatch {
		u()
	}
}

// keep holds subscriptions until the run stops; a run stopped meanwhile
// ends them at once.
func (r *run) keep(unwatch ...func()) {
	r.mu.Lock()
	stopped := r.stopped
	if !stopped {
		r.unwatch = append(r.unwatch, unwatch...)
	}
	r.mu.Unlock()
	if stopped {
		for _, u := range unwatch {
			u()
		}
	}
}

func (r *run) isStopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

// startVideo: the Show's switch "Video" on (decision 5), the video played,
// and its state watched to play it again on its end (WA-01, WA-05).
func (w *Waiting) startVideo(ctx context.Context, r *run) {
	sw := "switch." + r.node + "_video"
	states, err := w.o.HA.GetStates(ctx, haws.StateFilter{EntityIDs: []string{sw}})
	if err != nil {
		w.log.Warn("could not read the satellite's video switch; playing anyway", "satellite", r.node, "error", err)
	}
	for _, s := range states {
		if s.State == "off" {
			if err := w.o.HA.CallService(ctx, "switch", "turn_on", nil, &haws.Target{EntityID: []string{sw}}); err != nil {
				w.log.Warn("could not turn the satellite's video on; nothing is shown", "satellite", r.node, "error", err)
				w.end(r)
				return
			}
			w.log.Info("turned the satellite's video on", "satellite", r.node, "switch", sw)
		}
	}
	stateEntity := "sensor." + r.node + "_video_state"
	unwatch, err := w.o.HA.WatchState(ctx, stateEntity, func(state string) {
		// On the connection's read loop: only pass the change on.
		r.mu.Lock()
		if r.stopped {
			r.mu.Unlock()
			return
		}
		replay, failed := false, false
		switch state {
		case "loading":
			r.active = true
		case "playing", "paused":
			r.active, r.played = true, true
		case "idle":
			// Over after it played: again (decision 4). Over without
			// having played (it could not be loaded): not again.
			replay, failed = r.active && r.played, r.active && !r.played
			r.active, r.played = false, false
		}
		r.mu.Unlock()
		if replay {
			go w.play(context.WithoutCancel(ctx), r)
		}
		if failed {
			go func() {
				w.log.Warn("waiting video refused", "satellite", r.node, "thread", r.thread, "error", "it ended without playing")
				w.end(r)
			}()
		}
	})
	if err != nil {
		w.log.Warn("could not watch the satellite's video; nothing is shown", "satellite", r.node, "error", err)
		w.end(r)
		return
	}
	// The first call is the error as it is now, maybe of an earlier video:
	// only a new one counts.
	first := true
	unerr, err := w.o.HA.WatchState(ctx, "sensor."+r.node+"_video_error", func(state string) {
		r.mu.Lock()
		was := first
		first = false
		r.mu.Unlock()
		if was || strings.TrimSpace(state) == "" || r.isStopped() {
			return
		}
		go func() {
			w.log.Warn("waiting video refused", "satellite", r.node, "thread", r.thread, "error", state)
			w.end(r)
		}()
	})
	if err != nil {
		unerr = func() {}
		w.log.Warn("could not watch the satellite's video errors", "satellite", r.node, "error", err)
	}
	r.keep(unwatch, unerr)
	w.play(ctx, r)
}

func (w *Waiting) play(ctx context.Context, r *run) {
	if r.isStopped() {
		return
	}
	url := strings.TrimRight(w.o.BaseURL(ctx), "/") + VideoPath + "wait-" + w.lang() + ".mp4"
	if err := w.o.HA.CallService(ctx, "esphome", r.node+"_play_video", map[string]any{"url": url, "title": w.label()}, nil); err != nil {
		w.log.Warn("could not show the wait", "satellite", r.node, "thread", r.thread, "error", err)
		w.end(r)
	}
}

// startTimer: a silent timer of 60 s in turquoise, called with a response
// for its id (row 7), started again when it is gone while the thread waits.
func (w *Waiting) startTimer(ctx context.Context, r *run) {
	unwatch, err := w.o.HA.WatchState(ctx, "sensor."+r.node+"_timers", func(state string) {
		r.mu.Lock()
		has := strings.Contains(state, w.label())
		if has {
			r.seen = true
		}
		// Gone after it was there: ran out, again while the thread waits.
		gone := !r.stopped && r.seen && !has
		if gone {
			r.timerID, r.seen = "", false
		}
		r.mu.Unlock()
		if gone {
			go w.timer(context.WithoutCancel(ctx), r)
		}
	})
	if err != nil {
		w.log.Warn("could not watch the satellite's timers; nothing is shown", "satellite", r.node, "error", err)
		w.end(r)
		return
	}
	r.keep(unwatch)
	w.timer(ctx, r)
}

func (w *Waiting) timer(ctx context.Context, r *run) {
	if r.isStopped() {
		return
	}
	raw, err := w.o.HA.CallServiceResponse(ctx, "esphome", r.node+"_timer_start_silent",
		map[string]any{"duration": "60s", "label": w.label(), "color": Colour})
	if err != nil {
		w.log.Warn("could not show the wait", "satellite", r.node, "thread", r.thread, "error", err)
		w.end(r)
		return
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.ID == "" {
		w.log.Warn("the wait timer gave no id; it cannot be stopped before its end", "satellite", r.node, "answer", string(raw))
	}
	r.mu.Lock()
	stopped := r.stopped
	r.timerID = resp.ID
	r.mu.Unlock()
	if stopped && resp.ID != "" {
		// Stopped while it was being started.
		if err := w.o.HA.CallService(ctx, "esphome", r.node+"_timer_cancel", map[string]any{"id": resp.ID}, nil); err != nil {
			w.log.Warn("could not stop the wait timer; it ends by itself", "satellite", r.node, "error", err)
		}
	}
}
