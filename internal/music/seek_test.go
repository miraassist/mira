package music_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/music"
	"github.com/miraassist/mira/internal/tools"
)

// testNow is the tools' clock in the tests, far from the real time.
var testNow = time.Date(2031, 3, 4, 5, 6, 7, 0, time.UTC)

func seekTool(ha *fakeHA, players []music.Player, logs *bytes.Buffer) tools.InternalTool {
	return music.SeekTool(options(ha, players, logs))
}

func askSeek(t *testing.T, tl tools.InternalTool, area, args string) (string, error) {
	t.Helper()
	return tl.Run(context.Background(), tools.Invocation{Name: tl.Name, Args: json.RawMessage(args), Area: area})
}

// at is a place in the song: position seconds, reported ago before testNow,
// in a song of duration seconds (0 for none).
func at(position float64, ago time.Duration, duration float64) music.State {
	return music.State{Position: position, PositionKnown: true, PositionAt: testNow.Add(-ago), Duration: duration}
}

// kitchenPlays is the kitchen's player playing a song at 1:00, reported 10 s
// ago, of 5:00.
func kitchenPlays() *fakeHA {
	return &fakeHA{states: map[string]string{"media_player.show_five": "playing"},
		contents: map[string]string{"media_player.show_five": playing},
		places:   map[string]music.State{"media_player.show_five": at(60, 10*time.Second, 300)}}
}

// MU-42 music_seek is there only with Music Assistant players, and says it is not HassMediaNext
func TestMU42_MusicSeekIsThereOnlyWithMusicAssistantPlayers(t *testing.T) {
	var logs bytes.Buffer
	tl := seekTool(&fakeHA{}, house(), &logs)
	if tl.Name != "music_seek" || !tl.Enabled() || tl.ReadOnly {
		t.Errorf("with players: %+v", tl)
	}
	for _, example := range []string{"Spring dreißig Sekunden vor", "Geh eine Minute zurück", "Spul zwei Minuten vor",
		"Geh an den Anfang", "Spring zu Minute drei", "nicht HassMediaNext"} {
		if !strings.Contains(tl.Description, example) {
			t.Errorf("description lacks %q", example)
		}
	}
	o := options(&fakeHA{}, house(), &logs)
	o.Lang = i18n.EN
	en := music.SeekTool(o).Description
	for _, example := range []string{"Skip thirty seconds ahead", "Go back a minute", "Fast forward two minutes",
		"Go to the beginning", "Jump to minute three", "not HassMediaNext"} {
		if !strings.Contains(en, example) {
			t.Errorf("English description lacks %q: %q", example, en)
		}
	}
	for _, p := range []string{`"seconds"`, `"position"`, `"player"`} {
		if !strings.Contains(tl.Schema, p) {
			t.Errorf("schema lacks %s: %s", p, tl.Schema)
		}
	}
	if strings.Contains(tl.Schema, "required") {
		t.Errorf("schema %s", tl.Schema)
	}
	if seekTool(&fakeHA{}, nil, &logs).Enabled() {
		t.Error("offered without a Music Assistant player")
	}
}

