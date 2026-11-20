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

// ToolMore is the tool that appends music like what plays, or like a search
// hit, after the current item (docs/spec/music-players.md, part 6).
const ToolMore = "music_more"

type moreTexts struct {
	desc, idDesc, playerDesc                      string
	unknown, nonePlays, nonePlaysHit, severalFrom string
	station, noContent                            string
	likeCurrent, likeHit, afterStation            string
	readFailed, moreFailed                        string
}

var moreCatalog = map[i18n.Lang]moreTexts{
	i18n.DE: {
		desc: "Spielt nach dem laufenden Stück mehr Musik in der Art: Music Assistant hängt ähnliche Titel an die Warteschlange, " +
			"das laufende Stück spielt zu Ende („Spiel mehr davon“, „Mehr in der Art“, „Spiel Ähnliches“). Ohne media_content_id ist es " +
			"Musik wie das, was gerade läuft. Nennt der Nutzer, wie es sein soll („Spiel was wie Queen“), such das zuerst mit " +
			"media_player__search_media auf dem Player, der spielt, und gib hier den Treffer an. Nur für Musik aus Music Assistant.",
		idDesc:     "Nur wenn der Nutzer etwas nennt: der media_content_id eines Treffers von media_player__search_media. Ohne ist es Musik wie das laufende Stück.",
		playerDesc: "Wo, nur wenn der Nutzer es sagt: ein Raum oder der Name eines Music-Assistant-Players. Sonst findet das Werkzeug selbst, wo Musik läuft.",
		unknown:    "Ergebnis: nichts angehängt. „%s“ ist weder ein Raum mit einem Music-Assistant-Player noch ein solcher Player. Die Player: %s.",
		nonePlays:  "Ergebnis: nichts angehängt. Gerade läuft dort keine Musik, also gibt es nichts, wovon mehr kommen könnte.",
		nonePlaysHit: "Ergebnis: nichts angehängt. Gerade läuft dort keine Musik, also gibt es keine Warteschlange. " +
			"Spiel den Treffer stattdessen mit media_player__play_media.",
		severalFrom: "Ergebnis: nichts angehängt. Musik läuft an mehreren Orten: %s. Frag den Nutzer, wo er mehr davon hören will, " +
			"und ruf music_more dann mit player auf.",
		station: "Ergebnis: nichts angehängt. Auf %s läuft ein Radiosender; „mehr davon“ geht mit Titeln, nicht mit einem Sender. " +
			"Sag das dem Nutzer; nennt er einen Titel oder Künstler, such ihn und ruf music_more mit dem Treffer auf.",
		noContent: "Ergebnis: nichts angehängt. %s sagt nicht, was gerade läuft. Frag den Nutzer, wie die Musik sein soll, " +
			"such das und ruf music_more mit dem Treffer auf.",
		likeCurrent:  "Ergebnis: Auf %s folgt nach dem laufenden Stück ähnliche Musik.",
		likeHit:      "Ergebnis: Auf %s folgt nach dem laufenden Stück Musik in der Art des Treffers.",
		afterStation: " Gerade läuft dort ein Radiosender, der nicht von selbst endet: die Musik kommt erst, wenn der Nutzer „weiter“ sagt. Sag ihm das.",
		readFailed:   "Ich kann gerade nicht lesen, wo Musik läuft (%v).",
		moreFailed:   "Home Assistant hat das Anhängen ähnlicher Musik abgelehnt: %v",
	},
	i18n.EN: {
		desc: "Plays more music of the kind after the current song: Music Assistant appends similar songs to the queue, " +
			"the current song plays to its end (\"Play more of this\", \"More like this\", \"Play something similar\"). Without " +
			"media_content_id it is music like what plays now. When the user names what it should be like (\"Play something like Queen\"), " +
			"search that first with media_player__search_media on the player that plays and give the hit here. Only for music from Music Assistant.",
		idDesc:     "Only when the user names something: the media_content_id of a hit of media_player__search_media. Without it, the music is like the current song.",
		playerDesc: "Where, only when the user says so: a room or the name of a Music Assistant player. Otherwise the tool finds where music plays.",
		unknown:    "Result: nothing appended. \"%s\" is neither a room with a Music Assistant player nor such a player. The players: %s.",
		nonePlays:  "Result: nothing appended. No music plays there now, so there is nothing to play more of.",
		nonePlaysHit: "Result: nothing appended. No music plays there now, so there is no queue. " +
			"Play the hit with media_player__play_media instead.",
		severalFrom: "Result: nothing appended. Music plays in several places: %s. Ask the user where they want more of it, " +
			"then call music_more with player.",
		station: "Result: nothing appended. A radio station plays on %s; \"more of this\" works with songs, not with a station. " +
			"Tell the user; when they name a song or an artist, search it and call music_more with the hit.",
		noContent: "Result: nothing appended. %s does not say what plays. Ask the user what the music should be like, " +
			"search that and call music_more with the hit.",
		likeCurrent:  "Result: On %s, similar music follows after the current song.",
		likeHit:      "Result: On %s, music like the hit follows after the current song.",
		afterStation: " A radio station plays there, which does not end by itself: the music comes only when the user says \"next\". Tell them.",
		readFailed:   "I cannot read where music plays right now (%v).",
		moreFailed:   "Home Assistant refused to append similar music: %v",
	},
}

