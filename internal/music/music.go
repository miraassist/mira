// Package music is Mira's part in music with Music Assistant
// (docs/spec/music-players.md): which players play its music, for the
// prompt's prefix, and music_move, which takes what plays along to another
// room with Music Assistant's transfer_queue.
package music

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// ToolMove is the tool that takes the music along.
const ToolMove = "music_move"

// Player is an exposed media player of Music Assistant.
type Player struct {
	Entity string
	// Name is what Home Assistant calls it ("Onkyo").
	Name string
	// Names are the lower-cased names the model might use for it.
	Names []string
	// Area is its area id, "" for none; AreaName its display name.
	Area, AreaName string
}

func (p Player) label() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Entity
}

type texts struct {
	header, rule, noArea                                     string
	desc, toDesc, fromDesc                                   string
	unknown, severalTo, nonePlays, severalFrom, alreadyThere string
	readFailed, moveFailed, moved                            string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		header: "## Musik (Music Assistant)",
		rule: "Musik und Radiosender spielst du mit media_player__search_media und media_player__play_media nur auf diesen Playern, mit ihrem " +
			"Namen; andere Lautsprecher können keine Musik aus Music Assistant spielen.",
		noArea:   "ohne Bereich",
		desc:     "Nimmt die Musik, die gerade läuft, mit in einen anderen Raum oder auf einen anderen Player: Titel, Warteschlange und Stelle im Stück wandern mit, am alten Ort hört sie auf („Nimm die Musik mit in die Küche“, „Mach die Musik im Bad weiter“, „Hol die Musik her“, „Verschieb die Musik hierher“). Auch „Lass die Musik hier weiterlaufen“, wenn sie gerade in einem anderen Raum läuft; „hier“ ist der Raum, in dem der Nutzer spricht. Nur für Musik aus Music Assistant.",
		toDesc:   "Wohin: ein Raum oder der Name eines Music-Assistant-Players.",
		fromDesc: "Woher, nur wenn der Nutzer es sagt: ein Raum oder der Name eines Players. Sonst findet das Werkzeug selbst, wo Musik läuft.",
		unknown:  "Ergebnis: nichts mitgenommen. „%s“ ist weder ein Raum mit einem Music-Assistant-Player noch ein solcher Player. Die Player: %s.",
		severalTo: "Ergebnis: nichts mitgenommen. Im Raum %s gibt es mehrere Music-Assistant-Player: %s. Nimm einen davon mit " +
			"seinem Namen; frag nur, wenn nichts dafür spricht.",
		nonePlays:    "Ergebnis: nichts mitgenommen. Gerade läuft auf keinem Music-Assistant-Player Musik.",
		severalFrom:  "Ergebnis: nichts mitgenommen. Musik läuft an mehreren Orten: %s. Frag den Nutzer, welche er mitnehmen will, und ruf music_move dann mit from auf.",
		alreadyThere: "Ergebnis: nichts zu tun. Die Musik läuft schon auf %s.",
		readFailed:   "Ich kann gerade nicht lesen, wo Musik läuft (%v).",
		moveFailed:   "Home Assistant hat das Mitnehmen abgelehnt: %v",
		moved:        "Ergebnis: Die Musik läuft jetzt auf %s weiter, an derselben Stelle; auf %s hat sie aufgehört. Sag es kurz auf deine Weise, mit den Räumen.",
	},
	i18n.EN: {
		header: "## Music (Music Assistant)",
		rule: "Play music and radio stations with media_player__search_media and media_player__play_media only on these players, by their " +
			"name; other speakers cannot play music from Music Assistant.",
		noArea:   "no area",
		desc:     "Takes the music that plays along to another room or player: the title, the queue and the place in the song move along, and it stops where it was (\"Take the music to the kitchen\", \"Continue the music in the bathroom\", \"Bring the music here\", \"Move the music over here\"). Also \"Let the music go on here\" when it plays in another room; \"here\" is the room the user speaks in. Only for music from Music Assistant.",
		toDesc:   "Where to: a room or the name of a Music Assistant player.",
		fromDesc: "Where from, only when the user says so: a room or a player's name. Otherwise the tool finds where music plays.",
		unknown:  "Result: nothing moved. \"%s\" is neither a room with a Music Assistant player nor such a player. The players: %s.",
		severalTo: "Result: nothing moved. The room %s has several Music Assistant players: %s. Take one of them by its " +
			"name; ask only when nothing points to one.",
		nonePlays:    "Result: nothing moved. No Music Assistant player plays music now.",
		severalFrom:  "Result: nothing moved. Music plays in several places: %s. Ask the user which to take along, then call music_move with from.",
		alreadyThere: "Result: nothing to do. The music plays on %s already.",
		readFailed:   "I cannot read where music plays right now (%v).",
		moveFailed:   "Home Assistant refused to move the music: %v",
		moved:        "Result: The music goes on on %s, at the same place; on %s it stopped. Say it briefly in your words, with the rooms.",
	},
}

