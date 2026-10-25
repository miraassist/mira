// Package radio is radio_play: Mira finds a station in Home Assistant's Radio
// Browser and starts it on the room's speaker (docs/spec/radio.md). The
// Assist tools of Home Assistant offer no search for players like the
// TECHO5, so Mira searches the media source herself and plays the result
// with media_player.play_media.
package radio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

const ToolPlay = "radio_play"

// Station is one search hit.
type Station struct {
	Title, ContentID, ContentType string
}

// Player is a media player Mira may use in a room.
type Player struct {
	Entity string
	// Name is what Home Assistant calls it, for the user ("Show Bad
	// Speaker"); empty means the entity id.
	Name string
	// Names are the lower-cased names the model might use for it.
	Names []string
	// Device is its device id in Home Assistant, if it has one.
	Device string
}

type Options struct {
	Lang i18n.Lang
	// Search looks stations up by name, most popular first.
	Search func(ctx context.Context, query string) ([]Station, error)
	// Play starts a station on a player.
	Play func(ctx context.Context, entity string, st Station) error
	// Players are the exposed media players in an area id.
	Players func(area string) []Player
	// Area resolves what the model says ("Küche") to an area id; "" if unknown.
	Area func(name string) string
	// AreaName is the display name of an area id.
	AreaName func(area string) string
}

type texts struct {
	desc, stationDesc, roomDesc, playerDesc string
	noRoom, noPlayer, severalPlayers        string
	noStation, severalStations, unavailable string
	playing                                 string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		desc:            "Startet einen Radiosender auf dem Lautsprecher eines Raums (ohne Raumangabe: der Raum, in dem der Nutzer spricht). Nur für echte Radiosender: „Spiel SWR3“, „Mach das Radio an“, „Ich will Radio hören“. Naturgeräusche, Musik und Abläufe wie „Einschlafen mit Regen“ sind keine Sender; passt dazu ein Skript oder eine Szene des Nutzers, nimm diese. Beenden, leiser, lauter geht mit den normalen Medienwerkzeugen.",
		stationDesc:     "Name des Senders, so wie der Nutzer ihn sagt, etwa „SWR3“ oder „Deutschlandfunk“.",
		roomDesc:        "Raum, in dem das Radio laufen soll; weglassen für den Raum des Nutzers.",
		playerDesc:      "Name des Lautsprechers, nur nötig, wenn der Raum mehrere hat.",
		noRoom:          "Ich weiß nicht, in welchem Raum das Radio laufen soll. Frag den Nutzer danach.",
		noPlayer:        "Ergebnis: nichts gestartet. Grund: Im Raum %s ist kein Lautsprecher für Mira freigegeben.",
		severalPlayers:  "Im Raum %s gibt es mehrere Lautsprecher: %s. Frag den Nutzer mit diesen Namen, welcher es sein soll, und ruf dann radio_play mit player (der Entität) auf.",
		noStation:       "Ergebnis: nichts gestartet. Gesucht: „%s“. Kein Sender gefunden.",
		severalStations: "Zu „%s“ gibt es mehrere Sender: %s. Frag den Nutzer, welcher es sein soll.",
		unavailable:     "Ergebnis: nichts gestartet. Grund: Die Radiosuche ist gerade nicht möglich (ist die Integration „Radio Browser“ in Home Assistant eingerichtet?).",
		playing:         "Ergebnis: läuft: %s auf %s im Raum %s. Den Lautsprecher nennen.",
	},
	i18n.EN: {
		desc:            "Starts a radio station on a room's speaker (without a room: the room the user is speaking in). Only for real radio stations: \"play SWR3\", \"turn on the radio\", \"I want to listen to the radio\". Nature sounds, music and routines such as \"sleep with rain\" are not stations; if one of the user's scripts or scenes fits, use that. Stopping, quieter, louder work with the normal media tools.",
		stationDesc:     "The station's name as the user says it, such as \"SWR3\" or \"BBC Radio 4\".",
		roomDesc:        "The room the radio should play in; leave out for the user's room.",
		playerDesc:      "Name of the speaker, only needed if the room has several.",
		noRoom:          "I do not know which room the radio should play in. Ask the user.",
		noPlayer:        "Result: nothing started. Reason: no speaker in %s is released for Mira.",
		severalPlayers:  "%s has several speakers: %s. Ask the user which one, by these names, then call radio_play with player (the entity).",
		noStation:       "Result: nothing started. Looked for: \"%s\". No station found.",
		severalStations: "There are several stations for \"%s\": %s. Ask the user which one.",
		unavailable:     "Result: nothing started. Reason: searching for radio stations is not possible right now (is the \"Radio Browser\" integration set up in Home Assistant?).",
		playing:         "Result: playing: %s on %s in %s. Name the speaker.",
	},
}

