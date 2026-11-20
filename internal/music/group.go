package music

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// ToolGroup is the tool that adds players to what plays and takes them out
// (docs/spec/music-players.md, part 3).
const ToolGroup = "music_group"

// everywhere are the words for every Music Assistant player in add.
var everywhere = map[string]bool{"alle": true, "überall": true, "all": true, "everywhere": true}

type groupTexts struct {
	desc, addDesc, removeDesc                    string
	noArgs, unknown, nonePlays, severalFrom      string
	alreadyIn, nobodyElse, joined, left, notIn   string
	readFailed, joinFailed, unjoinFailed, header string
}

var groupCatalog = map[i18n.Lang]groupTexts{
	i18n.DE: {
		desc:         "Nimmt Räume oder Player zur Musik dazu, die gerade läuft, damit sie dort gleichzeitig dasselbe spielen, oder nimmt sie wieder heraus („Spiel das auch im Bad“, „Mach die Musik überall an“, „Nimm das Bad wieder raus“). Jeder behält seine Lautstärke. Wer die Musik ganz in einen anderen Raum mitnehmen will, ohne dass sie hier weiterläuft: music_move. Nur für Musik aus Music Assistant.",
		addDesc:      "Dazunehmen: Räume oder Namen von Music-Assistant-Playern, mit Komma getrennt; „alle“ für überall. Ein Raum mit mehreren Playern: alle davon.",
		removeDesc:   "Herausnehmen: Räume oder Namen von Music-Assistant-Playern, mit Komma getrennt.",
		noArgs:       "the arguments of %s name neither add nor remove: %.200q",
		unknown:      "Ergebnis: nichts geändert. „%s“ ist weder ein Raum mit einem Music-Assistant-Player noch ein solcher Player. Die Player: %s.",
		nonePlays:    "Ergebnis: nichts dazugenommen. Gerade läuft auf keinem Music-Assistant-Player Musik.",
		severalFrom:  "Ergebnis: nichts dazugenommen. Musik läuft an mehreren Orten: %s. Frag den Nutzer, zu welcher er dazunehmen will.",
		alreadyIn:    "Ergebnis: nichts dazugenommen. %s spielt schon mit %s.",
		nobodyElse:   "Ergebnis: nichts dazugenommen. Außer %s gibt es keinen Music-Assistant-Player.",
		joined:       "Ergebnis: %s spielt jetzt dieselbe Musik wie %s.",
		left:         "Ergebnis: %s ist aus der Gruppe heraus; dort hört die Musik auf, auf den anderen läuft sie weiter.",
		notIn:        "%s spielt in keiner Gruppe; nichts getan.",
		readFailed:   "Ich kann gerade nicht lesen, wo Musik läuft (%v).",
		joinFailed:   "Home Assistant hat das Dazunehmen abgelehnt: %v",
		unjoinFailed: "Home Assistant hat das Herausnehmen von %s abgelehnt: %v",
		header:       "Sag es kurz auf deine Weise, mit den Räumen.",
	},
	i18n.EN: {
		desc:         "Adds rooms or players to the music that plays, so that they play the same at the same time, or takes them out again (\"Play this in the bathroom too\", \"Put the music on everywhere\", \"Take the bathroom out again\"). Each keeps its own volume. To take the music to another room so that it stops here: music_move. Only for music from Music Assistant.",
		addDesc:      "To add: rooms or names of Music Assistant players, separated by commas; \"all\" for everywhere. A room with several players: all of them.",
		removeDesc:   "To take out: rooms or names of Music Assistant players, separated by commas.",
		noArgs:       "the arguments of %s name neither add nor remove: %.200q",
		unknown:      "Result: nothing changed. \"%s\" is neither a room with a Music Assistant player nor such a player. The players: %s.",
		nonePlays:    "Result: nothing added. No Music Assistant player plays music now.",
		severalFrom:  "Result: nothing added. Music plays in several places: %s. Ask the user which one to add to.",
		alreadyIn:    "Result: nothing added. %s plays with %s already.",
		nobodyElse:   "Result: nothing added. There is no Music Assistant player but %s.",
		joined:       "Result: %s now plays the same music as %s.",
		left:         "Result: %s is out of the group; the music stops there and goes on on the others.",
		notIn:        "%s plays in no group; nothing done.",
		readFailed:   "I cannot read where music plays right now (%v).",
		joinFailed:   "Home Assistant refused to add the players: %v",
		unjoinFailed: "Home Assistant refused to take out %s: %v",
		header:       "Say it briefly in your words, with the rooms.",
	},
}

