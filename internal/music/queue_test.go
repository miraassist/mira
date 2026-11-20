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

const hit = "deezer--Wws56Gqn://track/4091937401"

func queueTool(t *testing.T, ha *fakeHA, players []music.Player, logs *bytes.Buffer) tools.InternalTool {
	t.Helper()
	return music.QueueTool(options(ha, players, logs))
}

func enqueue(t *testing.T, tl tools.InternalTool, area string, args map[string]string) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return tl.Run(context.Background(), tools.Invocation{Name: tl.Name, Args: raw, Area: area})
}

// MU-20 music_queue is there only with Music Assistant players
func TestMU20_MusicQueueIsThereOnlyWithMusicAssistantPlayers(t *testing.T) {
	var logs bytes.Buffer
	tl := queueTool(t, &fakeHA{}, house(), &logs)
	if tl.Name != "music_queue" || !tl.Enabled() || tl.ReadOnly {
		t.Errorf("with players: %+v", tl)
	}
	for _, example := range []string{"Spiel danach Bohemian Rhapsody", "Als Nächstes", "Setz das auf die Liste", "hinten an", "Reih",
		"media_player__search_media", "media_player__play_media"} {
		if !strings.Contains(tl.Description, example) {
			t.Errorf("description lacks %q", example)
		}
	}
	o := options(&fakeHA{}, house(), &logs)
	o.Lang = i18n.EN
	if en := music.QueueTool(o).Description; !strings.Contains(en, "Play Bohemian Rhapsody after this") || !strings.Contains(en, "Put this on the list") {
		t.Errorf("English description %q", en)
	}
	if !strings.Contains(tl.Schema, `"required":["media_content_id"]`) || !strings.Contains(tl.Schema, `"enum":["next","end"]`) {
		t.Errorf("schema %s", tl.Schema)
	}
	if queueTool(t, &fakeHA{}, nil, &logs).Enabled() {
		t.Error("offered without a Music Assistant player")
	}
}

// MU-21 "next" and "end" are Music Assistant's enqueue next and add on the player that plays
func TestMU21_NextAndEndAreMusicAssistantsEnqueueOnThePlayerThatPlays(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	tl := queueTool(t, ha, house(), &logs)
	out, err := enqueue(t, tl, "", map[string]string{"media_content_id": hit, "media_content_type": "track", "when": "next"})
	if err != nil || len(ha.queued) != 1 || ha.queued[0] != "media_player.show_five <- "+hit+" track next" {
		t.Fatalf("%q %v %v", out, err, ha.queued)
	}
	if !strings.Contains(out, "eingereiht auf Show Five (Küche), als Nächstes") {
		t.Errorf("answer %q", out)
	}
	if !strings.Contains(logs.String(), `msg="music queued" player=media_player.show_five media_id=`+hit+" enqueue=next") {
		t.Errorf("log %s", logs.String())
	}
	// One read, filtered to the Music Assistant players.
	if len(ha.read) != 1 || len(ha.read[0]) != len(house()) {
		t.Errorf("reads %v", ha.read)
	}
	for _, when := range []string{"end", ""} {
		ha.queued = nil
		out, err = enqueue(t, tl, "", map[string]string{"media_content_id": hit, "when": when})
		if err != nil || len(ha.queued) != 1 || ha.queued[0] != "media_player.show_five <- "+hit+"  add" ||
			!strings.Contains(out, "ans Ende der Warteschlange") {
			t.Errorf("when %q: %q %v %v", when, out, err, ha.queued)
		}
	}
	// A paused player has a queue too.
	ha = &fakeHA{states: map[string]string{"media_player.dot_2": "paused"}}
	if _, err := enqueue(t, queueTool(t, ha, house(), &logs), "", map[string]string{"media_content_id": hit}); err != nil ||
		len(ha.queued) != 1 || !strings.HasPrefix(ha.queued[0], "media_player.dot_2 <- ") {
		t.Errorf("paused: %v %v", err, ha.queued)
	}
}

// MU-22 With music in several rooms the turn's room wins, else the tool names them
func TestMU22_WithMusicInSeveralRoomsTheTurnsRoomWins(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "playing"}}
	tl := queueTool(t, ha, house(), &logs)
	if _, err := enqueue(t, tl, "kueche", map[string]string{"media_content_id": hit}); err != nil || len(ha.queued) != 1 ||
		!strings.HasPrefix(ha.queued[0], "media_player.show_five <- ") {
		t.Errorf("turn's room: %v %v", err, ha.queued)
	}
	ha.queued = nil
	out, err := enqueue(t, tl, "bad", map[string]string{"media_content_id": hit})
	if err != nil || len(ha.queued) != 0 || !strings.Contains(out, "Onkyo (Wohnzimmer)") || !strings.Contains(out, "Show Five (Küche)") ||
		!strings.Contains(out, "music_queue dann mit player") {
		t.Errorf("several: %q %v %v", out, err, ha.queued)
	}
}

