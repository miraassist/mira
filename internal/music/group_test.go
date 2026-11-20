package music_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/music"
	"github.com/miraassist/mira/internal/tools"
)

func groupTool(t *testing.T, ha *fakeHA, players []music.Player, logs *bytes.Buffer) tools.InternalTool {
	t.Helper()
	return music.GroupTool(options(ha, players, logs))
}

func group(t *testing.T, tl tools.InternalTool, area, add, remove string) (string, error) {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"add": add, "remove": remove})
	return tl.Run(context.Background(), tools.Invocation{Name: tl.Name, Args: args, Area: area})
}

// MU-10 music_group is there only with Music Assistant players
func TestMU10_MusicGroupIsThereOnlyWithMusicAssistantPlayers(t *testing.T) {
	var logs bytes.Buffer
	tl := groupTool(t, &fakeHA{}, house(), &logs)
	if tl.Name != "music_group" || !tl.Enabled() || tl.ReadOnly {
		t.Errorf("with players: %+v", tl)
	}
	for _, example := range []string{"Spiel das auch im Bad", "Mach die Musik überall an", "Nimm das Bad wieder raus"} {
		if !strings.Contains(tl.Description, example) {
			t.Errorf("description lacks %q", example)
		}
	}
	if groupTool(t, &fakeHA{}, nil, &logs).Enabled() {
		t.Error("offered without a Music Assistant player")
	}
}

// MU-11 A room is added to the player that plays
func TestMU11_ARoomIsAddedToThePlayerThatPlays(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	out, err := group(t, groupTool(t, ha, house(), &logs), "", "Bad", "")
	if err != nil || len(ha.joins) != 1 || ha.joins[0] != "media_player.show_five <- media_player.dot_2" {
		t.Fatalf("%q %v %v", out, err, ha.joins)
	}
	if !strings.Contains(out, "Dot 2 (Bad unten) spielt jetzt dieselbe Musik wie Show Five (Küche)") {
		t.Errorf("answer %q", out)
	}
	if !strings.Contains(logs.String(), `msg="music joined" leader=media_player.show_five`) {
		t.Errorf("log %s", logs.String())
	}
	// Two play: the one in the turn's area leads.
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "playing"}}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "kueche", "Bad", ""); err != nil || len(ha.joins) != 1 ||
		ha.joins[0] != "media_player.show_five <- media_player.dot_2" {
		t.Errorf("%v %v", err, ha.joins)
	}
	// Several rooms in one go, one join.
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "", "Bad, CarPlay", ""); err != nil || len(ha.joins) != 1 ||
		ha.joins[0] != "media_player.show_five <- media_player.dot_2,media_player.carplay" {
		t.Errorf("%v %v", err, ha.joins)
	}
}

// MU-12 "Überall" adds every other player
func TestMU12_UeberallAddsEveryOtherPlayer(t *testing.T) {
	for _, word := range []string{"überall", "alle", "Everywhere"} {
		var logs bytes.Buffer
		ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
		if _, err := group(t, groupTool(t, ha, house(), &logs), "", word, ""); err != nil || len(ha.joins) != 1 ||
			ha.joins[0] != "media_player.show_five <- media_player.onkyo,media_player.sonos,media_player.dot_2,media_player.carplay" {
			t.Errorf("%s: %v %v", word, err, ha.joins)
		}
	}
}

// MU-13 A room with two players adds both; who is in the group already is left out
func TestMU13_ARoomWithTwoPlayersAddsBoth(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "", "Wohnzimmer", ""); err != nil || len(ha.joins) != 1 ||
		ha.joins[0] != "media_player.show_five <- media_player.onkyo,media_player.sonos" {
		t.Errorf("%v %v", err, ha.joins)
	}
	ha = &fakeHA{
		states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "playing"},
		groups: map[string][]string{
			"media_player.show_five": {"media_player.show_five", "media_player.onkyo"},
			"media_player.onkyo":     {"media_player.show_five", "media_player.onkyo"},
		},
	}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "kueche", "Wohnzimmer", ""); err != nil || len(ha.joins) != 1 ||
		ha.joins[0] != "media_player.show_five <- media_player.sonos" {
		t.Errorf("one in already: %v %v", err, ha.joins)
	}
	ha.joins = nil
	out, err := group(t, groupTool(t, ha, house(), &logs), "kueche", "Onkyo", "")
	if err != nil || len(ha.joins) != 0 || !strings.Contains(out, "Onkyo (Wohnzimmer) spielt schon mit Show Five (Küche)") {
		t.Errorf("all in already: %q %v %v", out, err, ha.joins)
	}
}