// MU-43 Relative: from the current place by the clock while playing, from the position while paused
func TestMU43_RelativeFromTheCurrentPlace(t *testing.T) {
	var logs bytes.Buffer
	ha := kitchenPlays()
	out, err := askSeek(t, seekTool(ha, house(), &logs), "", `{"seconds": 30}`)
	if err != nil || !slices.Equal(ha.seeks, []string{"media_player.show_five -> 100"}) {
		t.Fatalf("%q %v %v", out, err, ha.seeks)
	}
	if !strings.Contains(out, "Auf Show Five (Küche) geht es jetzt bei 1:40 im Stück weiter") {
		t.Errorf("answer %q", out)
	}
	if !strings.Contains(logs.String(), `msg="music wound" player=media_player.show_five position=100`) {
		t.Errorf("log %s", logs.String())
	}
	if len(ha.read) != 1 || len(ha.read[0]) != len(house()) {
		t.Errorf("reads %v", ha.read)
	}
	ha.seeks = nil
	if _, err := askSeek(t, seekTool(ha, house(), &logs), "", `{"seconds": -120}`); err != nil ||
		!slices.Equal(ha.seeks, []string{"media_player.show_five -> 0"}) {
		t.Errorf("before the start: %v %v", err, ha.seeks)
	}
	ha = kitchenPlays()
	ha.states["media_player.show_five"] = "paused"
	if _, err := askSeek(t, seekTool(ha, house(), &logs), "", `{"seconds": 30}`); err != nil ||
		!slices.Equal(ha.seeks, []string{"media_player.show_five -> 90"}) {
		t.Errorf("paused: %v %v", err, ha.seeks)
	}
	// Without the time of the report, the position is taken as it is.
	ha = kitchenPlays()
	ha.places["media_player.show_five"] = music.State{Position: 60, PositionKnown: true, Duration: 300}
	if _, err := askSeek(t, seekTool(ha, house(), &logs), "", `{"seconds": 30}`); err != nil ||
		!slices.Equal(ha.seeks, []string{"media_player.show_five -> 90"}) {
		t.Errorf("no report time: %v %v", err, ha.seeks)
	}
}

// MU-44 Absolute: the position is the target, also without a known place or length
func TestMU44_AbsoluteThePositionIsTheTarget(t *testing.T) {
	var logs bytes.Buffer
	ha := kitchenPlays()
	out, err := askSeek(t, seekTool(ha, house(), &logs), "", `{"position": 0}`)
	if err != nil || !slices.Equal(ha.seeks, []string{"media_player.show_five -> 0"}) || !strings.Contains(out, "bei 0:00") {
		t.Errorf("start: %q %v %v", out, err, ha.seeks)
	}
	ha = kitchenPlays()
	ha.places = nil
	out, err = askSeek(t, seekTool(ha, house(), &logs), "", `{"position": 180}`)
	if err != nil || !slices.Equal(ha.seeks, []string{"media_player.show_five -> 180"}) || !strings.Contains(out, "bei 3:00") {
		t.Errorf("unknown place: %q %v %v", out, err, ha.seeks)
	}
}

// MU-45 Beyond the end, a station or no known place: nothing is called
func TestMU45_BeyondTheEndAStationOrNoKnownPlaceCallNothing(t *testing.T) {
	var logs bytes.Buffer
	ha := kitchenPlays()
	ha.places["media_player.show_five"] = at(290, 0, 300)
	tl := seekTool(ha, house(), &logs)
	out, err := askSeek(t, tl, "", `{"seconds": 30}`)
	if err != nil || !strings.Contains(out, "nur 5:00 lang und steht bei 4:50; 5:20 liegt dahinter") {
		t.Errorf("relative: %q %v", out, err)
	}
	out, err = askSeek(t, tl, "", `{"position": 400}`)
	if err != nil || !strings.Contains(out, "nur 5:00 lang") || !strings.Contains(out, "6:40 liegt dahinter") {
		t.Errorf("absolute: %q %v", out, err)
	}
	for _, station := range []string{"library://radio/1", "radiobrowser://radio/ce4b8451-0601-11e8-ae97-52543be04c81"} {
		ha.contents["media_player.show_five"] = station
		for _, args := range []string{`{"seconds": 30}`, `{"position": 0}`} {
			out, err := askSeek(t, tl, "", args)
			if err != nil || !strings.Contains(out, "Radiosender") || !strings.Contains(out, "nicht spulen") {
				t.Errorf("%s %s: %q %v", station, args, out, err)
			}
		}
	}
	ha = kitchenPlays()
	ha.places = nil
	out, err = askSeek(t, seekTool(ha, house(), &logs), "", `{"seconds": -60}`)
	if err != nil || !strings.Contains(out, "sagt nicht, an welcher Stelle im Stück er ist") {
		t.Errorf("no place: %q %v", out, err)
	}
	if len(ha.seeks) != 0 {
		t.Errorf("called %v", ha.seeks)
	}
}

