package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// speakerOf is the media player of a satellite's device, where Mira plays
// her sounds (docs/spec/satellite.md); "" if the device has none.
func (a *app) speakerOf(_ context.Context, deviceID string) string {
	return a.deviceEntity(deviceID, "media_player")
}

// cardActionOf is the picture card action of a satellite's device
// (esphome.<node>_notify_picture, TECHO5); "" if the device has none.
func (a *app) cardActionOf(ctx context.Context, deviceID string) string {
	node := a.deviceNode(deviceID)
	if node == "" || !a.hasAction(ctx, "esphome", node+"_notify_picture") {
		return ""
	}
	return "esphome." + node + "_notify_picture"
}

// mediaPlaying says whether media plays on a satellite's device, for a sound
// (docs/spec/satellite.md). The speaker also reports "playing" while Mira
// speaks, so a TECHO5's own sensor <node>_now_playing_state comes first: it
// counts media only (seen 2026-11-05: "paused" throughout the quiz's
// answers). Without it, the speaker's state. from is the entity read.
func (a *app) mediaPlaying(ctx context.Context, deviceID, speaker string) (bool, string, error) {
	ids := []string{speaker}
	if node := a.deviceNode(deviceID); node != "" {
		ids = []string{"sensor." + node + "_now_playing_state", speaker}
	}
	states, err := a.ha.GetStates(ctx, haws.StateFilter{EntityIDs: ids})
	if err != nil {
		return false, ids[0], err
	}
	by := map[string]string{}
	for _, s := range states {
		by[s.EntityID] = s.State
	}
	for _, id := range ids {
		if st, ok := by[id]; ok {
			return st == "playing", id, nil
		}
	}
	return false, speaker, nil
}

// watchOne subscribes to one entity's state (subscribe_entities filtered to
// it) and hands each change on with since when it holds; for the speaker of
// a spoken answer (docs/spec/streaming.md, step 10). The first state Home
// Assistant sends may come before the subscription returns: it is handed on
// then.
func (a *app) watchOne(ctx context.Context, entity string, changed func(state string, since time.Time)) (func(), error) {
	var mu sync.Mutex
	var w *haws.StateWatch
	last := ""
	check := func() {
		mu.Lock()
		defer mu.Unlock()
		if w == nil {
			return
		}
		st, ok := w.State(entity)
		if !ok || st.State+st.LastChanged == last {
			return
		}
		last = st.State + st.LastChanged
		since, err := time.Parse(time.RFC3339Nano, st.LastChanged)
		if err != nil {
			since = time.Now()
		}
		changed(st.State, since)
	}
	if a.ha == nil {
		return nil, errors.New("no connection to Home Assistant")
	}
	ww, err := a.ha.WatchStates(ctx, haws.StateFilter{EntityIDs: []string{entity}}, check)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	w = ww
	mu.Unlock()
	check()
	return ww.Stop, nil
}

// supportedFeatures is an entity's supported_features, read once; for an
// alarm, whether the satellite's speaker can set its volume and announce
// (docs/spec/alarm.md).
func (a *app) supportedFeatures(ctx context.Context, entity string) (int, error) {
	states, err := a.ha.GetStates(ctx, haws.StateFilter{EntityIDs: []string{entity}})
	if err != nil {
		return 0, err
	}
	for _, s := range states {
		if s.EntityID == entity {
			f, _ := s.Attributes["supported_features"].(float64)
			return int(f), nil
		}
	}
	return 0, fmt.Errorf("home assistant has no state of %s", entity)
}
