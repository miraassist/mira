package main

import (
	"context"
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
