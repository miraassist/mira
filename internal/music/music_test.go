package music_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/music"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/radio"
	"github.com/miraassist/mira/internal/tools"
)

// The house of the tests: the living room with two Music Assistant
// players, the kitchen and the bathroom with one each, CarPlay without an
// area.
func house() []music.Player {
	return []music.Player{
		{Entity: "media_player.onkyo", Name: "Onkyo", Names: []string{"onkyo"}, Area: "wohnzimmer", AreaName: "Wohnzimmer"},
		{Entity: "media_player.sonos", Name: "Sonos", Names: []string{"sonos"}, Area: "wohnzimmer", AreaName: "Wohnzimmer"},
		{Entity: "media_player.show_five", Name: "Show Five", Names: []string{"show five"}, Area: "kueche", AreaName: "Küche"},
		{Entity: "media_player.dot_2", Name: "Dot 2", Names: []string{"dot 2"}, Area: "bad", AreaName: "Bad unten"},
		{Entity: "media_player.carplay", Name: "CarPlay", Names: []string{"carplay"}},
	}
}

var areas = map[string]string{"wohnzimmer": "wohnzimmer", "küche": "kueche", "kueche": "kueche", "bad": "bad", "bad unten": "bad"}

type fakeHA struct {
	states    map[string]string
	groups    map[string][]string // group_members per player
	moves     []string
	joins     []string // "leader <- a,b"
	unjoins   []string
	contents  map[string]string      // media_content_id per player
	queued    []string               // "player <- id type enqueue"
	more      []string               // "player <- seed" (radio_mode, enqueue add)
	places    map[string]music.State // the place in the song per player: Position, PositionKnown, PositionAt, Duration
	seeks     []string               // "player -> position"
	read      [][]string             // the players of each States read
	refuse    error
	readError error
}

func options(ha *fakeHA, players []music.Player, logs *bytes.Buffer) music.Options {
	return music.Options{Lang: i18n.DE, Logger: slog.New(slog.NewTextHandler(logs, nil)),
		Players: func() []music.Player { return players },
		Area:    func(name string) string { return areas[name] },
		States: func(_ context.Context, ids []string) (map[string]music.State, error) {
			ha.read = append(ha.read, ids)
			if ha.readError != nil {
				return nil, ha.readError
			}
			out := map[string]music.State{}
			for _, id := range ids {
				p := ha.places[id]
				out[id] = music.State{State: ha.states[id], Members: ha.groups[id], Content: ha.contents[id],
					Position: p.Position, PositionKnown: p.PositionKnown, PositionAt: p.PositionAt, Duration: p.Duration}
			}
			return out, nil
		},
		Transfer: func(_ context.Context, target, source string) error {
			if ha.refuse != nil {
				return ha.refuse
			}
			ha.moves = append(ha.moves, source+" -> "+target)
			return nil
		},
		Join: func(_ context.Context, leader string, members []string) error {
			if ha.refuse != nil {
				return ha.refuse
			}
			ha.joins = append(ha.joins, leader+" <- "+strings.Join(members, ","))
			return nil
		},
		Unjoin: func(_ context.Context, player string) error {
			if ha.refuse != nil {
				return ha.refuse
			}
			ha.unjoins = append(ha.unjoins, player)
			return nil
		},
		Queue: func(_ context.Context, player, id, mediaType, enqueue string) error {
			if ha.refuse != nil {
				return ha.refuse
			}
			ha.queued = append(ha.queued, player+" <- "+id+" "+mediaType+" "+enqueue)
			return nil
		},
		More: func(_ context.Context, player, seed string) error {
			if ha.refuse != nil {
				return ha.refuse
			}
			ha.more = append(ha.more, player+" <- "+seed)
			return nil
		},
		Seek: func(_ context.Context, player string, position float64) error {
			if ha.refuse != nil {
				return ha.refuse
			}
			ha.seeks = append(ha.seeks, fmt.Sprintf("%s -> %g", player, position))
			return nil
		},
		Now: func() time.Time { return testNow },
	}
}

func tool(t *testing.T, ha *fakeHA, players []music.Player, logs *bytes.Buffer) tools.InternalTool {
	t.Helper()
	return music.Tool(options(ha, players, logs))
}

func call(t *testing.T, tl tools.InternalTool, area, to, from string) (string, error) {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"to": to, "from": from})
	return tl.Run(context.Background(), tools.Invocation{Name: tl.Name, Args: args, Area: area})
}

