package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/music"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/tools"
)

// musicPlayers are the exposed Music Assistant players of the house
// structure (docs/spec/music-players.md).
func musicPlayers(snap structure.Snapshot) []music.Player {
	out := make([]music.Player, 0, len(snap.MusicPlayers))
	for id := range snap.MusicPlayers {
		area := snap.EntityArea[id]
		out = append(out, music.Player{Entity: id, Name: snap.EntityLabel[id], Names: snap.EntityNames[id],
			Area: area, AreaName: snap.AreaNames[area], Device: snap.EntityDevice[id],
			Manufacturer: snap.Devices[snap.EntityDevice[id]].Manufacturer})
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

// placeInSong reads a player's media_position, media_position_updated_at
// and media_duration into st (docs/spec/music-players.md, part 8). A time
// that does not parse leaves a log line; the position is then taken as it is
// (decision 43).
func placeInSong(log *slog.Logger, entity string, attrs map[string]any, st *music.State) {
	st.Position, st.PositionKnown = attrs["media_position"].(float64)
	st.Duration, _ = attrs["media_duration"].(float64)
	at, ok := attrs["media_position_updated_at"].(string)
	if !ok || at == "" {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		log.Warn("music: media_position_updated_at does not parse; the position is taken as it is",
			"player", entity, "value", at, "error", err)
		return
	}
	st.PositionAt = t
}

// volumeTools are Home Assistant's tools that set a player's volume: a call
// of one during a turn keeps the players it reaches at their new volume
// (docs/spec/music-players.md, part 7, decision 35). Muting is not among
// them: setting the volume back does not touch it.
var volumeTools = []string{"HassSetVolume", "HassSetVolumeRelative"}

// volumeCall tells the ducker of a successful call of one of Home
// Assistant's volume tools, with the turn's area and the call's targets.
func volumeCall(log *slog.Logger, d *music.Ducker, inv tools.Invocation) {
	server, name := prompt.SplitQualifiedName(inv.Name)
	if server != haServer || !slices.Contains(volumeTools, name) || inv.Area == "" {
		return
	}
	var args map[string]any
	if len(inv.Args) > 0 {
		if err := json.Unmarshal(inv.Args, &args); err != nil {
			log.Warn("ducking: the arguments of a volume tool call are no JSON object; the players are set back after the turn",
				"tool", inv.Name, "args", excerpt.Bytes(inv.Args), "error", err)
			return
		}
	}
	str := func(k string) string { s, _ := args[k].(string); return s }
	d.VolumeSet(music.VolumeCall{TurnArea: inv.Area, Name: str("name"), Area: str("area"), Floor: str("floor")})
}

// duckOptions connect ducking (docs/spec/music-players.md, part 7) to Home
// Assistant: the satellites' states from subscribe_entities filtered to the
// domain assist_satellite, the players' volumes from a read filtered to them.
func (a *app) duckOptions(snapshot func() structure.Snapshot) music.DuckOptions {
	return music.DuckOptions{
		Logger:  a.log,
		Players: func() []music.Player { return musicPlayers(snapshot()) },
		Satellite: func(entity string) string {
			snap := snapshot()
			e := snap.Entities[entity]
			if e.AreaID != "" {
				return e.AreaID
			}
			return snap.DeviceArea[e.DeviceID]
		},
		Named: func(name, area string) []string { return snapshot().ResolveName(name, area) },
		Area:  func(name string) string { return snapshot().ResolveArea(name) },
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
