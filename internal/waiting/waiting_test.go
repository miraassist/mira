package waiting

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
)

// fakeHA records the actions called and lets a test push states to the
// watched entities.
type fakeHA struct {
	mu       sync.Mutex
	calls    []string // "domain.service data"
	states   map[string]string
	watchers map[string][]func(string)
	nextID   int
}

func newFakeHA() *fakeHA {
	return &fakeHA{states: map[string]string{}, watchers: map[string][]func(string){}}
}

func (f *fakeHA) record(domain, service string, data map[string]any, target *haws.Target) {
	raw, _ := json.Marshal(data)
	call := domain + "." + service + " " + string(raw)
	if target != nil {
		call += " " + strings.Join(target.EntityID, ",")
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakeHA) CallService(_ context.Context, domain, service string, data map[string]any, target *haws.Target) error {
	f.record(domain, service, data, target)
	return nil
}

func (f *fakeHA) CallServiceResponse(_ context.Context, domain, service string, data map[string]any) (json.RawMessage, error) {
	f.record(domain, service, data, nil)
	f.mu.Lock()
	f.nextID++
	id := f.nextID
	f.mu.Unlock()
	return json.Marshal(map[string]string{"id": "local:t" + string(rune('0'+id))})
}

func (f *fakeHA) GetStates(_ context.Context, filter haws.StateFilter) ([]haws.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []haws.State
	for _, id := range filter.EntityIDs {
		if s, ok := f.states[id]; ok {
			out = append(out, haws.State{EntityID: id, State: s})
		}
	}
	return out, nil
}

func (f *fakeHA) WatchState(_ context.Context, entity string, fn func(string)) (func(), error) {
	f.mu.Lock()
	f.watchers[entity] = append(f.watchers[entity], fn)
	now := f.states[entity]
	f.mu.Unlock()
	fn(now)
	return func() {
		f.mu.Lock()
		delete(f.watchers, entity)
		f.mu.Unlock()
	}, nil
}

func (f *fakeHA) push(entity, state string) {
	f.mu.Lock()
	f.states[entity] = state
	ws := append([]func(string){}, f.watchers[entity]...)
	f.mu.Unlock()
	for _, fn := range ws {
		fn(state)
	}
}

func (f *fakeHA) callsOf(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// waitFor waits until cond holds (the replays run in goroutines).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// lockedBuf is a log the replays may write to while the test reads it.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newWaiting(ha *fakeHA, actions ...string) (*Waiting, *lockedBuf) {
	logs := &lockedBuf{}
	w := New(Options{
		Lang: i18n.DE, Logger: slog.New(slog.NewTextHandler(logs, nil)), HA: ha,
		Device: func(context.Context, string) string { return "dev" },
		Node:   func(string) string { return "show" },
		HasAction: func(_ context.Context, domain, service string) bool {
			for _, a := range actions {
				if domain+"."+service == a {
					return true
				}
			}
			return false
		},
		BaseURL: func(context.Context) string { return "http://ha:8123" },
	})
	return w, logs
}

const thread = "t1"

func TestWA01_PairingShowsTheWaitOnTheShow(t *testing.T) {
	ha := newFakeHA()
	ha.states["switch.show_video"] = "on"
	ha.states["sensor.show_video_state"] = "idle"
	w, _ := newWaiting(ha, "esphome.show_play_video")
	w.Start(context.Background(), thread)
	waitFor(t, "the video", func() bool { return len(ha.callsOf("esphome.show_play_video")) == 1 })
	plays := ha.callsOf("esphome.show_play_video")
	if len(plays) != 1 || !strings.Contains(plays[0], "http://ha:8123/api/mira/panel/ui/videos/wait-de.mp4") {
		t.Fatalf("plays = %v", plays)
	}
	// It plays to its end; while the thread waits it plays again (decision 4).
	ha.push("sensor.show_video_state", "loading")
	ha.push("sensor.show_video_state", "playing")
	ha.push("sensor.show_video_state", "idle")
	waitFor(t, "the replay", func() bool { return len(ha.callsOf("esphome.show_play_video")) == 2 })
	// Paused by a turn and playing again is no end.
	ha.push("sensor.show_video_state", "playing")
	ha.push("sensor.show_video_state", "paused")
	ha.push("sensor.show_video_state", "playing")
	time.Sleep(20 * time.Millisecond)
	if n := len(ha.callsOf("esphome.show_play_video")); n != 2 {
		t.Errorf("plays after a pause = %d, want 2", n)
	}
	w.Stop(context.Background(), thread)
}

func TestWA02_TheDeviceTurnsUpAndTheVideoStops(t *testing.T) {
	ha := newFakeHA()
	ha.states["sensor.show_video_state"] = "idle"
	w, _ := newWaiting(ha, "esphome.show_play_video")
	w.Start(context.Background(), thread)
	waitFor(t, "the video", func() bool { return len(ha.callsOf("esphome.show_play_video")) == 1 })
	ha.push("sensor.show_video_state", "playing")
	w.Stop(context.Background(), thread)
	if got := ha.callsOf("esphome.show_stop_video"); len(got) != 1 {
		t.Fatalf("stops = %v", got)
	}
	// Its end after the stop plays nothing again.
	ha.push("sensor.show_video_state", "idle")
	time.Sleep(20 * time.Millisecond)
	if n := len(ha.callsOf("esphome.show_play_video")); n != 1 {
		t.Errorf("plays = %d after the stop, want 1", n)
	}
}

func TestWA03_CancellingStopsTheVideo(t *testing.T) {
	// Cancelling ends the thread, and the thread's end stops the wait as
	// any end does (cmd/mira: Ended); here the timer of an Echo.
	ha := newFakeHA()
	w, _ := newWaiting(ha, "esphome.show_timer_start_silent")
	w.Start(context.Background(), thread)
	waitFor(t, "the timer", func() bool { return len(ha.callsOf("esphome.show_timer_start_silent")) == 1 })
	starts := ha.callsOf("esphome.show_timer_start_silent")
	if len(starts) != 1 || !strings.Contains(starts[0], `"color":"#40E0D0"`) || !strings.Contains(starts[0], `"duration":"60s"`) {
		t.Fatalf("timer = %v", starts)
	}
	ha.push("sensor.show_timers", "Mira wartet at 4:21 PM")
	w.Stop(context.Background(), thread)
	if got := ha.callsOf("esphome.show_timer_cancel"); len(got) != 1 || !strings.Contains(got[0], `"id":"local:t1"`) {
		t.Fatalf("cancel = %v", got)
	}
}

func TestWA03_TheTimerRunsAgainWhileTheThreadWaits(t *testing.T) {
	ha := newFakeHA()
	w, _ := newWaiting(ha, "esphome.show_timer_start_silent")
	w.Start(context.Background(), thread)
	waitFor(t, "the timer", func() bool { return len(ha.callsOf("esphome.show_timer_start_silent")) == 1 })
	// Not there yet: no restart.
	ha.push("sensor.show_timers", "")
	ha.push("sensor.show_timers", "Mira wartet at 4:21 PM")
	ha.push("sensor.show_timers", "")
	waitFor(t, "the timer again", func() bool { return len(ha.callsOf("esphome.show_timer_start_silent")) == 2 })
	w.Stop(context.Background(), thread)
}

func TestWA04_ASatelliteWithoutVideoShowsNothing(t *testing.T) {
	ha := newFakeHA()
	w, logs := newWaiting(ha)
	w.Start(context.Background(), thread)
	w.Stop(context.Background(), thread)
	if len(ha.callsOf("")) != 0 {
		t.Errorf("calls = %v", ha.callsOf(""))
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("log: %s", logs)
	}
}

func TestWA05_TheShowsVideoSwitchIsTurnedOn(t *testing.T) {
	ha := newFakeHA()
	ha.states["switch.show_video"] = "off"
	ha.states["sensor.show_video_error"] = "video: videos are off on this device"
	w, logs := newWaiting(ha, "esphome.show_play_video")
	w.Start(context.Background(), thread)
	waitFor(t, "the video", func() bool { return len(ha.callsOf("esphome.show_play_video")) == 1 })
	on := ha.callsOf("switch.turn_on")
	if len(on) != 1 || !strings.Contains(on[0], "switch.show_video") {
		t.Fatalf("switch = %v", on)
	}
	if len(ha.callsOf("esphome.show_play_video")) != 1 {
		t.Fatalf("not played after the switch")
	}
	// The error of before is not this video's.
	ha.push("sensor.show_video_state", "playing")
	if strings.Count(logs.String(), "turned the satellite's video on") != 1 || strings.Contains(logs.String(), "refused") {
		t.Errorf("log: %s", logs)
	}
	w.Stop(context.Background(), thread)
}

func TestWaitingVideoThatDoesNotLoadIsNotPlayedAgain(t *testing.T) {
	ha := newFakeHA()
	ha.states["sensor.show_video_state"] = "idle"
	w, logs := newWaiting(ha, "esphome.show_play_video")
	w.Start(context.Background(), thread)
	waitFor(t, "the video", func() bool { return len(ha.callsOf("esphome.show_play_video")) == 1 })
	ha.push("sensor.show_video_state", "loading")
	ha.push("sensor.show_video_state", "idle")
	waitFor(t, "the log", func() bool { return strings.Contains(logs.String(), "waiting video refused") })
	if n := len(ha.callsOf("esphome.show_play_video")); n != 1 {
		t.Errorf("plays = %d, want 1", n)
	}
}

func TestTheVideosAreThere(t *testing.T) {
	for _, name := range []string{"wait-de", "wait-en"} {
		if data, ok := Video(name); !ok || len(data) < 1000 {
			t.Errorf("%s: %d bytes, %v", name, len(data), ok)
		}
	}
	if _, ok := Video("../waiting"); ok {
		t.Error("another name is served")
	}
}

func TestWA06_TheWaitShowsOnceTheSatelliteIsIdleAfterTheAnswer(t *testing.T) {
	ha := newFakeHA()
	ha.states["assist_satellite.show_assist_satellit"] = "processing"
	w, _ := newWaiting(ha, "esphome.show_timer_start_silent")
	w.o.Satellite = func(string) string { return "assist_satellite.show_assist_satellit" }
	w.Start(context.Background(), thread)
	ha.push("assist_satellite.show_assist_satellit", "responding")
	time.Sleep(30 * time.Millisecond)
	if n := len(ha.callsOf("esphome.show_timer_start_silent")); n != 0 {
		t.Fatalf("the timer started while the satellite answers (%d)", n)
	}
	ha.push("assist_satellite.show_assist_satellit", "idle")
	waitFor(t, "the timer after the answer", func() bool { return len(ha.callsOf("esphome.show_timer_start_silent")) == 1 })
	w.Stop(context.Background(), thread)
}

func TestWA06_StoppedBeforeTheSatelliteIsIdleShowsNothing(t *testing.T) {
	ha := newFakeHA()
	ha.states["assist_satellite.show_assist_satellit"] = "responding"
	w, _ := newWaiting(ha, "esphome.show_timer_start_silent")
	w.o.Satellite = func(string) string { return "assist_satellite.show_assist_satellit" }
	w.Start(context.Background(), thread)
	w.Stop(context.Background(), thread)
	ha.push("assist_satellite.show_assist_satellit", "idle")
	time.Sleep(30 * time.Millisecond)
	if n := len(ha.callsOf("esphome.show_timer_start_silent")); n != 0 {
		t.Errorf("the timer started after the stop (%d)", n)
	}
}