// MU-01 The exposed Music Assistant players are in the prefix, per area
func TestMU01_TheExposedMusicAssistantPlayersAreInThePrefixPerArea(t *testing.T) {
	section := music.Section(i18n.DE, house())
	want := "## Musik (Music Assistant)\nMusik und Radiosender spielst du mit media_player__search_media und media_player__play_media nur auf diesen Playern, " +
		"mit ihrem Namen; andere Lautsprecher können keine Musik aus Music Assistant spielen.\n" +
		"- Bad unten: Dot 2\n- Küche: Show Five\n- Wohnzimmer: Onkyo, Sonos\n- ohne Bereich: CarPlay"
	if section != want {
		t.Fatalf("section:\n%s\nwant:\n%s", section, want)
	}
	sys := prompt.Prefix{Lang: i18n.DE, Agent: "Regeln", Structure: "Das Haus", Music: section, Profile: "Profil"}.SystemText()
	if !(strings.Index(sys, "Das Haus") < strings.Index(sys, "## Musik") && strings.Index(sys, "## Musik") < strings.Index(sys, "Profil")) {
		t.Errorf("order:\n%s", sys)
	}
	// The same players in another order give the same text: the prefix stays.
	rev := house()
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	if music.Section(i18n.DE, rev) != section {
		t.Error("the order of the players changes the section")
	}
}

// MU-02 Other players are not named
func TestMU02_OtherPlayersAreNotNamed(t *testing.T) {
	snap := structure.Build(structure.Registry{
		Areas: []haws.Area{{AreaID: "kueche", Name: "Küche"}},
		Entities: []haws.Entity{
			{EntityID: "media_player.show_five", Platform: "music_assistant", AreaID: "kueche"},
			{EntityID: "media_player.show_kueche_speaker", Platform: "esphome", AreaID: "kueche"},
			{EntityID: "media_player.versteckt", Platform: "music_assistant", AreaID: "kueche"},
			{EntityID: "media_player.aus", Platform: "music_assistant", AreaID: "kueche", DisabledBy: "user"},
		},
		Exposed: map[string]bool{"media_player.show_five": true, "media_player.show_kueche_speaker": true, "media_player.aus": true},
	})
	if len(snap.MusicPlayers) != 1 || !snap.MusicPlayers["media_player.show_five"] {
		t.Errorf("music players %v", snap.MusicPlayers)
	}
}

// MU-03 Without Music Assistant players there is no section
func TestMU03_WithoutMusicAssistantPlayersThereIsNoSection(t *testing.T) {
	if s := music.Section(i18n.DE, nil); s != "" {
		t.Errorf("section %q", s)
	}
	if sys := (prompt.Prefix{Lang: i18n.DE, Agent: "Regeln", Structure: "Das Haus"}).SystemText(); strings.Contains(sys, "Musik") {
		t.Errorf("system:\n%s", sys)
	}
}

// MU-04 The section changes only with the players
func TestMU04_TheSectionChangesOnlyWithThePlayers(t *testing.T) {
	reg := func(name string) structure.Registry {
		return structure.Registry{
			Areas:    []haws.Area{{AreaID: "kueche", Name: "Küche"}},
			Entities: []haws.Entity{{EntityID: "media_player.show_five", Platform: "music_assistant", AreaID: "kueche", Name: name}},
			Exposed:  map[string]bool{"media_player.show_five": true},
		}
	}
	a, b := structure.Build(reg("Küche")), structure.Build(reg("Küche"))
	if !structure.Same(a, b) {
		t.Error("the same registries differ")
	}
	moved := reg("Küche")
	moved.Entities[0].Platform = "esphome"
	if structure.Same(a, structure.Build(moved)) {
		t.Error("a player that is no longer Music Assistant's does not change the structure")
	}
	if structure.Same(a, structure.Build(reg("Küchenradio"))) {
		t.Error("a renamed player does not change the structure")
	}
}

// MU-05 music_move is there only with Music Assistant players
func TestMU05_MusicMoveIsThereOnlyWithMusicAssistantPlayers(t *testing.T) {
	var logs bytes.Buffer
	if tl := tool(t, &fakeHA{}, house(), &logs); tl.Name != "music_move" || !tl.Enabled() || tl.ReadOnly {
		t.Errorf("with players: %+v", tl)
	}
	if tool(t, &fakeHA{}, nil, &logs).Enabled() {
		t.Error("offered without a Music Assistant player")
	}
}

// MU-06 The source is the player that plays
func TestMU06_TheSourceIsThePlayerThatPlays(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "idle"}}
	if _, err := call(t, tool(t, ha, house(), &logs), "", "Bad", ""); err != nil || len(ha.moves) != 1 ||
		ha.moves[0] != "media_player.show_five -> media_player.dot_2" {
		t.Fatalf("%v %v", err, ha.moves)
	}
	// Two play: the one in the area of the turn's satellite.
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing", "media_player.onkyo": "playing"}}
	if _, err := call(t, tool(t, ha, house(), &logs), "kueche", "Bad", ""); err != nil || len(ha.moves) != 1 ||
		ha.moves[0] != "media_player.show_five -> media_player.dot_2" {
		t.Fatalf("%v %v", err, ha.moves)
	}
	// Neither in the turn's area: nothing moves, the answer names both.
	out, err := call(t, tool(t, ha, house(), &logs), "bad", "Bad", "")
	if err != nil || len(ha.moves) != 1 || !strings.Contains(out, "Onkyo (Wohnzimmer)") || !strings.Contains(out, "Show Five (Küche)") {
		t.Errorf("%q %v %v", out, err, ha.moves)
	}
	// Only paused: that one.
	ha = &fakeHA{states: map[string]string{"media_player.onkyo": "paused"}}
	if _, err := call(t, tool(t, ha, house(), &logs), "", "Küche", ""); err != nil || len(ha.moves) != 1 ||
		ha.moves[0] != "media_player.onkyo -> media_player.show_five" {
		t.Errorf("%v %v", err, ha.moves)
	}
}

