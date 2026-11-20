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

// ToolQueue is the tool that puts an item into the queue of the player that
// plays (docs/spec/music-players.md, part 5).
const ToolQueue = "music_queue"

// Music Assistant's enqueue modes for music_queue's when.
const (
	EnqueueNext = "next"
	EnqueueAdd  = "add"
)

type queueTexts struct {
	desc, idDesc, typeDesc, whenDesc, playerDesc string
	noID, badWhen                                string
	unknown, nonePlays, severalFrom              string
	next, end, afterStation                      string
	readFailed, queueFailed                      string
}

var queueCatalog = map[i18n.Lang]queueTexts{
	i18n.DE: {
		desc: "Reiht einen Titel, ein Album, einen Künstler oder eine Playlist in die Warteschlange der Musik ein, die gerade läuft, ohne sie zu unterbrechen. " +
			"Als Nächstes nach dem laufenden Stück („Spiel danach Bohemian Rhapsody“, „Als Nächstes bitte was von Queen“) oder hinten an („Setz das auf die Liste“, " +
			"„Häng Thriller hinten an“, „Reih noch was von Adele ein“). Such das Stück zuerst mit media_player__search_media auf dem Player, der spielt, " +
			"und gib hier den Treffer an. media_player__play_media ersetzt dagegen, was läuft. Nur für Musik aus Music Assistant.",
		idDesc:     "Der media_content_id eines Treffers von media_player__search_media.",
		typeDesc:   "Der media_content_type desselben Treffers, wenn er einen hat.",
		whenDesc:   "„next“: gleich nach dem laufenden Stück; „end“: ans Ende der Warteschlange (Standard).",
		playerDesc: "Wo, nur wenn der Nutzer es sagt: ein Raum oder der Name eines Music-Assistant-Players. Sonst findet das Werkzeug selbst, wo Musik läuft.",
		noID:       "the arguments of %s have no media_content_id: %.200q",
		badWhen:    "when of %s is %q, not \"next\" or \"end\"",
		unknown:    "Ergebnis: nichts eingereiht. „%s“ ist weder ein Raum mit einem Music-Assistant-Player noch ein solcher Player. Die Player: %s.",
		nonePlays:  "Ergebnis: nichts eingereiht. Gerade läuft dort keine Musik, also gibt es keine Warteschlange. Spiel den Treffer stattdessen mit media_player__play_media.",
		severalFrom: "Ergebnis: nichts eingereiht. Musik läuft an mehreren Orten: %s. Frag den Nutzer, wo er es einreihen will, " +
			"und ruf music_queue dann mit player auf.",
		next:         "Ergebnis: eingereiht auf %s, als Nächstes nach dem laufenden Stück.",
		end:          "Ergebnis: eingereiht auf %s, ans Ende der Warteschlange.",
		afterStation: " Gerade läuft dort ein Radiosender, der nicht von selbst endet: das Eingereihte kommt erst, wenn der Nutzer „weiter“ sagt. Sag ihm das.",
		readFailed:   "Ich kann gerade nicht lesen, wo Musik läuft (%v).",
		queueFailed:  "Home Assistant hat das Einreihen abgelehnt: %v",
	},
	i18n.EN: {
		desc: "Puts a song, an album, an artist or a playlist into the queue of the music that plays, without interrupting it. " +
			"Next after the current song (\"Play Bohemian Rhapsody after this\", \"Next, something by Queen\") or at the end (\"Put this on the list\", " +
			"\"Add Thriller at the end\", \"Queue something by Adele\"). Search the item first with media_player__search_media on the player that plays " +
			"and give the hit here. media_player__play_media, by contrast, replaces what plays. Only for music from Music Assistant.",
		idDesc:     "The media_content_id of a hit of media_player__search_media.",
		typeDesc:   "The media_content_type of the same hit, when it has one.",
		whenDesc:   "\"next\": right after the current song; \"end\": at the end of the queue (the default).",
		playerDesc: "Where, only when the user says so: a room or the name of a Music Assistant player. Otherwise the tool finds where music plays.",
		noID:       "the arguments of %s have no media_content_id: %.200q",
		badWhen:    "when of %s is %q, not \"next\" or \"end\"",
		unknown:    "Result: nothing queued. \"%s\" is neither a room with a Music Assistant player nor such a player. The players: %s.",
		nonePlays:  "Result: nothing queued. No music plays there now, so there is no queue. Play the hit with media_player__play_media instead.",
		severalFrom: "Result: nothing queued. Music plays in several places: %s. Ask the user where to queue it, " +
			"then call music_queue with player.",
		next:         "Result: queued on %s, next after the current song.",
		end:          "Result: queued on %s, at the end of the queue.",
		afterStation: " A radio station plays there, which does not end by itself: the queued item comes only when the user says \"next\". Tell them.",
		readFailed:   "I cannot read where music plays right now (%v).",
		queueFailed:  "Home Assistant refused to queue it: %v",
	},
}

