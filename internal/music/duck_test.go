package music_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/music"
)

// duckHA is the fake Home Assistant of the ducking tests: the players'
// states and volumes, the volume_set calls.
type duckHA struct {
	states    map[string]string
	volumes   map[string]float64
	calls     []string // "player 0.23"
	reads     [][]string
	readError error
	refuse    map[string]bool // volume_set refused for these players
}

func newDuckHA() *duckHA {
	return &duckHA{
		states:  map[string]string{"media_player.onkyo": "playing", "media_player.sonos": "paused", "media_player.show_five": "playing"},
		volumes: map[string]float64{"media_player.onkyo": 0.7, "media_player.sonos": 0.5, "media_player.show_five": 0.6},
		refuse:  map[string]bool{},
	}
}

// The satellites of the tests and their areas: two in the living room, one
// in the kitchen, one without an area.
var satellites = map[string]string{
	"assist_satellite.wohnzimmer": "wohnzimmer",
	"assist_satellite.couch":      "wohnzimmer",
	"assist_satellite.kueche":     "kueche",
	"assist_satellite.flur":       "",
}

// duckPlayers are the house's players, a Cast group in the living room
// (paused unless a test plays it), and two devices there that lower
// themselves while they listen (decision 39), both playing.
func duckPlayers() []music.Player {
	return append(house(),
		music.Player{Entity: "media_player.wohnzimmer_cast", Area: "wohnzimmer", Manufacturer: "Google Inc."},
		music.Player{Entity: "media_player.wohnzimmer_dot", Area: "wohnzimmer", Manufacturer: "TECHO5"},
		music.Player{Entity: "media_player.wohnzimmer_echo", Area: "wohnzimmer", Manufacturer: "EchoLocal"})
}

func duckOptions(ha *duckHA, logs *bytes.Buffer) music.DuckOptions {
	ha.states["media_player.wohnzimmer_cast"] = "paused"
	ha.volumes["media_player.wohnzimmer_cast"] = 0.9
	for _, id := range []string{"media_player.wohnzimmer_dot", "media_player.wohnzimmer_echo"} {
		ha.states[id], ha.volumes[id] = "playing", 0.8
	}
	return music.DuckOptions{
		Logger:    slog.New(slog.NewTextHandler(logs, nil)),
		Players:   duckPlayers,
		Satellite: func(entity string) string { return satellites[entity] },
		Named: func(name, area string) []string {
			var out []string
			for _, p := range duckPlayers() {
				if (area == "" || areas[strings.ToLower(area)] == p.Area) && slices.Contains(p.Names, strings.ToLower(name)) {
					out = append(out, p.Entity)
				}
			}
			return out
		},
		Area: func(name string) string { return areas[strings.ToLower(name)] },
		Volumes: func(_ context.Context, ids []string) (map[string]music.Volume, error) {
			ha.reads = append(ha.reads, slices.Clone(ids))
			if ha.readError != nil {
				return nil, ha.readError
			}
			out := map[string]music.Volume{}
			for _, id := range ids {
				v, ok := ha.volumes[id]
				out[id] = music.Volume{State: ha.states[id], Level: v, Known: ok}
			}
			return out, nil
		},
		SetVolume: func(_ context.Context, id string, level float64) error {
			if ha.refuse[id] {
				return errors.New("entity unavailable")
			}
			ha.calls = append(ha.calls, fmt.Sprintf("%s %.2f", id, level))
			ha.volumes[id] = level
			return nil
		},
	}
}

// step gives the ducker the satellites' states; the ones not named are idle.
func step(d *music.Ducker, active map[string]string) {
	states := map[string]string{}
	for s := range satellites {
		states[s] = "idle"
	}
	for s, st := range active {
		states[s] = st
	}
	d.Update(context.Background(), states)
}

func newDucker(ha *duckHA, logs *bytes.Buffer) *music.Ducker {
	d := music.NewDucker(duckOptions(ha, logs))
	step(d, nil) // the first states are only recorded
	return d
}

func wantCalls(t *testing.T, ha *duckHA, want ...string) {
	t.Helper()
	if !slices.Equal(ha.calls, want) {
		t.Fatalf("volume_set calls %v, want %v", ha.calls, want)
	}
}