// Section is the prefix's section on the Music Assistant players, per area
// (docs/spec/music-players.md, part 1); "" without any.
func Section(l i18n.Lang, players []Player) string {
	if len(players) == 0 {
		return ""
	}
	t := i18n.Pick(catalog, l)
	byArea := map[string][]string{}
	for _, p := range players {
		area := p.AreaName
		if p.Area == "" {
			area = ""
		} else if area == "" {
			area = p.Area
		}
		byArea[area] = append(byArea[area], p.label())
	}
	areas := make([]string, 0, len(byArea))
	for a := range byArea {
		if a != "" {
			areas = append(areas, a)
		}
	}
	sort.Strings(areas)
	if _, ok := byArea[""]; ok {
		areas = append(areas, "")
	}
	var b strings.Builder
	b.WriteString(t.header + "\n" + t.rule)
	for _, a := range areas {
		names := byArea[a]
		sort.Strings(names)
		label := a
		if a == "" {
			label = t.noArea
		}
		fmt.Fprintf(&b, "\n- %s: %s", label, strings.Join(names, ", "))
	}
	return b.String()
}

// Options are what music_move needs from the app.
type Options struct {
	Lang i18n.Lang
	// Players are the exposed Music Assistant players now.
	Players func() []Player
	// Area resolves what the model says ("Küche") to an area id; "" if unknown.
	Area func(name string) string
	// States reads the states of these players (one filtered read): the
	// state and the group each is in.
	States func(ctx context.Context, entities []string) (map[string]State, error)
	// Transfer is music_assistant.transfer_queue from source to target.
	Transfer func(ctx context.Context, target, source string) error
	// Join is media_player.join on the leader with these members; Unjoin
	// media_player.unjoin on a player (music_group).
	Join   func(ctx context.Context, leader string, members []string) error
	Unjoin func(ctx context.Context, player string) error
	Logger *slog.Logger
}

// State is a player's state and the players of its group (group_members,
// the player itself among them; empty when it is in none).
type State struct {
	State   string
	Members []string
}

// Tool is music_move, offered while there is a Music Assistant player.
func Tool(o Options) tools.InternalTool {
	t := i18n.Pick(catalog, o.Lang)
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to":   map[string]any{"type": "string", "description": t.toDesc},
			"from": map[string]any{"type": "string", "description": t.fromDesc},
		},
		"required": []string{"to"},
	})
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return tools.InternalTool{
		Name: ToolMove, Description: t.desc, Schema: string(schema),
		Enabled: func() bool { return len(o.Players()) > 0 },
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct{ To, From string }
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", fmt.Errorf("the arguments of %s are no JSON object: %.200q", ToolMove, string(inv.Args))
			}
			return move(ctx, o, t, log, inv.Area, a.To, a.From)
		},
	}
}

// find is the players a room or a name stands for: the player of that name,
// else the players of that area. ok is false when it is neither.
func find(o Options, players []Player, what string) ([]Player, string, bool) {
	key := strings.ToLower(strings.TrimSpace(what))
	for _, p := range players {
		for _, n := range p.Names {
			if n == key {
				return []Player{p}, "", true
			}
		}
	}
	area := o.Area(key)
	if area == "" {
		return nil, "", false
	}
	var out []Player
	areaName := area
	for _, p := range players {
		if p.Area == area {
			out = append(out, p)
			if p.AreaName != "" {
				areaName = p.AreaName
			}
		}
	}
	return out, areaName, len(out) > 0
}

