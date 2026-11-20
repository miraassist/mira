package music

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// ToolSeek is the tool that winds within the song that plays
// (docs/spec/music-players.md, part 8).
const ToolSeek = "music_seek"

type seekTexts struct {
	desc, secondsDesc, positionDesc, playerDesc string
	unknown, nonePlays, severalFrom             string
	station, noPosition, beyondEnd, beyondLen   string
	sought                                      string
	readFailed, seekFailed                      string
}

var seekCatalog = map[i18n.Lang]seekTexts{
	i18n.DE: {
		desc: "Spult im Stück, das gerade läuft, vor oder zurück oder springt an eine Stelle darin. Relativ mit seconds " +
			"(„Spring dreißig Sekunden vor“, „Geh eine Minute zurück“, „Spul zwei Minuten vor“) oder an eine Stelle mit position " +
			"(„Geh an den Anfang“, „Spring zu Minute drei“). Genau eins von beiden. Dafür ist dieses Werkzeug da, nicht HassMediaNext: " +
			"das wechselt zum nächsten Titel. Nur für Musik aus Music Assistant.",
		secondsDesc:  "Um wie viele Sekunden, relativ zur Stelle jetzt: positiv vor, negativ zurück („eine Minute zurück“ = -60).",
		positionDesc: "An welche Stelle, in Sekunden vom Anfang des Stücks („an den Anfang“ = 0, „zu Minute drei“ = 180).",
		playerDesc:   "Wo, nur wenn der Nutzer es sagt: ein Raum oder der Name eines Music-Assistant-Players. Sonst findet das Werkzeug selbst, wo Musik läuft.",
		unknown:      "Ergebnis: nichts gespult. „%s“ ist weder ein Raum mit einem Music-Assistant-Player noch ein solcher Player. Die Player: %s.",
		nonePlays:    "Ergebnis: nichts gespult. Gerade läuft dort keine Musik.",
		severalFrom: "Ergebnis: nichts gespult. Musik läuft an mehreren Orten: %s. Frag den Nutzer, wo er spulen will, " +
			"und ruf music_seek dann mit player auf.",
		station:    "Ergebnis: nichts gespult. Auf %s läuft ein Radiosender; in einem Sender kann man nicht spulen. Sag das dem Nutzer.",
		noPosition: "Ergebnis: nichts gespult. %s sagt nicht, an welcher Stelle im Stück er ist, also geht relatives Spulen nicht. Eine feste Stelle (position) geht.",
		beyondEnd:  "Ergebnis: nichts gespult. Das Stück auf %s ist nur %s lang und steht bei %s; %s liegt dahinter. Sag das dem Nutzer.",
		beyondLen:  "Ergebnis: nichts gespult. Das Stück auf %s ist nur %s lang; %s liegt dahinter. Sag das dem Nutzer.",
		sought:     "Ergebnis: Auf %s geht es jetzt bei %s im Stück weiter.",
		readFailed: "Ich kann gerade nicht lesen, wo Musik läuft (%v).",
		seekFailed: "Home Assistant hat das Spulen abgelehnt: %v",
	},
	i18n.EN: {
		desc: "Winds forward or back within the song that plays, or jumps to a place in it. Relative with seconds " +
			"(\"Skip thirty seconds ahead\", \"Go back a minute\", \"Fast forward two minutes\") or to a place with position " +
			"(\"Go to the beginning\", \"Jump to minute three\"). Exactly one of the two. This is the tool for it, not HassMediaNext: " +
			"that goes to the next song. Only for music from Music Assistant.",
		secondsDesc:  "By how many seconds, from the place now: positive forward, negative back (\"back a minute\" = -60).",
		positionDesc: "To which place, in seconds from the start of the song (\"to the beginning\" = 0, \"to minute three\" = 180).",
		playerDesc:   "Where, only when the user says so: a room or the name of a Music Assistant player. Otherwise the tool finds where music plays.",
		unknown:      "Result: nothing wound. \"%s\" is neither a room with a Music Assistant player nor such a player. The players: %s.",
		nonePlays:    "Result: nothing wound. No music plays there now.",
		severalFrom: "Result: nothing wound. Music plays in several places: %s. Ask the user where to wind, " +
			"then call music_seek with player.",
		station:    "Result: nothing wound. A radio station plays on %s; a station cannot be wound. Tell the user.",
		noPosition: "Result: nothing wound. %s does not tell its place in the song, so winding relatively does not work. A fixed place (position) does.",
		beyondEnd:  "Result: nothing wound. The song on %s is only %s long and is at %s; %s is beyond its end. Tell the user.",
		beyondLen:  "Result: nothing wound. The song on %s is only %s long; %s is beyond its end. Tell the user.",
		sought:     "Result: On %s the song goes on at %s.",
		readFailed: "I cannot read where music plays right now (%v).",
		seekFailed: "Home Assistant refused to wind: %v",
	},
}