// MU-41 While a satellite listens, the players that play in its area go down to a third, but not those that lower themselves
func TestMU41_WhileASatelliteListensThePlayersThatPlayInItsAreaGoDownButNotThoseThatLowerThemselves(t *testing.T) {
	ha, logs := newDuckHA(), &bytes.Buffer{}
	d := newDucker(ha, logs)
	if len(ha.reads) != 0 || len(ha.calls) != 0 {
		t.Fatalf("the first states lowered something: reads %v, calls %v", ha.reads, ha.calls)
	}
	step(d, map[string]string{"assist_satellite.wohnzimmer": "listening"})
	// Onkyo plays (0.7 -> 0.23); Sonos and the Cast group are paused; the
	// kitchen is another area; the TECHO5 and EchoLocal devices play but
	// lower themselves.
	wantCalls(t, ha, "media_player.onkyo 0.23")
	if len(ha.reads) != 1 || !slices.Equal(ha.reads[0], []string{"media_player.onkyo", "media_player.sonos", "media_player.wohnzimmer_cast"}) {
		t.Errorf("reads %v, want one filtered to the living room's players without those that lower themselves", ha.reads)
	}
	if !strings.Contains(logs.String(), "music lowered while a satellite listens") {
		t.Errorf("no log line:\n%s", logs)
	}
	// A satellite without an area lowers nothing and reads nothing.
	step(d, map[string]string{"assist_satellite.wohnzimmer": "listening", "assist_satellite.flur": "listening"})
	if len(ha.reads) != 1 || len(ha.calls) != 1 {
		t.Errorf("the satellite without an area did something: reads %v, calls %v", ha.reads, ha.calls)
	}
}

// MU-35 The follow-up question keeps the music low; idle sets it back
func TestMU35_TheFollowUpKeepsTheMusicLowIdleSetsItBack(t *testing.T) {
	ha, logs := newDuckHA(), &bytes.Buffer{}
	d := newDucker(ha, logs)
	sat := "assist_satellite.kueche"
	for _, st := range []string{"listening", "processing", "responding", "listening", "processing", "responding"} {
		step(d, map[string]string{sat: st})
	}
	wantCalls(t, ha, "media_player.show_five 0.20")
	step(d, nil)
	wantCalls(t, ha, "media_player.show_five 0.20", "media_player.show_five 0.60")
	if !strings.Contains(logs.String(), "music set back after the turn") {
		t.Errorf("no log line:\n%s", logs)
	}
	// A satellite that becomes unavailable ends its turn as idle does.
	step(d, map[string]string{sat: "listening"})
	step(d, map[string]string{sat: "unavailable"})
	wantCalls(t, ha, "media_player.show_five 0.20", "media_player.show_five 0.60",
		"media_player.show_five 0.20", "media_player.show_five 0.60")
	// A player that stopped during the turn ("stop") is set back all the same.
	step(d, map[string]string{sat: "idle"})
	step(d, map[string]string{sat: "listening"})
	ha.states["media_player.show_five"] = "paused"
	step(d, nil)
	if got := ha.volumes["media_player.show_five"]; got != 0.6 {
		t.Errorf("the paused player's volume %v, want 0.6", got)
	}
}