// MoreTool is music_more, offered while there is a Music Assistant player.
func MoreTool(o Options) tools.InternalTool {
	m := i18n.Pick(moreCatalog, o.Lang)
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"media_content_id": map[string]any{"type": "string", "description": m.idDesc},
			"player":           map[string]any{"type": "string", "description": m.playerDesc},
		},
	})
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return tools.InternalTool{
		Name: ToolMore, Description: m.desc, Schema: string(schema),
		Enabled: func() bool { return len(o.Players()) > 0 },
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				ID     string `json:"media_content_id"`
				Player string `json:"player"`
			}
			if len(strings.TrimSpace(string(inv.Args))) > 0 {
				if err := json.Unmarshal(inv.Args, &a); err != nil {
					return "", fmt.Errorf("the arguments of %s are no JSON object: %.200q", ToolMore, string(inv.Args))
				}
			}
			return more(ctx, o, m, log, inv.Area, strings.TrimSpace(a.ID), a.Player)
		},
	}
}

func more(ctx context.Context, o Options, m moreTexts, log *slog.Logger, turnArea, hit, player string) (string, error) {
	players := o.Players()
	candidates := players
	if strings.TrimSpace(player) != "" {
		var ok bool
		if candidates, ok = findSource(o, players, player); !ok {
			return fmt.Sprintf(m.unknown, player, list(players)), nil
		}
	}
	ids := make([]string, len(candidates))
	for i, p := range candidates {
		ids[i] = p.Entity
	}
	states, err := o.States(ctx, ids)
	if err != nil {
		log.Warn("music_more: could not read the players' states", "error", err)
		return "", fmt.Errorf(m.readFailed, err)
	}
	target, several := source(states, candidates, turnArea)
	switch {
	case target.Entity != "":
	case len(several) > 0:
		return fmt.Sprintf(m.severalFrom, list(several)), nil
	case hit != "":
		return m.nonePlaysHit, nil
	default:
		return m.nonePlays, nil
	}
	content := states[target.Entity].Content
	radio := station(content)
	seed := hit
	if seed == "" {
		switch {
		case radio:
			return fmt.Sprintf(m.station, where(target)), nil
		case content == "":
			log.Warn("music_more: the player that plays has no media_content_id; nothing appended", "player", target.Entity)
			return fmt.Sprintf(m.noContent, where(target)), nil
		}
		seed = content
	}
	if err := o.More(ctx, target.Entity, seed); err != nil {
		log.Warn("music_more: Home Assistant refused similar music", "player", target.Entity, "seed", seed, "error", err)
		return "", fmt.Errorf(m.moreFailed, err)
	}
	log.Info("similar music appended", "player", target.Entity, "seed", seed, "from_hit", hit != "", "after_station", radio)
	answer := fmt.Sprintf(m.likeCurrent, where(target))
	if hit != "" {
		answer = fmt.Sprintf(m.likeHit, where(target))
	}
	if radio {
		answer += m.afterStation
	}
	return answer + " " + i18n.Pick(groupCatalog, o.Lang).header, nil
}
