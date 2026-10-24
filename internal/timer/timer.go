// Package timer is the sleep timer: "stop in 30 minutes" (docs/spec/geplante-aktionen.md).
// Mira does not keep the timer. She asks the Mira integration to put a
// one-time automation into Home Assistant, which runs it – even when Mira is
// off – and the integration tidies it away afterwards.
//
// Countdown timers ("timer for 10 minutes for the pasta") are not here: they
// are Home Assistant's own, and Mira uses its tools (HassStartTimer and the
// others), which its MCP server offers for a satellite that has timers.
package timer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

const (
	// ToolSleep is the sleep timer (a one-time automation in Home Assistant).
	ToolSleep = "sleep_timer"

	kindSleep = "sleep"
)

// HA is the part of the Home Assistant client the timers use.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
}

type Options struct {
	Lang     i18n.Lang
	HA       HA
	Clock    func() time.Time
	Location *time.Location
	// Players and Lights are the exposed media players and lights in an area id.
	Players func(area string) []string
	Lights  func(area string) []string
	// Area resolves what the model says ("Küche") to an area id; "" if unknown.
	Area func(name string) string
	// AreaName is the display name of an area id.
	AreaName func(area string) string
	// Forbidden is what the integration refuses in a timer's actions.
	Forbidden func() map[string]any
}

type texts struct {
	sleepDesc, actionDesc, minutesDesc, roomDesc, lightsDesc, idDesc                             string
	noRoom, nothingToStop, set, setLights, none, listLine, several, noMatch, extended, cancelled string
	badMinutes, title, failed, badAction                                                         string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		sleepDesc:     "Schlaf-Timer: nach der Zeit hört im Raum die Wiedergabe auf (Hörbuch, Radio, Musik), auf Wunsch geht auch das Licht aus. action: set (Standard) stellt ihn („Schlaf-Timer 30 Minuten“, „schalte in einer halben Stunde die Musik aus“; ein vorhandener im Raum wird ersetzt), list zeigt die offenen mit Restzeit, extend verlängert („noch zehn Minuten“), cancel bricht ab. Läuft in Home Assistant, auch wenn Mira aus ist. Nicht für Küchen-Timer zum Herunterzählen: dafür gibt es die Timer-Werkzeuge von Home Assistant (HassStartTimer und Verwandte).",
		actionDesc:    "set, list, extend oder cancel.",
		minutesDesc:   "set: nach wie vielen Minuten der Timer abläuft. extend: um wie viele Minuten er länger läuft.",
		roomDesc:      "Raum; weglassen für den Raum des Nutzers.",
		lightsDesc:    "set: true nur, wenn der Nutzer sagt, dass auch das Licht ausgehen soll.",
		idDesc:        "extend und cancel: Kennung aus list; weglassen, wenn es nur einen Timer gibt.",
		noRoom:        "Ich weiß nicht, in welchem Raum der Timer gelten soll. Frag den Nutzer danach.",
		nothingToStop: "Ergebnis: kein Timer. Grund: Im Raum %s ist weder ein Lautsprecher noch ein Licht für Mira freigegeben, es gäbe nichts zu tun.",
		set:           "Schlaf-Timer gestellt: %s (in %d Minuten) hört im Raum %s die Wiedergabe auf.",
		setLights:     "Schlaf-Timer gestellt: %s (in %d Minuten) hört im Raum %s die Wiedergabe auf und das Licht geht aus.",
		none:          "Keine offenen Schlaf-Timer.",
		listLine:      "%s: %s, %s (in %d Minuten), Raum %s",
		several:       "Es gibt mehrere Schlaf-Timer: %s. Frag den Nutzer, welcher es sein soll, und ruf das Werkzeug dann mit id auf.",
		noMatch:       "Dazu gibt es keinen offenen Schlaf-Timer.",
		extended:      "Der Schlaf-Timer läuft jetzt bis %s (in %d Minuten).",
		cancelled:     "Der Schlaf-Timer ist abgebrochen.",
		badMinutes:    "Die Minuten müssen mehr als 0 sein.",
		title:         "Schlaf-Timer %s",
		failed:        "Ergebnis: kein Timer. Grund: Anlegen fehlgeschlagen: %s.",
		badAction:     "Unbekannte action; erlaubt sind set, list, extend, cancel.",
	},
	i18n.EN: {
		sleepDesc:     "Sleep timer: after the time the playback in the room stops (audiobook, radio, music), and on request the light goes off too. action: set (default) sets it (\"sleep timer 30 minutes\", \"turn the music off in half an hour\"; an existing one in the room is replaced), list shows the open ones with the time left, extend extends (\"ten more minutes\"), cancel cancels. Runs in Home Assistant, even when Mira is off. Not for kitchen timers that count down: Home Assistant's timer tools (HassStartTimer and the others) are for that.",
		actionDesc:    "set, list, extend or cancel.",
		minutesDesc:   "set: after how many minutes the timer ends. extend: by how many minutes it runs longer.",
		roomDesc:      "Room; leave out for the user's room.",
		lightsDesc:    "set: true only if the user says the light should go off too.",
		idDesc:        "extend and cancel: id from list; leave out if there is only one timer.",
		noRoom:        "I do not know which room the timer is for. Ask the user.",
		nothingToStop: "Result: no timer. Reason: in %s no speaker and no light is released for Mira, so there would be nothing to do.",
		set:           "Sleep timer set: %s (in %d minutes) the playback in %s stops.",
		setLights:     "Sleep timer set: %s (in %d minutes) the playback in %s stops and the light goes off.",
		none:          "No open sleep timers.",
		listLine:      "%s: %s, %s (in %d minutes), room %s",
		several:       "There are several sleep timers: %s. Ask the user which one, then call the tool with id.",
		noMatch:       "There is no open sleep timer for that.",
		extended:      "The sleep timer now runs until %s (in %d minutes).",
		cancelled:     "The sleep timer is cancelled.",
		badMinutes:    "The minutes must be more than 0.",
		title:         "Sleep timer %s",
		failed:        "Result: no timer. Reason: setting it failed: %s.",
		badAction:     "Unknown action; allowed are set, list, extend, cancel.",
	},
}

// Tools is the sleep timer.
func Tools(o Options) []tools.InternalTool {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	t := i18n.Pick(catalog, o.Lang)
	c := &controller{o: o, t: t}
	return []tools.InternalTool{
		{
			Name: ToolSleep, Description: t.sleepDesc,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"action":{"type":"string","enum":["set","list","extend","cancel"],"description":%q},"minutes":{"type":"integer","description":%q},"room":{"type":"string","description":%q},"lights_off":{"type":"boolean","description":%q},"id":{"type":"string","description":%q}}}`,
				t.actionDesc, t.minutesDesc, t.roomDesc, t.lightsDesc, t.idDesc),
			Run: c.sleepTimer,
		},
	}
}

type controller struct {
	o Options
	t texts
}

func (c *controller) clock(t time.Time) string { return t.In(c.o.Location).Format("15:04") }

func (c *controller) minutesLeft(due time.Time) int {
	left := int(due.Sub(c.o.Clock()).Round(time.Minute) / time.Minute)
	return max(left, 0)
}

func (c *controller) forbidden() map[string]any {
	if c.o.Forbidden == nil {
		return map[string]any{"domains": []string{}, "entities": []string{}}
	}
	return c.o.Forbidden()
}
