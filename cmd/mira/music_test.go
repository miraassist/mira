package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/music"
)

// MU-43 Relative: the place in the song comes from the player's attributes
func TestMU43_ThePlaceInTheSongComesFromThePlayersAttributes(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	var st music.State
	placeInSong(log, "media_player.onkyo", map[string]any{"media_position": 12.5, "media_duration": 1052.0,
		"media_position_updated_at": "2026-11-20T20:14:50.123456+00:00"}, &st)
	want := time.Date(2026, 11, 20, 20, 14, 50, 123456000, time.UTC)
	if !st.PositionKnown || st.Position != 12.5 || st.Duration != 1052 || !st.PositionAt.Equal(want) {
		t.Errorf("%+v", st)
	}
	st = music.State{}
	placeInSong(log, "media_player.onkyo", map[string]any{"state": "idle"}, &st)
	if st.PositionKnown || st.Duration != 0 || !st.PositionAt.IsZero() {
		t.Errorf("without attributes: %+v", st)
	}
	st = music.State{}
	placeInSong(log, "media_player.onkyo", map[string]any{"media_position": 3.0, "media_position_updated_at": "gestern"}, &st)
	if !st.PositionKnown || !st.PositionAt.IsZero() ||
		!strings.Contains(logs.String(), "media_position_updated_at does not parse") || !strings.Contains(logs.String(), "value=gestern") {
		t.Errorf("unparsable time: %+v\n%s", st, logs.String())
	}
}
