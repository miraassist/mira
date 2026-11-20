package music_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/music"
	"github.com/miraassist/mira/internal/tools"
)

// playing is the URI of the song the tests' players play.
const playing = "deezer--Wws56Gqn://track/1109731"

func moreTool(ha *fakeHA, players []music.Player, logs *bytes.Buffer) tools.InternalTool {
	return music.MoreTool(options(ha, players, logs))
}

func askMore(t *testing.T, tl tools.InternalTool, area string, args map[string]string) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return tl.Run(context.Background(), tools.Invocation{Name: tl.Name, Args: raw, Area: area})
}

// MU-27 music_more is there only with Music Assistant players
func TestMU27_MusicMoreIsThereOnlyWithMusicAssistantPlayers(t *testing.T) {
	var logs bytes.Buffer
	tl := moreTool(&fakeHA{}, house(), &logs)
	if tl.Name != "music_more" || !tl.Enabled() || tl.ReadOnly {
		t.Errorf("with players: %+v", tl)
	}
	for _, example := range []string{"Spiel mehr davon", "Mehr in der Art", "Spiel Ähnliches", "Spiel was wie Queen",
		"media_player__search_media", "nach dem laufenden Stück"} {
		if !strings.Contains(tl.Description, example) {
			t.Errorf("description lacks %q", example)
		}
	}
	o := options(&fakeHA{}, house(), &logs)
	o.Lang = i18n.EN
	en := music.MoreTool(o).Description
	for _, example := range []string{"Play more of this", "More like this", "Play something similar", "Play something like Queen"} {
		if !strings.Contains(en, example) {
			t.Errorf("English description lacks %q: %q", example, en)
		}
	}
	if strings.Contains(tl.Schema, "required") || !strings.Contains(tl.Schema, `"media_content_id"`) || !strings.Contains(tl.Schema, `"player"`) {
		t.Errorf("schema %s", tl.Schema)
	}
	if moreTool(&fakeHA{}, nil, &logs).Enabled() {
		t.Error("offered without a Music Assistant player")
	}
}

// MU-28 Without a hit, the seed is the item that plays, appended in radio mode
func TestMU28_WithoutAHitTheSeedIsTheItemThatPlays(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"},
		contents: map[string]string{"media_player.show_five": playing}}
	out, err := askMore(t, moreTool(ha, house(), &logs), "", map[string]string{})
	if err != nil || !slices.Equal(ha.more, []string{"media_player.show_five <- " + playing}) {
		t.Fatalf("%q %v %v", out, err, ha.more)
	}
	if !strings.Contains(out, "Auf Show Five (Küche) folgt nach dem laufenden Stück ähnliche Musik") {
		t.Errorf("answer %q", out)
	}
	if !strings.Contains(logs.String(), `msg="similar music appended" player=media_player.show_five seed=`+playing+" from_hit=false") {
		t.Errorf("log %s", logs.String())
	}
	// One read, filtered to the Music Assistant players.
	if len(ha.read) != 1 || len(ha.read[0]) != len(house()) {
		t.Errorf("reads %v", ha.read)
	}
	// Called with no arguments at all, too; a paused player has a queue as well.
	ha = &fakeHA{states: map[string]string{"media_player.dot_2": "paused"}, contents: map[string]string{"media_player.dot_2": playing}}
	tl := moreTool(ha, house(), &logs)
	if _, err := tl.Run(context.Background(), tools.Invocation{Name: tl.Name}); err != nil ||
		!slices.Equal(ha.more, []string{"media_player.dot_2 <- " + playing}) {
		t.Errorf("paused, no arguments: %v %v", err, ha.more)
	}
}

// MU-29 With a hit, the hit is the seed, after a station too
func TestMU29_WithAHitTheHitIsTheSeed(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"},
		contents: map[string]string{"media_player.show_five": playing}}
	out, err := askMore(t, moreTool(ha, house(), &logs), "", map[string]string{"media_content_id": hit})
	if err != nil || !slices.Equal(ha.more, []string{"media_player.show_five <- " + hit}) ||
		!strings.Contains(out, "Musik in der Art des Treffers") || strings.Contains(out, "Radiosender") {
		t.Errorf("%q %v %v", out, err, ha.more)
	}
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing"},
		contents: map[string]string{"media_player.show_five": "library://radio/1"}}
	out, err = askMore(t, moreTool(ha, house(), &logs), "", map[string]string{"media_content_id": hit})
	if err != nil || len(ha.more) != 1 || !strings.Contains(out, "Radiosender") || !strings.Contains(out, "„weiter“") {
		t.Errorf("after a station: %q %v %v", out, err, ha.more)
	}
}