// SeekTool is music_seek, offered while there is a Music Assistant player.
func SeekTool(o Options) tools.InternalTool {
	s := i18n.Pick(seekCatalog, o.Lang)
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"seconds":  map[string]any{"type": "number", "description": s.secondsDesc},
			"position": map[string]any{"type": "number", "description": s.positionDesc},
			"player":   map[string]any{"type": "string", "description": s.playerDesc},
		},
	})
	log := o.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return tools.InternalTool{
		Name: ToolSeek, Description: s.desc, Schema: string(schema),
		Enabled: func() bool { return len(o.Players()) > 0 },
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				Seconds  *float64 `json:"seconds"`
				Position *float64 `json:"position"`
				Player   string   `json:"player"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", fmt.Errorf("the arguments of %s are no JSON object with numbers seconds and position: %.200q", ToolSeek, string(inv.Args))
			}
			switch {
			case a.Seconds != nil && a.Position != nil:
				return "", fmt.Errorf("the arguments of %s have both seconds and position, give one: %.200q", ToolSeek, string(inv.Args))
			case a.Seconds == nil && a.Position == nil:
				return "", fmt.Errorf("the arguments of %s have neither seconds nor position, give one: %.200q", ToolSeek, string(inv.Args))
			case a.Position != nil && *a.Position < 0:
				return "", fmt.Errorf("position of %s is below 0: %.200q", ToolSeek, string(inv.Args))
			}
			return seek(ctx, o, s, log, inv.Area, a.Seconds, a.Position, a.Player)
		},
	}
}

func seek(ctx context.Context, o Options, s seekTexts, log *slog.Logger, turnArea string, seconds, position *float64, player string) (string, error) {
	players := o.Players()
	candidates := players
	if strings.TrimSpace(player) != "" {
		var ok bool
		if candidates, ok = findSource(o, players, player); !ok {
			return fmt.Sprintf(s.unknown, player, list(players)), nil
		}
	}
	ids := make([]string, len(candidates))
	for i, p := range candidates {
		ids[i] = p.Entity
	}
	states, err := o.States(ctx, ids)
	if err != nil {
		log.Warn("music_seek: could not read the players' states", "error", err)
		return "", fmt.Errorf(s.readFailed, err)
	}
	target, several := source(states, candidates, turnArea)
	switch {
	case target.Entity != "":
	case len(several) == 0:
		return s.nonePlays, nil
	default:
		return fmt.Sprintf(s.severalFrom, list(several)), nil
	}
	st := states[target.Entity]
	if station(st.Content) {
		return fmt.Sprintf(s.station, where(target)), nil
	}
	var to float64
	if position != nil {
		to = math.Round(*position)
	} else {
		if !st.PositionKnown {
			return fmt.Sprintf(s.noPosition, where(target)), nil
		}
		to = math.Max(0, math.Round(current(o, st)+*seconds))
	}
	if st.Duration > 0 && to > st.Duration {
		if !st.PositionKnown {
			return fmt.Sprintf(s.beyondLen, where(target), clock(st.Duration), clock(to)), nil
		}
		return fmt.Sprintf(s.beyondEnd, where(target), clock(st.Duration), clock(current(o, st)), clock(to)), nil
	}
	if err := o.Seek(ctx, target.Entity, to); err != nil {
		log.Warn("music_seek: Home Assistant refused the seek", "player", target.Entity, "position", to, "error", err)
		return "", fmt.Errorf(s.seekFailed, err)
	}
	log.Info("music wound", "player", target.Entity, "position", to)
	return fmt.Sprintf(s.sought, where(target), clock(to)), nil
}

// current is the place in the song now (decision 43): while playing, the
// reported position plus the time since it was reported; else the position.
func current(o Options, st State) float64 {
	if st.State != "playing" || st.PositionAt.IsZero() {
		return st.Position
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	return st.Position + now().Sub(st.PositionAt).Seconds()
}

// clock writes seconds as m:ss, from an hour as h:mm:ss.
func clock(seconds float64) string {
	t := int(math.Round(seconds))
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t%3600/60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}
