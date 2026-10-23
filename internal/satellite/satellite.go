// Package satellite is what Mira does at the satellite she is talking at
// besides speaking (docs/spec/satellit.md): sound_play plays one of her own
// sounds on its speaker, display_card puts a card on its screen,
// keep_listening has it listen again after the answer. Both go to
// the satellite of the turn only; where it cannot, nothing happens.
package satellite

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

const (
	ToolSound = "sound_play"
	ToolCard  = "display_card"
)

// SoundPath is where Home Assistant serves the sounds to satellites: the
// integration's open page path, which passes them through from Mira's
// /ui/sounds/.
const SoundPath = "/api/mira/panel/ui/sounds/"

// afterSound is how long the tool waits past a sound's end, so Mira's answer
// does not cut its last note.
const afterSound = 500 * time.Millisecond

// cardTag marks Mira's cards among the device's notifications.
const cardTag = "mira-card"

//go:embed sounds/*.mp3
var files embed.FS

// Sound is one of Mira's sounds, named by what it means.
type Sound struct {
	Name   string
	Length time.Duration
}

// Sounds are the sounds Mira brings; their sources are in sounds/SOURCES.md.
var Sounds = []Sound{
	{Name: "fanfare_big", Length: 4650 * time.Millisecond},
	{Name: "fanfare", Length: 1740 * time.Millisecond},
	{Name: "too_bad", Length: 1760 * time.Millisecond},
}

// File is a sound's MP3; false for a name that is no sound of Mira's.
func File(name string) ([]byte, bool) {
	if _, ok := find(name); !ok {
		return nil, false
	}
	data, err := files.ReadFile("sounds/" + name + ".mp3")
	return data, err == nil
}

func find(name string) (Sound, bool) {
	for _, s := range Sounds {
		if s.Name == name {
			return s, true
		}
	}
	return Sound{}, false
}

// Card is what display_card puts on a screen.
type Card struct {
	Title, Line, Picture string
	Seconds              int
}

type Options struct {
	Lang i18n.Lang
	// Speaker is the media player of a satellite's device; "" if none.
	Speaker func(ctx context.Context, deviceID string) string
	// Play plays an address on a media player.
	Play func(ctx context.Context, entity, url string) error
	// BaseURL is Home Assistant's address as satellites reach it.
	BaseURL func(ctx context.Context) string
	// CardAction is the card action of a satellite's device
	// ("esphome.show_notify_picture"); "" if it has none.
	CardAction func(ctx context.Context, deviceID string) string
	// Show calls a card action.
	Show func(ctx context.Context, action string, c Card) error
	// Wait waits for a sound to end; nil waits for real.
	Wait func(ctx context.Context, d time.Duration) error
}

type texts struct {
	soundDesc, nameDesc, sounds                     string
	cardDesc, titleDesc, lineDesc, picDesc, secDesc string
	noSatellite, noSpeaker, unknown, played         string
	noScreen, noPicture, shown                      string
	listenDesc, listening                           string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		soundDesc:   "Spielt einen kurzen Klang von Mira auf dem Satelliten, an dem gerade gesprochen wird, und kommt erst zurück, wenn er zu Ende ist; sprich danach. Nur dort, wo ein Skill es sagt. Klänge: %s.",
		nameDesc:    "Name des Klangs.",
		sounds:      "fanfare_big (großer Erfolg, alles richtig), fanfare (Erfolg), too_bad (hat nicht geklappt)",
		cardDesc:    "Zeigt eine Karte auf dem Bildschirm des Satelliten, an dem gerade gesprochen wird: ein Bild über den ganzen Bildschirm, darüber ein Titel und eine Zeile. Zum Hinschauen, nie der einzige Ort einer Auskunft: Was darauf steht, sagst du auch. Eine neue Karte ersetzt die alte. Nur dort, wo ein Skill es sagt.",
		titleDesc:   "Titel, kurz (höchstens 40 Zeichen).",
		lineDesc:    "Eine Zeile, ein Stichwort statt eines Satzes (etwa 50 Zeichen; mehr wird abgeschnitten).",
		picDesc:     "Adresse des Bilds, https://…, JPEG oder PNG.",
		secDesc:     "Wie lange die Karte stehen bleibt, in Sekunden; 0 bis jemand sie antippt oder eine neue kommt.",
		noSatellite: "Hier gibt es keinen Satelliten (kein Gespräch per Sprache). Mach ohne weiter und erwähne es nicht.",
		noSpeaker:   "Der Satellit hat keinen Lautsprecher, auf dem Mira Klänge spielen kann. Mach ohne weiter und erwähne es nicht.",
		unknown:     "Einen Klang „%s“ gibt es nicht. Klänge: %s.",
		played:      "Gespielt und zu Ende. Sprich jetzt.",
		noScreen:    "Dieser Satellit hat keinen Bildschirm für Karten. Mach ohne weiter und erwähne es nicht.",
		noPicture:   "Das Bild braucht eine Adresse mit https:// oder http://.",
		shown:       "Die Karte steht auf dem Bildschirm.",
		listenDesc:  "Lässt den Satelliten nach deiner Antwort noch einmal zuhören, ohne Aufwachwort, wenn du gleich ein kurzes Wort erwartest, ohne zu fragen („weiter“ nach einem kurzen Handgriff). Nach einer Frage hört er ohnehin zu. Nur dort, wo ein Skill es sagt.",
		listening:   "Der Satellit hört nach deiner Antwort noch einmal zu. Frag deshalb nicht extra.",
	},
	i18n.EN: {
		soundDesc:   "Plays a short sound of Mira's on the satellite being spoken at and returns only when it is over; speak after it. Only where a skill says so. Sounds: %s.",
		nameDesc:    "The sound's name.",
		sounds:      "fanfare_big (a big success, all right), fanfare (a success), too_bad (it did not work out)",
		cardDesc:    "Shows a card on the screen of the satellite being spoken at: a picture over the whole screen, with a title and one line over it. Something to glance at, never the only place a thing is said: say what is on it as well. A new card replaces the one up. Only where a skill says so.",
		titleDesc:   "Title, short (at most 40 characters).",
		lineDesc:    "One line, a note rather than a sentence (about 50 characters; more is cut off).",
		picDesc:     "The picture's address, https://…, JPEG or PNG.",
		secDesc:     "How long the card stays, in seconds; 0 until somebody taps it or a new one comes.",
		noSatellite: "There is no satellite here (no voice conversation). Go on without it and do not mention it.",
		noSpeaker:   "The satellite has no speaker Mira can play sounds on. Go on without it and do not mention it.",
		unknown:     "There is no sound \"%s\". Sounds: %s.",
		played:      "Played and over. Speak now.",
		noScreen:    "This satellite has no screen for cards. Go on without it and do not mention it.",
		noPicture:   "The picture needs an address with https:// or http://.",
		shown:       "The card is on the screen.",
		listenDesc:  "Makes the satellite listen again after your answer, without the wake word, when you expect a quick word without asking (\"next\" after a short step). After a question it listens anyway. Only where a skill says so.",
		listening:   "The satellite listens again after your answer, so you need not ask.",
	},
}