// MU-40 A volume tool call of the turn keeps the players it reaches; any other volume is set back
func TestMU40_AVolumeToolCallKeepsThePlayersItReaches(t *testing.T) {
	lowerBoth := func(t *testing.T) (*duckHA, *bytes.Buffer, *music.Ducker) {
		t.Helper()
		ha, logs := newDuckHA(), &bytes.Buffer{}
		ha.states["media_player.sonos"] = "playing"
		d := newDucker(ha, logs)
		ha.states["media_player.wohnzimmer_cast"] = "playing"
		step(d, map[string]string{"assist_satellite.couch": "listening"})
		wantCalls(t, ha, "media_player.onkyo 0.23", "media_player.sonos 0.17", "media_player.wohnzimmer_cast 0.30")
		ha.calls = nil
		return ha, logs, d
	}
	const wz = "wohnzimmer"
	t.Run("a rounded volume is set back all the same", func(t *testing.T) {
		ha, _, d := lowerBoth(t)
		ha.volumes["media_player.onkyo"] = 0.06 // the device's own step, not Mira's 0.23
		step(d, nil)
		wantCalls(t, ha, "media_player.onkyo 0.70", "media_player.sonos 0.50", "media_player.wohnzimmer_cast 0.90")
		if len(ha.reads) != 1 {
			t.Errorf("reads %v, want only the one when lowering", ha.reads)
		}
	})
	t.Run("no target: the players of the turn's area keep their volume", func(t *testing.T) {
		ha, logs, d := lowerBoth(t)
		d.VolumeSet(music.VolumeCall{TurnArea: wz})
		step(d, nil)
		wantCalls(t, ha)
		if !strings.Contains(logs.String(), "music left at the volume set by a volume command of the turn") {
			t.Errorf("no log line:\n%s", logs)
		}
	})
	t.Run("a name keeps that player only", func(t *testing.T) {
		ha, _, d := lowerBoth(t)
		d.VolumeSet(music.VolumeCall{TurnArea: wz, Name: "Onkyo"})
		step(d, nil)
		wantCalls(t, ha, "media_player.sonos 0.50", "media_player.wohnzimmer_cast 0.90")
	})
	t.Run("an area keeps the players of that area; another area keeps nothing here", func(t *testing.T) {
		ha, _, d := lowerBoth(t)
		d.VolumeSet(music.VolumeCall{TurnArea: wz, Area: "Küche"})
		step(d, nil)
		wantCalls(t, ha, "media_player.onkyo 0.70", "media_player.sonos 0.50", "media_player.wohnzimmer_cast 0.90")
		ha.calls = nil
		step(d, map[string]string{"assist_satellite.couch": "listening"})
		ha.calls = nil
		d.VolumeSet(music.VolumeCall{TurnArea: wz, Area: "Wohnzimmer"})
		step(d, nil)
		wantCalls(t, ha)
	})
	t.Run("a floor keeps every lowered player", func(t *testing.T) {
		ha, _, d := lowerBoth(t)
		d.VolumeSet(music.VolumeCall{TurnArea: wz, Floor: "Erdgeschoss"})
		step(d, nil)
		wantCalls(t, ha)
	})
	t.Run("a call of a turn from an area that is not lowered counts for nothing", func(t *testing.T) {
		ha, _, d := lowerBoth(t)
		d.VolumeSet(music.VolumeCall{TurnArea: "kueche", Name: "Onkyo"})
		d.VolumeSet(music.VolumeCall{Name: "Onkyo"})
		step(d, nil)
		wantCalls(t, ha, "media_player.onkyo 0.70", "media_player.sonos 0.50", "media_player.wohnzimmer_cast 0.90")
	})
	t.Run("the mark ends with the turn", func(t *testing.T) {
		ha, _, d := lowerBoth(t)
		d.VolumeSet(music.VolumeCall{TurnArea: wz})
		step(d, nil)
		wantCalls(t, ha)
		// The user's new volumes, then the next turn: lowered and set back.
		ha.volumes["media_player.onkyo"], ha.volumes["media_player.sonos"], ha.volumes["media_player.wohnzimmer_cast"] = 0.7, 0.5, 0.9
		step(d, map[string]string{"assist_satellite.couch": "listening"})
		step(d, nil)
		wantCalls(t, ha, "media_player.onkyo 0.23", "media_player.sonos 0.17", "media_player.wohnzimmer_cast 0.30",
			"media_player.onkyo 0.70", "media_player.sonos 0.50", "media_player.wohnzimmer_cast 0.90")
	})
	t.Run("VolumeSet never blocks", func(t *testing.T) {
		_, _, d := lowerBoth(t)
		for range 1000 {
			d.VolumeSet(music.VolumeCall{TurnArea: wz})
		}
	})
}

// MU-37 Two satellites in one room: the music comes back when both are idle
func TestMU37_TwoSatellitesInOneRoomTheMusicComesBackWhenBothAreIdle(t *testing.T) {
	ha, logs := newDuckHA(), &bytes.Buffer{}
	d := newDucker(ha, logs)
	step(d, map[string]string{"assist_satellite.wohnzimmer": "listening"})
	step(d, map[string]string{"assist_satellite.wohnzimmer": "responding", "assist_satellite.couch": "listening"})
	wantCalls(t, ha, "media_player.onkyo 0.23")
	step(d, map[string]string{"assist_satellite.couch": "processing"})
	wantCalls(t, ha, "media_player.onkyo 0.23")
	step(d, nil)
	wantCalls(t, ha, "media_player.onkyo 0.23", "media_player.onkyo 0.70")
	// A satellite that is removed while it listens ends its turn.
	step(d, map[string]string{"assist_satellite.couch": "listening"})
	d.Update(context.Background(), map[string]string{"assist_satellite.wohnzimmer": "idle"})
	wantCalls(t, ha, "media_player.onkyo 0.23", "media_player.onkyo 0.70", "media_player.onkyo 0.23", "media_player.onkyo 0.70")
}

