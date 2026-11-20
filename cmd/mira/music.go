package main

import (
	"context"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/music"
)

// musicPlayers are the exposed Music Assistant players of the house
// structure (docs/spec/music-players.md).
func musicPlayers(snap structure.Snapshot) []music.Player {
	out := make([]music.Player, 0, len(snap.MusicPlayers))
	for id := range snap.MusicPlayers {
		area := snap.EntityArea[id]
		out = append(out, music.Player{Entity: id, Name: snap.EntityLabel[id], Names: snap.EntityNames[id],
			Area: area, AreaName: snap.AreaNames[area], Device: snap.EntityDevice[id]})
	}
	return out
}

// groupMembers is a player's attribute group_members: the players of its
// group, itself among them; empty when it is in none.
func groupMembers(attrs map[string]any) []string {
	list, _ := attrs["group_members"].([]any)
	out := make([]string, 0, len(list))
	for _, m := range list {
		if id, ok := m.(string); ok {
			out = append(out, id)
		}
	}
	return out
}

// duckOptions connect ducking (docs/spec/music-players.md, part 7) to Home
// Assistant: the satellites' states from subscribe_entities filtered to the
// domain assist_satellite, the players' volumes from a read filtered to them.
func (a *app) duckOptions(snapshot func() structure.Snapshot) music.DuckOptions {
	return music.DuckOptions{
		Logger:  a.log,
		Players: func() []music.Player { return musicPlayers(snapshot()) },
		Satellite: func(entity string) (string, string) {
			snap := snapshot()
			e := snap.Entities[entity]
			if e.AreaID != "" {
				return e.AreaID, e.DeviceID
			}
			return snap.DeviceArea[e.DeviceID], e.DeviceID
		},
		Watch: func(ctx context.Context, changed func()) (func() map[string]string, func(), error) {
			w, err := a.ha.WatchStates(ctx, haws.StateFilter{Domains: []string{"assist_satellite"}}, changed)
			if err != nil {
				return nil, nil, err
			}
			states := func() map[string]string {
				out := map[string]string{}
				for _, s := range w.States() {
					out[s.EntityID] = s.State
				}
				return out
			}
			return states, w.Stop, nil
		},
		Volumes: func(ctx context.Context, entities []string) (map[string]music.Volume, error) {
			states, err := a.ha.GetStates(ctx, haws.StateFilter{EntityIDs: entities})
			if err != nil {
				return nil, err
			}
			out := map[string]music.Volume{}
			for _, s := range states {
				level, ok := s.Attributes["volume_level"].(float64)
				out[s.EntityID] = music.Volume{State: s.State, Level: level, Known: ok}
			}
			return out, nil
		},
		SetVolume: func(ctx context.Context, entity string, level float64) error {
			return a.ha.CallService(ctx, "media_player", "volume_set", map[string]any{"volume_level": level},
				&haws.Target{EntityID: []string{entity}})
		},
	}
}