// MU-46 The player is found as music_queue's
func TestMU46_ThePlayerIsFoundAsMusicQueues(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "playing"},
		contents: map[string]string{"media_player.show_five": playing, "media_player.onkyo": hit}}
	tl := seekTool(ha, house(), &logs)
	if _, err := askSeek(t, tl, "kueche", `{"position": 0}`); err != nil ||
		!slices.Equal(ha.seeks, []string{"media_player.show_five -> 0"}) {
		t.Errorf("turn's room: %v %v", err, ha.seeks)
	}
	ha.seeks = nil
	out, err := askSeek(t, tl, "bad", `{"position": 0}`)
	if err != nil || len(ha.seeks) != 0 || !strings.Contains(out, "Onkyo (Wohnzimmer)") || !strings.Contains(out, "Show Five (Küche)") ||
		!strings.Contains(out, "music_seek dann mit player") {
		t.Errorf("several: %q %v %v", out, err, ha.seeks)
	}
	for _, named := range []string{"Wohnzimmer", "Onkyo"} {
		ha.seeks = nil
		if _, err := askSeek(t, tl, "kueche", `{"position": 0, "player": "`+named+`"}`); err != nil ||
			!slices.Equal(ha.seeks, []string{"media_player.onkyo -> 0"}) {
			t.Errorf("%s: %v %v", named, err, ha.seeks)
		}
	}
	ha.seeks = nil
	out, err = askSeek(t, tl, "", `{"position": 0, "player": "Garage"}`)
	if err != nil || len(ha.seeks) != 0 || !strings.Contains(out, "„Garage“ ist weder") {
		t.Errorf("unknown: %q %v %v", out, err, ha.seeks)
	}
	ha = &fakeHA{states: map[string]string{}}
	out, err = askSeek(t, seekTool(ha, house(), &logs), "", `{"seconds": 30}`)
	if err != nil || len(ha.seeks) != 0 || !strings.Contains(out, "keine Musik") {
		t.Errorf("none plays: %q %v %v", out, err, ha.seeks)
	}
}

// MU-47 Exactly one of seconds and position
func TestMU47_ExactlyOneOfSecondsAndPosition(t *testing.T) {
	var logs bytes.Buffer
	ha := kitchenPlays()
	tl := seekTool(ha, house(), &logs)
	for args, want := range map[string]string{
		`{"seconds": 30, "position": 0}`: "both seconds and position",
		`{"player": "Küche"}`:            "neither seconds nor position",
		`{}`:                             "neither seconds nor position",
		`{"position": -5}`:               "below 0",
		`["vor"]`:                        "no JSON object",
		`{"seconds": "dreißig"}`:         "no JSON object",
	} {
		_, err := askSeek(t, tl, "", args)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), strings.ReplaceAll(args, `"`, `\"`)) {
			t.Errorf("%s: %v", args, err)
		}
	}
	if len(ha.read) != 0 || len(ha.seeks) != 0 {
		t.Errorf("read %v, called %v", ha.read, ha.seeks)
	}
}

// MU-48 What fails is the tool's error and names its input
func TestMU48_WhatFailsIsTheToolsErrorAndNamesItsInput(t *testing.T) {
	var logs bytes.Buffer
	ha := kitchenPlays()
	ha.refuse = errors.New("Entity media_player.show_five does not support this service")
	_, err := askSeek(t, seekTool(ha, house(), &logs), "", `{"seconds": 30}`)
	if err == nil || !strings.Contains(err.Error(), "does not support this service") ||
		!strings.Contains(logs.String(), `"music_seek: Home Assistant refused the seek" player=media_player.show_five position=100`) {
		t.Errorf("refused: %v\n%s", err, logs.String())
	}
	ha = &fakeHA{readError: errors.New("not connected")}
	if _, err := askSeek(t, seekTool(ha, house(), &logs), "", `{"seconds": 30}`); err == nil ||
		!strings.Contains(err.Error(), "not connected") || !strings.Contains(logs.String(), "music_seek: could not read") {
		t.Errorf("read: %v", err)
	}
}