// QueueTool is music_queue, offered while there is a Music Assistant player.
func QueueTool(o Options) tools.InternalTool {
	q := i18n.Pick(queueCatalog, o.Lang)
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"media_content_id":   map[string]any{"type": "string", "description": q.idDesc},
			"media_content_type": map[string]any{"type": "string", "description": q.typeDesc},
			"when":               map[string]any{"type": "string", "enum": []string{"next", "end"}, "description": q.whenDesc},
			"player":             map[string]any{"type": "string", "description": q.playerDesc},
		},
		"required": []string{"media_content_id"},
	})
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return tools.InternalTool{
		Name: ToolQueue, Description: q.desc, Schema: string(schema),
		Enabled: func() bool { return len(o.Players()) > 0 },
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				ID     string `json:"media_content_id"`
				Type   string `json:"media_content_type"`
				When   string `json:"when"`
				Player string `json:"player"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", fmt.Errorf("the arguments of %s are no JSON object: %.200q", ToolQueue, string(inv.Args))
			}
			if strings.TrimSpace(a.ID) == "" {
				return "", fmt.Errorf(q.noID, ToolQueue, string(inv.Args))
			}
			enqueue := EnqueueAdd
			switch strings.TrimSpace(a.When) {
			case "", "end":
			case "next":
				enqueue = EnqueueNext
			default:
				return "", fmt.Errorf(q.badWhen, ToolQueue, a.When)
			}
			return queue(ctx, o, q, log, inv.Area, strings.TrimSpace(a.ID), strings.TrimSpace(a.Type), enqueue, a.Player)
		},
	}
}

// station tells whether what a player plays is a radio station: Music
// Assistant's URI of it has "://radio/" ("library://radio/1",
// "radiobrowser://radio/…").
func station(contentID string) bool { return strings.Contains(contentID, "://radio/") }

func queue(ctx context.Context, o Options, q queueTexts, log *slog.Logger, turnArea, id, mediaType, enqueue, player string) (string, error) {
	players := o.Players()
	candidates := players
	if strings.TrimSpace(player) != "" {
		var ok bool
		if candidates, ok = findSource(o, players, player); !ok {
			return fmt.Sprintf(q.unknown, player, list(players)), nil
		}
	}
	ids := make([]string, len(candidates))
	for i, p := range candidates {
		ids[i] = p.Entity
	}
	states, err := o.States(ctx, ids)
	if err != nil {
		log.Warn("music_queue: could not read the players' states", "error", err)
		return "", fmt.Errorf(q.readFailed, err)
	}
	target, several := source(states, candidates, turnArea)
	switch {
	case target.Entity != "":
	case len(several) == 0:
		return q.nonePlays, nil
	default:
		return fmt.Sprintf(q.severalFrom, list(several)), nil
	}
	if err := o.Queue(ctx, target.Entity, id, mediaType, enqueue); err != nil {
		log.Warn("music_queue: Home Assistant refused to queue", "player", target.Entity, "media_id", id, "enqueue", enqueue, "error", err)
		return "", fmt.Errorf(q.queueFailed, err)
	}
	radio := station(states[target.Entity].Content)
	log.Info("music queued", "player", target.Entity, "media_id", id, "enqueue", enqueue, "after_station", radio)
	answer := fmt.Sprintf(q.end, where(target))
	if enqueue == EnqueueNext {
		answer = fmt.Sprintf(q.next, where(target))
	}
	if radio {
		answer += q.afterStation
	}
	return answer + " " + i18n.Pick(groupCatalog, o.Lang).header, nil
}