// Tool is radio_play.
func Tool(o Options) tools.InternalTool {
	t := i18n.Pick(catalog, o.Lang)
	schema := fmt.Sprintf(`{"type":"object","properties":{"station":{"type":"string","description":%q},"room":{"type":"string","description":%q},"player":{"type":"string","description":%q}},"required":["station"]}`,
		t.stationDesc, t.roomDesc, t.playerDesc)
	return tools.InternalTool{
		Name: ToolPlay, Description: t.desc, Schema: schema,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var args struct{ Station, Room, Player string }
			if err := json.Unmarshal(inv.Args, &args); err != nil || strings.TrimSpace(args.Station) == "" {
				return "", errors.New("station is required")
			}
			area := inv.Area
			if r := strings.TrimSpace(args.Room); r != "" {
				if area = o.Area(strings.ToLower(r)); area == "" {
					return t.noRoom, nil
				}
			}
			if area == "" {
				return t.noRoom, nil
			}
			room := o.AreaName(area)
			players := o.Players(area)
			if p := strings.ToLower(strings.TrimSpace(args.Player)); p != "" {
				players = slices.DeleteFunc(slices.Clone(players), func(pl Player) bool {
					return !slices.Contains(pl.Names, p) && strings.ToLower(pl.Entity) != p
				})
			}
			// Several speakers and none named: in the satellite's own room the
			// satellite's speaker plays (docs/spec/radio.md).
			if len(players) > 1 && args.Player == "" && area == inv.Area && inv.DeviceID != "" {
				own := slices.DeleteFunc(slices.Clone(players), func(pl Player) bool { return pl.Device != inv.DeviceID })
				if len(own) == 1 {
					players = own
				}
			}
			switch {
			case len(players) == 0:
				return fmt.Sprintf(t.noPlayer, room), nil
			case len(players) > 1:
				var ids []string
				for _, p := range players {
					ids = append(ids, p.label())
				}
				return fmt.Sprintf(t.severalPlayers, room, strings.Join(ids, ", ")), nil
			}
			stations, err := o.Search(ctx, args.Station)
			if err != nil {
				return t.unavailable, nil
			}
			st, titles := pick(stations, args.Station)
			switch {
			case len(stations) == 0:
				return fmt.Sprintf(t.noStation, args.Station), nil
			case st == nil:
				return fmt.Sprintf(t.severalStations, args.Station, strings.Join(titles, "; ")), nil
			}
			if err := o.Play(ctx, players[0].Entity, *st); err != nil {
				return "", err
			}
			return fmt.Sprintf(t.playing, st.Title, players[0].display(), room), nil
		},
	}
}

// pick chooses the station that was meant: the one whose name equals the
// query (spaces, case and punctuation aside), the most popular first; a
// single hit is taken as it is. With several different ones it returns nil
// and their names, so the user is asked.
func pick(stations []Station, query string) (*Station, []string) {
	if len(stations) == 1 {
		return &stations[0], nil
	}
	want := norm(query)
	for i := range stations {
		if norm(stations[i].Title) == want {
			return &stations[i], nil
		}
	}
	var titles []string
	for _, s := range stations {
		if !slices.Contains(titles, s.Title) && len(titles) < 5 {
			titles = append(titles, s.Title)
		}
	}
	return nil, titles
}

func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// label is a player for a question back: its name and, to call it, its
// entity.
func (p Player) label() string {
	if p.Name == "" || p.Name == p.Entity {
		return p.Entity
	}
	return fmt.Sprintf("%s (%s)", p.Name, p.Entity)
}

// display is the player's name for the user.
func (p Player) display() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Entity
}