// MU-07 The target is a room with one player, or a player by name
func TestMU07_TheTargetIsARoomWithOnePlayerOrAPlayerByName(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	tl := tool(t, ha, house(), &logs)
	out, _ := call(t, tl, "", "Wohnzimmer", "")
	if len(ha.moves) != 0 || !strings.Contains(out, "mehrere Music-Assistant-Player") || !strings.Contains(out, "Onkyo") {
		t.Errorf("%q %v", out, ha.moves)
	}
	if _, err := call(t, tl, "", "Onkyo", ""); err != nil || ha.moves[0] != "media_player.show_five -> media_player.onkyo" {
		t.Errorf("%v %v", err, ha.moves)
	}
	if out, _ := call(t, tl, "", "Garage", ""); !strings.Contains(out, "„Garage“ ist weder") {
		t.Errorf("%q", out)
	}
	// A player named like its area: the name wins over the area.
	named := append(house(), music.Player{Entity: "media_player.wohnzimmer", Name: "Wohnzimmer", Names: []string{"wohnzimmer"},
		Area: "wohnzimmer", AreaName: "Wohnzimmer"})
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	if _, err := call(t, tool(t, ha, named, &logs), "", "Wohnzimmer", ""); err != nil || len(ha.moves) != 1 ||
		ha.moves[0] != "media_player.show_five -> media_player.wohnzimmer" {
		t.Errorf("named like the area: %v %v", err, ha.moves)
	}
}

// MU-08 The move is Music Assistant's transfer_queue
func TestMU08_TheMoveIsMusicAssistantsTransferQueue(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{"media_player.show_five": "playing"}}
	out, err := call(t, tool(t, ha, house(), &logs), "", "Bad", "")
	if err != nil || !strings.Contains(out, "auf Dot 2 (Bad unten) weiter") || !strings.Contains(out, "auf Show Five (Küche) hat sie aufgehört") {
		t.Errorf("%q %v", out, err)
	}
	if !strings.Contains(logs.String(), `msg="music moved" from=media_player.show_five to=media_player.dot_2`) {
		t.Errorf("log %s", logs.String())
	}
}

// MU-09 What cannot be moved says why
func TestMU09_WhatCannotBeMovedSaysWhy(t *testing.T) {
	var logs bytes.Buffer
	ha := &fakeHA{states: map[string]string{}}
	if out, _ := call(t, tool(t, ha, house(), &logs), "", "Bad", ""); !strings.Contains(out, "läuft auf keinem") {
		t.Errorf("none plays: %q", out)
	}
	ha = &fakeHA{states: map[string]string{"media_player.dot_2": "playing"}}
	if out, _ := call(t, tool(t, ha, house(), &logs), "", "Bad", ""); !strings.Contains(out, "läuft schon auf Dot 2") || len(ha.moves) != 0 {
		t.Errorf("already there: %q", out)
	}
	ha = &fakeHA{states: map[string]string{"media_player.show_five": "playing"}, refuse: errors.New("player unavailable")}
	if _, err := call(t, tool(t, ha, house(), &logs), "", "Bad", ""); err == nil || !strings.Contains(err.Error(), "player unavailable") ||
		!strings.Contains(logs.String(), "Home Assistant refused the move") {
		t.Errorf("refused: %v\n%s", err, logs.String())
	}
	ha = &fakeHA{readError: errors.New("not connected")}
	if _, err := call(t, tool(t, ha, house(), &logs), "", "Bad", ""); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("read: %v", err)
	}
}

// MU-18 radio_play is there only without Music Assistant players
func TestMU18_RadioPlayIsThereOnlyWithoutMusicAssistantPlayers(t *testing.T) {
	radioTool := radio.Tool(radio.Options{Lang: i18n.DE})
	with := music.OnlyWithout(radioTool, func() []music.Player { return house() })
	if with.Name != "radio_play" || with.Enabled() {
		t.Errorf("radio_play offered beside Music Assistant players: %+v", with)
	}
	if !music.OnlyWithout(radioTool, func() []music.Player { return nil }).Enabled() {
		t.Error("radio_play not offered in a house without Music Assistant")
	}
}

// MU-19 The section says that radio stations play on these players too
func TestMU19_TheSectionSaysThatRadioStationsPlayOnThesePlayersToo(t *testing.T) {
	for l, want := range map[i18n.Lang]string{i18n.DE: "Musik und Radiosender spielst du", i18n.EN: "Play music and radio stations"} {
		if got := music.Section(l, house()); !strings.Contains(got, want) {
			t.Errorf("%s: section without %q:\n%s", l, want, got)
		}
	}
}