// GroupTool is music_group, offered while there is a Music Assistant player.
func GroupTool(o Options) tools.InternalTool {
	g := i18n.Pick(groupCatalog, o.Lang)
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"add":    map[string]any{"type": "string", "description": g.addDesc},
			"remove": map[string]any{"type": "string", "description": g.removeDesc},
		},
	})
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return tools.InternalTool{
		Name: ToolGroup, Description: g.desc, Schema: string(schema),
		Enabled: func() bool { return len(o.Players()) > 0 },
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct{ Add, Remove string }
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", fmt.Errorf("the arguments of %s are no JSON object: %.200q", ToolGroup, string(inv.Args))
			}
			if strings.TrimSpace(a.Add) == "" && strings.TrimSpace(a.Remove) == "" {
				return "", fmt.Errorf(g.noArgs, ToolGroup, string(inv.Args))
			}
			return group(ctx, o, g, log, inv.Area, a.Add, a.Remove)
		},
	}
}

// resolve is the players a comma-separated list of rooms and names stands
// for, each once; all is true for "alle". unknown is the first word that is
// neither a room with a player nor a player.
func resolve(o Options, players []Player, what string) (out []Player, all bool, unknown string) {
	seen := map[string]bool{}
	for _, part := range strings.Split(what, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if everywhere[strings.ToLower(part)] {
			all = true
			continue
		}
		found, _, ok := find(o, players, part)
		if !ok {
			return nil, false, part
		}
		for _, p := range found {
			if !seen[p.Entity] {
				seen[p.Entity] = true
				out = append(out, p)
			}
		}
	}
	return out, all, ""
}

func group(ctx context.Context, o Options, g groupTexts, log *slog.Logger, turnArea, add, remove string) (string, error) {
	players := o.Players()
	adds, all, unknown := resolve(o, players, add)
	if unknown != "" {
		return fmt.Sprintf(g.unknown, unknown, list(players)), nil
	}
	if all {
		adds = players
	}
	removes, _, unknown := resolve(o, players, remove)
	if unknown != "" {
		return fmt.Sprintf(g.unknown, unknown, list(players)), nil
	}
	ids := make([]string, len(players))
	for i, p := range players {
		ids[i] = p.Entity
	}
	states, err := o.States(ctx, ids)
	if err != nil {
		log.Warn("music_group: could not read the players' states", "error", err)
		return "", fmt.Errorf(g.readFailed, err)
	}
	var answers []string
	if strings.TrimSpace(add) != "" {
		answer, err := join(ctx, o, g, log, states, players, adds, turnArea)
		if err != nil {
			return "", err
		}
		answers = append(answers, answer)
	}
	if len(removes) > 0 {
		answer, err := unjoin(ctx, o, g, log, states, removes)
		if err != nil {
			return "", err
		}
		answers = append(answers, answer)
	}
	return strings.Join(answers, " ") + " " + g.header, nil
}

func join(ctx context.Context, o Options, g groupTexts, log *slog.Logger, states map[string]State, players, adds []Player, turnArea string) (string, error) {
	leader, several := source(states, players, turnArea)
	switch {
	case leader.Entity != "":
	case len(several) == 0:
		return g.nonePlays, nil
	default:
		return fmt.Sprintf(g.severalFrom, list(several)), nil
	}
	in := map[string]bool{leader.Entity: true}
	for _, m := range states[leader.Entity].Members {
		in[m] = true
	}
	var members []Player
	var already []Player
	for _, p := range adds {
		switch {
		case p.Entity == leader.Entity:
		case in[p.Entity]:
			already = append(already, p)
		default:
			members = append(members, p)
		}
	}
	if len(members) == 0 {
		if len(already) == 0 {
			return fmt.Sprintf(g.nobodyElse, where(leader)), nil
		}
		return fmt.Sprintf(g.alreadyIn, list(already), where(leader)), nil
	}
	ids := make([]string, len(members))
	for i, p := range members {
		ids[i] = p.Entity
	}
	if err := o.Join(ctx, leader.Entity, ids); err != nil {
		log.Warn("music_group: Home Assistant refused the join", "leader", leader.Entity, "members", ids, "error", err)
		return "", fmt.Errorf(g.joinFailed, err)
	}
	log.Info("music joined", "leader", leader.Entity, "members", ids)
	return fmt.Sprintf(g.joined, list(members), where(leader)), nil
}

func unjoin(ctx context.Context, o Options, g groupTexts, log *slog.Logger, states map[string]State, removes []Player) (string, error) {
	var left, notIn []Player
	for _, p := range removes {
		if len(states[p.Entity].Members) < 2 {
			notIn = append(notIn, p)
			continue
		}
		if err := o.Unjoin(ctx, p.Entity); err != nil {
			log.Warn("music_group: Home Assistant refused the unjoin", "player", p.Entity, "done", len(left), "error", err)
			return "", fmt.Errorf(g.unjoinFailed, where(p), err)
		}
		log.Info("music unjoined", "player", p.Entity)
		left = append(left, p)
	}
	var out []string
	if len(left) > 0 {
		out = append(out, fmt.Sprintf(g.left, list(left)))
	}
	if len(notIn) > 0 {
		out = append(out, fmt.Sprintf(g.notIn, list(notIn)))
	}
	return strings.Join(out, " "), nil
}