func where(p Player) string {
	if p.AreaName == "" {
		return p.label()
	}
	return p.label() + " (" + p.AreaName + ")"
}

func list(players []Player) string {
	out := make([]string, len(players))
	for i, p := range players {
		out[i] = where(p)
	}
	return strings.Join(out, ", ")
}

func move(ctx context.Context, o Options, t texts, log *slog.Logger, turnArea, to, from string) (string, error) {
	players := o.Players()
	targets, area, ok := find(o, players, to)
	switch {
	case !ok:
		return fmt.Sprintf(t.unknown, to, list(players)), nil
	case len(targets) > 1:
		return fmt.Sprintf(t.severalTo, area, list(targets)), nil
	}
	target := targets[0]
	var sources []Player
	if strings.TrimSpace(from) != "" {
		var ok bool
		if sources, _, ok = find(o, players, from); !ok {
			return fmt.Sprintf(t.unknown, from, list(players)), nil
		}
	} else {
		sources = players
	}
	source, answer, err := playing(ctx, o, t, sources, turnArea)
	if err != nil {
		log.Warn("music_move: could not read the players' states", "error", err)
		return "", fmt.Errorf(t.readFailed, err)
	}
	if answer != "" {
		return answer, nil
	}
	if source.Entity == target.Entity {
		return fmt.Sprintf(t.alreadyThere, where(target)), nil
	}
	if err := o.Transfer(ctx, target.Entity, source.Entity); err != nil {
		log.Warn("music_move: Home Assistant refused the move", "from", source.Entity, "to", target.Entity, "error", err)
		return "", fmt.Errorf(t.moveFailed, err)
	}
	log.Info("music moved", "from", source.Entity, "to", target.Entity)
	return fmt.Sprintf(t.moved, where(target), where(source)), nil
}

// playing is the source among candidates: the one that plays, else the one
// paused; with several, the one in the turn's area. answer is the sentence
// for the model when there is none to take.
func playing(ctx context.Context, o Options, t texts, candidates []Player, turnArea string) (Player, string, error) {
	ids := make([]string, len(candidates))
	for i, p := range candidates {
		ids[i] = p.Entity
	}
	states, err := o.States(ctx, ids)
	if err != nil {
		return Player{}, "", err
	}
	p, several := source(states, candidates, turnArea)
	switch {
	case p.Entity != "":
		return p, "", nil
	case len(several) == 0:
		return Player{}, t.nonePlays, nil
	}
	return Player{}, fmt.Sprintf(t.severalFrom, list(several)), nil
}

// source picks the player that plays among candidates from their states.
// Without one, several are the players that play (or are paused) when there
// are more than one and none is in the turn's area; empty when none plays.
func source(states map[string]State, candidates []Player, turnArea string) (Player, []Player) {
	pick := func(state string) []Player {
		var out []Player
		for _, p := range candidates {
			if states[p.Entity].State == state {
				out = append(out, p)
			}
		}
		return out
	}
	found := pick("playing")
	if len(found) == 0 {
		found = pick("paused")
	}
	switch len(found) {
	case 0:
		return Player{}, nil
	case 1:
		return found[0], nil
	}
	var here []Player
	for _, p := range found {
		if turnArea != "" && p.Area == turnArea {
			here = append(here, p)
		}
	}
	if len(here) == 1 {
		return here[0], nil
	}
	return Player{}, found
}

// OnlyWithout offers tool (radio_play) only while there is no Music Assistant
// player: radio then plays through them as music (docs/spec/music-players.md,
// part 4).
func OnlyWithout(tool tools.InternalTool, players func() []Player) tools.InternalTool {
	before := tool.Enabled
	tool.Enabled = func() bool { return len(players()) == 0 && (before == nil || before()) }
	return tool
}