// MU-38 What fails leaves a log line and the rest goes on
func TestMU38_WhatFailsLeavesALogLineAndTheRestGoesOn(t *testing.T) {
	t.Run("reading fails when lowering", func(t *testing.T) {
		ha, logs := newDuckHA(), &bytes.Buffer{}
		d := newDucker(ha, logs)
		ha.readError = errors.New("connection lost")
		step(d, map[string]string{"assist_satellite.wohnzimmer": "listening"})
		step(d, nil)
		wantCalls(t, ha)
		if !strings.Contains(logs.String(), "nothing is lowered for this turn") || !strings.Contains(logs.String(), "media_player.onkyo") {
			t.Errorf("log:\n%s", logs)
		}
	})
	t.Run("a refused player stays, the others go down", func(t *testing.T) {
		ha, logs := newDuckHA(), &bytes.Buffer{}
		d := newDucker(ha, logs)
		ha.states["media_player.sonos"] = "playing"
		ha.refuse["media_player.onkyo"] = true
		step(d, map[string]string{"assist_satellite.wohnzimmer": "listening"})
		step(d, nil)
		wantCalls(t, ha, "media_player.sonos 0.17", "media_player.sonos 0.50")
		if !strings.Contains(logs.String(), "refused to lower a player") {
			t.Errorf("log:\n%s", logs)
		}
	})
	t.Run("setting back is refused", func(t *testing.T) {
		ha, logs := newDuckHA(), &bytes.Buffer{}
		d := newDucker(ha, logs)
		step(d, map[string]string{"assist_satellite.kueche": "listening"})
		ha.refuse["media_player.show_five"] = true
		step(d, nil)
		if !strings.Contains(logs.String(), "it stays lowered") {
			t.Errorf("log:\n%s", logs)
		}
	})
	t.Run("the watch cannot start", func(t *testing.T) {
		o := duckOptions(newDuckHA(), &bytes.Buffer{})
		o.Watch = func(context.Context, func()) (func() map[string]string, func(), error) {
			return nil, nil, errors.New("not connected")
		}
		err := music.NewDucker(o).Run(context.Background())
		if err == nil || !strings.Contains(err.Error(), "not connected") {
			t.Errorf("Run: %v", err)
		}
	})
}

// MU-39 The subscription's handler only signals; the ducker runs on its own
func TestMU39_TheSubscriptionsHandlerOnlySignals(t *testing.T) {
	ha, logs := newDuckHA(), &bytes.Buffer{}
	o := duckOptions(ha, logs)
	var mu sync.Mutex
	states := map[string]string{"assist_satellite.kueche": "idle"}
	set := func(s string) {
		mu.Lock()
		states["assist_satellite.kueche"] = s
		mu.Unlock()
	}
	var changed func()
	read := make(chan struct{}, 100) // one per read of the states
	ready := make(chan struct{})
	stopped := make(chan struct{})
	o.Watch = func(_ context.Context, c func()) (func() map[string]string, func(), error) {
		changed = c
		close(ready)
		return func() map[string]string {
			mu.Lock()
			defer mu.Unlock()
			read <- struct{}{}
			return map[string]string{"assist_satellite.kueche": states["assist_satellite.kueche"]}
		}, func() { close(stopped) }, nil
	}
	calls := make(chan string, 10)
	set0 := o.SetVolume
	o.SetVolume = func(ctx context.Context, id string, level float64) error {
		err := set0(ctx, id, level)
		calls <- fmt.Sprintf("%s %.2f", id, level)
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- music.NewDucker(o).Run(ctx) }()
	<-ready
	wait := func(want string) {
		t.Helper()
		select {
		case got := <-calls:
			if got != want {
				t.Fatalf("call %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no call %q", want)
		}
	}
	changed() // the first states: only recorded
	<-read
	set("listening")
	for range 50 { // many changes at once never block the read loop
		changed()
	}
	wait("media_player.show_five 0.20")
	set("idle")
	changed()
	wait("media_player.show_five 0.60")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run: %v", err)
	}
	<-stopped
}
