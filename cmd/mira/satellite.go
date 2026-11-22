package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/alarm"
	"github.com/miraassist/mira/internal/excerpt"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/radio"
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

// alarmMusic finds what an alarm wakes with, when it is set
// (docs/spec/alarm.md, decision 10): where the area has an exposed Music
// Assistant player, Music Assistant's search on it, any kind (station,
// playlist, album, artist, track), and that player plays it; else only a
// station from Radio Browser, as radio_play, on the satellite's speaker. A
// station's name with a space is searched once more without it, as radio_play
// does. Cost: one search, two for a name with a space and no hit.
func (a *app) alarmMusic(ctx context.Context, area, speaker, name, kind string) (*alarm.Music, []string, error) {
	snap := a.structure.Snapshot()
	var players []string
	for id := range snap.MusicPlayers {
		if snap.EntityArea[id] == area {
			players = append(players, id)
		}
	}
	sort.Strings(players)
	if len(players) == 0 {
		if kind != "" && kind != "radio" {
			return nil, nil, alarm.ErrNeedsMusicAssistant
		}
		search := radio.Search(a.ha)
		stations, err := search(ctx, name)
		if joined := strings.Join(strings.Fields(name), ""); err == nil && len(stations) == 0 && joined != strings.TrimSpace(name) {
			stations, err = search(ctx, joined)
		}
		if err != nil {
			return nil, nil, err
		}
		st, titles := radio.Pick(stations, name)
		if st == nil {
			return nil, titles, nil
		}
		return &alarm.Music{Player: speaker, ContentID: st.ContentID, ContentType: st.ContentType, Title: st.Title}, nil, nil
	}
	player := players[0]
	search := func(query string) ([]alarm.Hit, error) {
		raw, err := a.ha.CallServiceResponse(ctx, "media_player", "search_media",
			map[string]any{"entity_id": player, "search_query": query})
		if err != nil {
			return nil, err
		}
		var res map[string]struct {
			Result []struct {
				Class       string `json:"media_class"`
				Title       string `json:"title"`
				ContentID   string `json:"media_content_id"`
				ContentType string `json:"media_content_type"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("search_media answered %q: %w", excerpt.Bytes(raw), err)
		}
		var out []alarm.Hit
		for _, r := range res[player].Result {
			out = append(out, alarm.Hit{Class: r.Class, Title: r.Title, ContentID: r.ContentID, ContentType: r.ContentType})
		}
		return out, nil
	}
	hits, err := search(name)
	if err != nil {
		return nil, nil, err
	}
	hit, titles := alarm.Pick(hits, name, kind)
	if joined := strings.Join(strings.Fields(name), ""); hit == nil && len(titles) == 0 && kind == "radio" && joined != strings.TrimSpace(name) {
		if hits, err = search(joined); err != nil {
			return nil, nil, err
		}
		hit, titles = alarm.Pick(hits, name, kind)
	}
	if hit == nil {
		return nil, titles, nil
	}
	return &alarm.Music{Player: player, ContentID: hit.ContentID, ContentType: hit.ContentType, Title: hit.Title}, nil, nil
}