// MU-30 The player is found as music_queue's: the turn's room, a room or player named, else the tool names them
func TestMU30_ThePlayerIsFoundAsMusicQueues(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "playing"},
		contents: map[string]string{"media_player.show_five": playing, "media_player.onkyo": hit}}
	tl := moreTool(ha, house(), &logs)
	if _, err := askMore(t, tl, "kueche", map[string]string{}); err != nil ||
		!slices.Equal(ha.more, []string{"media_player.show_five <- " + playing}) {
		t.Errorf("turn's room: %v %v", err, ha.more)
	}
	ha.more = nil
	out, err := askMore(t, tl, "bad", map[string]string{})
	if err != nil || len(ha.more) != 0 || !strings.Contains(out, "Onkyo (Wohnzimmer)") || !strings.Contains(out, "Show Five (Küche)") ||
		!strings.Contains(out, "music_more dann mit player") {
		t.Errorf("several: %q %v %v", out, err, ha.more)
	}
	for _, named := range []string{"Wohnzimmer", "Onkyo"} {
		ha.more, ha.read = nil, nil
		if _, err := askMore(t, tl, "kueche", map[string]string{"player": named}); err != nil ||
			!slices.Equal(ha.more, []string{"media_player.onkyo <- " + hit}) {
			t.Errorf("%s: %v %v", named, err, ha.more)
		}
	}
	ha.more = nil
	out, err = askMore(t, tl, "", map[string]string{"player": "Garage"})
	if err != nil || len(ha.more) != 0 || !strings.Contains(out, "„Garage“ ist weder") {
		t.Errorf("unknown: %q %v %v", out, err, ha.more)
	}
}

// MU-31 When nothing plays, nothing is called; with a hit the model is told to play it
func TestMU31_WhenNothingPlaysNothingIsCalled(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{}}
	tl := moreTool(ha, house(), &logs)
	if out, err := askMore(t, tl, "", map[string]string{}); err != nil || !strings.Contains(out, "keine Musik") ||
		strings.Contains(out, "media_player__play_media") {
		t.Errorf("without a hit: %q %v", out, err)
	}
	if out, err := askMore(t, tl, "", map[string]string{"media_content_id": hit}); err != nil || !strings.Contains(out, "keine Musik") ||
		!strings.Contains(out, "media_player__play_media") {
		t.Errorf("with a hit: %q %v", out, err)
	}
	if len(ha.more) != 0 {
		t.Errorf("called %v", ha.more)
	}
}

// MU-32 A station or a player that does not say what it plays calls nothing without a hit
func TestMU32_AStationOrAPlayerWithoutContentCallsNothingWithoutAHit(t *testing.T) {
	for _, station := range []string{"library://radio/1", "radiobrowser://radio/ce4b8451-0601-11e8-ae97-52543be04c81"} {
		var logs bytes.Buffer
		ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"},
			contents: map[string]string{"media_player.show_five": station}}
		out, err := askMore(t, moreTool(ha, house(), &logs), "", map[string]string{})
		if err != nil || len(ha.more) != 0 || !strings.Contains(out, "Radiosender") || !strings.Contains(out, "nicht mit einem Sender") {
			t.Errorf("%s: %q %v %v", station, out, err, ha.more)
		}
	}
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	out, err := askMore(t, moreTool(ha, house(), &logs), "", map[string]string{})
	if err != nil || len(ha.more) != 0 || !strings.Contains(out, "sagt nicht, was gerade läuft") {
		t.Errorf("no content: %q %v %v", out, err, ha.more)
	}
	if !strings.Contains(logs.String(), `"music_more: the player that plays has no media_content_id; nothing appended" player=media_player.show_five`) {
		t.Errorf("log %s", logs.String())
	}
}

// MU-33 What fails is the tool's error and names its input
func TestMU33_WhatFailsIsTheToolsErrorAndNamesItsInput(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"},
		contents: map[string]string{"media_player.show_five": playing}, refuse: errors.New("media not found")}
	_, err := askMore(t, moreTool(ha, house(), &logs), "", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "media not found") ||
		!strings.Contains(logs.String(), `"music_more: Home Assistant refused similar music" player=media_player.show_five seed=`+playing) {
		t.Errorf("refused: %v\n%s", err, logs.String())
	}
	ha = &fakeHA{readError: errors.New("not connected")}
	if _, err := askMore(t, moreTool(ha, house(), &logs), "", map[string]string{}); err == nil ||
		!strings.Contains(err.Error(), "not connected") || !strings.Contains(logs.String(), "music_more: could not read") {
		t.Errorf("read: %v", err)
	}
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	tl := moreTool(ha, house(), &logs)
	if _, err := tl.Run(context.Background(), tools.Invocation{Name: tl.Name, Args: json.RawMessage(`["Queen"]`)}); err == nil ||
		!strings.Contains(err.Error(), `[\"Queen\"]`) {
		t.Errorf("no object: %v", err)
	}
	if len(ha.more) != 0 {
		t.Errorf("called %v", ha.more)
	}
}