// MU-23 A room or a player the user names is where it is queued
func TestMU23_ARoomOrAPlayerTheUserNamesIsWhereItIsQueued(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "playing"}}
	tl := queueTool(t, ha, house(), &logs)
	if _, err := enqueue(t, tl, "kueche", map[string]string{"media_content_id": hit, "player": "Wohnzimmer"}); err != nil ||
		len(ha.queued) != 1 || !strings.HasPrefix(ha.queued[0], "media_player.onkyo <- ") {
		t.Errorf("room: %v %v", err, ha.queued)
	}
	if !slices.Equal(ha.read[0], []string{"media_player.onkyo", "media_player.sonos"}) {
		t.Errorf("read %v", ha.read)
	}
	ha.queued = nil
	if _, err := enqueue(t, tl, "kueche", map[string]string{"media_content_id": hit, "player": "Onkyo"}); err != nil ||
		len(ha.queued) != 1 || !strings.HasPrefix(ha.queued[0], "media_player.onkyo <- ") {
		t.Errorf("name: %v %v", err, ha.queued)
	}
	ha.queued = nil
	out, err := enqueue(t, tl, "", map[string]string{"media_content_id": hit, "player": "Garage"})
	if err != nil || len(ha.queued) != 0 || !strings.Contains(out, "„Garage“ ist weder") {
		t.Errorf("unknown: %q %v %v", out, err, ha.queued)
	}
}

// MU-24 When nothing plays, the model is told to play the item instead
func TestMU24_WhenNothingPlaysTheModelIsToldToPlayTheItemInstead(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.onkyo": "playing"}}
	tl := queueTool(t, ha, house(), &logs)
	// A room named where nothing plays: no queue there.
	if out, err := enqueue(t, tl, "", map[string]string{"media_content_id": hit, "player": "Bad"}); err != nil || len(ha.queued) != 0 ||
		!strings.Contains(out, "keine Musik") || !strings.Contains(out, "media_player__play_media") {
		t.Errorf("named: %q %v %v", out, err, ha.queued)
	}
	ha = &fakeHA{states: map[string]string{}}
	out, err := enqueue(t, queueTool(t, ha, house(), &logs), "", map[string]string{"media_content_id": hit})
	if err != nil || len(ha.queued) != 0 || !strings.Contains(out, "media_player__play_media") {
		t.Errorf("none: %q %v %v", out, err, ha.queued)
	}
}

// MU-25 After a radio station the item is queued all the same, and the answer says it comes on "weiter"
func TestMU25_AfterARadioStationTheItemIsQueuedAndTheAnswerSaysItComesOnWeiter(t *testing.T) {
	for _, station := range []string{"library://radio/1", "radiobrowser://radio/ce4b8451-0601-11e8-ae97-52543be04c81"} {
		var logs bytes.Buffer
		ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"},
			contents: map[string]string{"media_player.show_five": station}}
		out, err := enqueue(t, queueTool(t, ha, house(), &logs), "", map[string]string{"media_content_id": hit, "when": "next"})
		if err != nil || len(ha.queued) != 1 || !strings.Contains(out, "Radiosender") || !strings.Contains(out, "„weiter“") {
			t.Errorf("%s: %q %v %v", station, out, err, ha.queued)
		}
		if !strings.Contains(logs.String(), "after_station=true") {
			t.Errorf("log %s", logs.String())
		}
	}
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"},
		contents: map[string]string{"media_player.show_five": hit}}
	if out, _ := enqueue(t, queueTool(t, ha, house(), &logs), "", map[string]string{"media_content_id": hit}); strings.Contains(out, "Radiosender") {
		t.Errorf("a song is no station: %q", out)
	}
}

// MU-26 What fails is the tool's error and names its input
func TestMU26_WhatFailsIsTheToolsErrorAndNamesItsInput(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}, refuse: errors.New("media not found")}
	_, err := enqueue(t, queueTool(t, ha, house(), &logs), "", map[string]string{"media_content_id": hit, "when": "next"})
	if err == nil || !strings.Contains(err.Error(), "media not found") ||
		!strings.Contains(logs.String(), `"music_queue: Home Assistant refused to queue" player=media_player.show_five media_id=`+hit+" enqueue=next") {
		t.Errorf("refused: %v\n%s", err, logs.String())
	}
	ha = &fakeHA{readError: errors.New("not connected")}
	if _, err := enqueue(t, queueTool(t, ha, house(), &logs), "", map[string]string{"media_content_id": hit}); err == nil ||
		!strings.Contains(err.Error(), "not connected") || !strings.Contains(logs.String(), "music_queue: could not read") {
		t.Errorf("read: %v", err)
	}
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	tl := queueTool(t, ha, house(), &logs)
	if _, err := enqueue(t, tl, "", map[string]string{"when": "next"}); err == nil || !strings.Contains(err.Error(), `{\"when\":\"next\"}`) {
		t.Errorf("no id: %v", err)
	}
	if _, err := enqueue(t, tl, "", map[string]string{"media_content_id": hit, "when": "later"}); err == nil || !strings.Contains(err.Error(), "later") {
		t.Errorf("bad when: %v", err)
	}
	if len(ha.queued) != 0 {
		t.Errorf("queued %v", ha.queued)
	}
}