// Tools are sound_play and display_card.
func Tools(o Options) []tools.InternalTool {
	t := i18n.Pick(catalog, o.Lang)
	if o.Wait == nil {
		o.Wait = wait
	}
	return []tools.InternalTool{soundTool(o, t), cardTool(o, t), listenTool(t)}
}

// listenTool is keep_listening; the conversation engine sees the call and
// keeps the satellite listening after the answer.
func listenTool(t texts) tools.InternalTool {
	return tools.InternalTool{
		Name: tools.ToolKeepListening, Description: t.listenDesc, Schema: `{"type":"object","properties":{}}`,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			if inv.DeviceID == "" {
				return t.noSatellite, nil
			}
			return t.listening, nil
		},
	}
}

func soundTool(o Options, t texts) tools.InternalTool {
	schema := fmt.Sprintf(`{"type":"object","properties":{"name":{"type":"string","description":%q}},"required":["name"]}`, t.nameDesc)
	return tools.InternalTool{
		Name: ToolSound, Description: fmt.Sprintf(t.soundDesc, t.sounds), Schema: schema,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var args struct{ Name string }
			if err := json.Unmarshal(inv.Args, &args); err != nil {
				return "", errors.New("name is required")
			}
			s, ok := find(strings.TrimSpace(args.Name))
			if !ok {
				return fmt.Sprintf(t.unknown, args.Name, t.sounds), nil
			}
			if inv.DeviceID == "" {
				return t.noSatellite, nil
			}
			speaker := o.Speaker(ctx, inv.DeviceID)
			if speaker == "" {
				return t.noSpeaker, nil
			}
			url := strings.TrimRight(o.BaseURL(ctx), "/") + SoundPath + s.Name + ".mp3"
			if err := o.Play(ctx, speaker, url); err != nil {
				return "", err
			}
			if err := o.Wait(ctx, s.Length+afterSound); err != nil {
				return "", err
			}
			return t.played, nil
		},
	}
}

func cardTool(o Options, t texts) tools.InternalTool {
	schema := fmt.Sprintf(`{"type":"object","properties":{"title":{"type":"string","description":%q},"line":{"type":"string","description":%q},"picture":{"type":"string","description":%q},"seconds":{"type":"integer","description":%q}},"required":["title","line","picture"]}`,
		t.titleDesc, t.lineDesc, t.picDesc, t.secDesc)
	return tools.InternalTool{
		Name: ToolCard, Description: t.cardDesc, Schema: schema,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var c Card
			if err := json.Unmarshal(inv.Args, &c); err != nil {
				return "", errors.New("title, line and picture are required")
			}
			c.Picture = strings.TrimSpace(c.Picture)
			if !strings.HasPrefix(c.Picture, "https://") && !strings.HasPrefix(c.Picture, "http://") {
				return t.noPicture, nil
			}
			if inv.DeviceID == "" {
				return t.noSatellite, nil
			}
			action := o.CardAction(ctx, inv.DeviceID)
			if action == "" {
				return t.noScreen, nil
			}
			if err := o.Show(ctx, action, c); err != nil {
				return "", err
			}
			return t.shown, nil
		},
	}
}

// CardData is a card as the TECHO5 action takes it: every field, without a
// chime and not spoken, since Mira says it herself.
func CardData(c Card) map[string]any {
	return map[string]any{
		"title": c.Title, "message": c.Line, "picture": c.Picture,
		"chime": "off", "speak": "off", "sound": "", "tag": cardTag, "seconds": max(c.Seconds, 0),
	}
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