// MU-14 A player is taken out with unjoin; one not in a group is named
func TestMU14_APlayerIsTakenOutWithUnjoin(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{
		states: map[string]string{"media_player.show_five": "playing", "media_player.dot_2": "playing"},
		groups: map[string][]string{
			"media_player.show_five": {"media_player.show_five", "media_player.dot_2"},
			"media_player.dot_2":     {"media_player.show_five", "media_player.dot_2"},
		},
	}
	out, err := group(t, groupTool(t, ha, house(), &logs), "", "", "Bad, Onkyo")
	if err != nil || len(ha.unjoins) != 1 || ha.unjoins[0] != "media_player.dot_2" {
		t.Fatalf("%q %v %v", out, err, ha.unjoins)
	}
	if !strings.Contains(out, "Dot 2 (Bad unten) ist aus der Gruppe heraus") || !strings.Contains(out, "Onkyo (Wohnzimmer) spielt in keiner Gruppe") {
		t.Errorf("answer %q", out)
	}
	if !strings.Contains(logs.String(), `msg="music unjoined" player=media_player.dot_2`) {
		t.Errorf("log %s", logs.String())
	}
}

// MU-15 The player that plays can be taken out
func TestMU15_ThePlayerThatPlaysCanBeTakenOut(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{
		states: map[string]string{"media_player.show_five": "playing", "media_player.dot_2": "playing"},
		groups: map[string][]string{
			"media_player.show_five": {"media_player.show_five", "media_player.dot_2"},
			"media_player.dot_2":     {"media_player.show_five", "media_player.dot_2"},
		},
	}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "", "", "Küche"); err != nil || len(ha.unjoins) != 1 ||
		ha.unjoins[0] != "media_player.show_five" {
		t.Errorf("%v %v", err, ha.unjoins)
	}
}

// MU-16 Unknown rooms and no music say so
func TestMU16_UnknownRoomsAndNoMusicSaySo(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{}}
	if out, _ := group(t, groupTool(t, ha, house(), &logs), "", "Bad", ""); !strings.Contains(out, "läuft auf keinem") || len(ha.joins) != 0 {
		t.Errorf("none plays: %q", out)
	}
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	if out, _ := group(t, groupTool(t, ha, house(), &logs), "", "Bad, Garage", ""); !strings.Contains(out, "„Garage“ ist weder") || len(ha.joins) != 0 {
		t.Errorf("unknown: %q %v", out, ha.joins)
	}
	if out, _ := group(t, groupTool(t, ha, house(), &logs), "", "", "Garage"); !strings.Contains(out, "„Garage“ ist weder") || len(ha.unjoins) != 0 {
		t.Errorf("unknown remove: %q", out)
	}
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "playing"}}
	if out, _ := group(t, groupTool(t, ha, house(), &logs), "bad", "Bad", ""); !strings.Contains(out, "Onkyo (Wohnzimmer)") ||
		!strings.Contains(out, "Show Five (Küche)") || len(ha.joins) != 0 {
		t.Errorf("several play: %q", out)
	}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "", "", ""); err == nil {
		t.Error("neither add nor remove is no error")
	}
}

// MU-17 What Home Assistant refuses is the tool's error
func TestMU17_WhatHomeAssistantRefusesIsTheToolsError(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}, refuse: errors.New("cannot group these players")}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "", "Bad", ""); err == nil || !strings.Contains(err.Error(), "cannot group these players") ||
		!strings.Contains(logs.String(), "Home Assistant refused the join") {
		t.Errorf("join: %v\n%s", err, logs.String())
	}
	ha.groups = map[string][]string{"media_player.dot_2": {"media_player.show_five", "media_player.dot_2"}}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "", "", "Bad"); err == nil || !strings.Contains(err.Error(), "cannot group these players") ||
		!strings.Contains(logs.String(), "Home Assistant refused the unjoin") {
		t.Errorf("unjoin: %v\n%s", err, logs.String())
	}
	ha = &fakeHA{readError: errors.New("not connected")}
	if _, err := group(t, groupTool(t, ha, house(), &logs), "", "Bad", ""); err == nil || !strings.Contains(err.Error(), "not connected") ||
		!strings.Contains(logs.String(), "music_group: could not read") {
		t.Errorf("read: %v", err)
	}
}
