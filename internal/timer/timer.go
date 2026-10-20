// Package timer has two tools, neither keeps a timer of its own.
//
// sleep_timer is "stop in 30 minutes" (docs/spec/geplante-aktionen.md): Mira
// asks the Mira integration to put a one-time automation into Home
// Assistant, which runs it – even when Mira is off – and the integration
// tidies it away afterwards.
//
// timer is the countdown timer ("timer for 10 minutes for the pasta"): the
// timers of Home Assistant itself, started on the satellite that heard the
// request. The satellite counts, shows and rings; Mira only speaks for it
// (the model providers of Home Assistant get these tools from its own
// conversation API, Mira's route through MCP does not).
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
	// ToolSleep is the sleep timer (a one-time automation in Home Assistant),
	// ToolTimer the countdown timer (Home Assistant's own timers on the satellite).
	ToolSleep = "sleep_timer"
	ToolTimer = "timer"

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
	// Intents runs Home Assistant's timer intents; DeviceOf gives the
	// satellite a voice session came from ("" for none).
	Intents  Intents
	DeviceOf func(ctx context.Context, sessionKey string) string
}

type texts struct {
	sleepDesc, actionDesc, minutesDesc, roomDesc, lightsDesc, idDesc                             string
	noRoom, nothingToStop, set, setLights, none, listLine, several, noMatch, extended, cancelled string
	badMinutes, title, failed, badAction                                                         string

	timerDesc, tActionDesc, hoursDesc, tMinutesDesc, secondsDesc, nameDesc, allDesc string
	noDevice, tNeedsTime, started, done, tNotFound, tSeveral, tNone, tLine, tPaused string
	tFailed, tCanceledAll, tCanceled                                                string
	hourWord, hoursWord, minuteWord, minutesWord, secondWord, secondsWord, andWord  string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		sleepDesc:     "Schlaf-Timer: nach der Zeit hört im Raum die Wiedergabe auf (Hörbuch, Radio, Musik), auf Wunsch geht auch das Licht aus. action: set (Standard) stellt ihn („Schlaf-Timer 30 Minuten“, „schalte in einer halben Stunde die Musik aus“; ein vorhandener im Raum wird ersetzt), list zeigt die offenen mit Restzeit, extend verlängert („noch zehn Minuten“), cancel bricht ab. Läuft in Home Assistant, auch wenn Mira aus ist. Nicht für Küchen-Timer zum Herunterzählen: dafür timer.",
		actionDesc:    "set, list, extend oder cancel.",
		minutesDesc:   "set: nach wie vielen Minuten der Timer abläuft. extend: um wie viele Minuten er länger läuft.",
		roomDesc:      "Raum; weglassen für den Raum des Nutzers.",
		lightsDesc:    "set: true nur, wenn der Nutzer sagt, dass auch das Licht ausgehen soll.",
		idDesc:        "extend und cancel: Kennung aus list; weglassen, wenn es nur einen Timer gibt.",
		noRoom:        "Ich weiß nicht, in welchem Raum der Timer gelten soll. Frag den Nutzer danach.",
		nothingToStop: "Im Raum %s ist weder ein Lautsprecher noch ein Licht für Mira freigegeben, also gäbe es nichts zu tun. Sag das dem Nutzer.",
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
		failed:        "Der Schlaf-Timer konnte nicht angelegt werden: %s. Sag das dem Nutzer ehrlich.",
		badAction:     "Unbekannte action; erlaubt sind set, list, extend, cancel.",

		timerDesc:    "Timer zum Herunterzählen auf dem Lautsprecher, an dem gesprochen wird: Er zählt, zeigt und klingelt dort („Timer auf 10 Minuten für die Nudeln“). action: start, status („wie lange noch?“), cancel, add („noch 5 Minuten dazu“), remove, pause, resume. Mehrere Timer unterscheidet der Name. Nicht für Erinnerungen mit Text (reminder_set) und nicht für das Ausschalten nach einer Zeit (sleep_timer).",
		tActionDesc:  "start, status, cancel, add, remove, pause oder resume.",
		hoursDesc:    "Stunden (start; bei add und remove die Menge).",
		tMinutesDesc: "Minuten (start; bei add und remove die Menge).",
		secondsDesc:  "Sekunden (start; bei add und remove die Menge).",
		nameDesc:     "Name des Timers („Nudeln“): bei start vergeben, sonst zum Auswählen, wenn es mehrere gibt.",
		allDesc:      "cancel: true bricht alle Timer des Lautsprechers ab.",
		noDevice:     "Hier gibt es keinen Lautsprecher mit Timern: Timer laufen auf dem Satelliten, an dem gesprochen wird. Sag das dem Nutzer.",
		tNeedsTime:   "Für start und für add und remove braucht es eine Dauer (hours, minutes oder seconds).",
		started:      "Timer gestellt: %s.",
		done:         "Erledigt.",
		tNotFound:    "Einen solchen Timer gibt es nicht.",
		tSeveral:     "Es laufen mehrere Timer. Frag den Nutzer, welcher gemeint ist (status zeigt die Namen), und ruf das Werkzeug mit name auf.",
		tNone:        "Es läuft kein Timer.",
		tLine:        "%s: noch %s%s",
		tPaused:      " (angehalten)",
		tFailed:      "Der Timer geht nicht: %s. Sag das dem Nutzer ehrlich.",
		tCanceledAll: "%d Timer abgebrochen.",
		tCanceled:    "Timer abgebrochen.",
		hourWord:     "Stunde", hoursWord: "Stunden", minuteWord: "Minute", minutesWord: "Minuten",
		secondWord: "Sekunde", secondsWord: "Sekunden", andWord: "und",
	},
	i18n.EN: {
		sleepDesc:     "Sleep timer: after the time the playback in the room stops (audiobook, radio, music), and on request the light goes off too. action: set (default) sets it (\"sleep timer 30 minutes\", \"turn the music off in half an hour\"; an existing one in the room is replaced), list shows the open ones with the time left, extend extends (\"ten more minutes\"), cancel cancels. Runs in Home Assistant, even when Mira is off. Not for kitchen timers that count down: that is timer.",
		actionDesc:    "set, list, extend or cancel.",
		minutesDesc:   "set: after how many minutes the timer ends. extend: by how many minutes it runs longer.",
		roomDesc:      "Room; leave out for the user's room.",
		lightsDesc:    "set: true only if the user says the light should go off too.",
		idDesc:        "extend and cancel: id from list; leave out if there is only one timer.",
		noRoom:        "I do not know which room the timer is for. Ask the user.",
		nothingToStop: "In %s no speaker and no light is released for Mira, so there would be nothing to do. Tell the user.",
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
		failed:        "The sleep timer could not be set: %s. Tell the user honestly.",
		badAction:     "Unknown action; allowed are set, list, extend, cancel.",

		timerDesc:    "Countdown timer on the speaker the user is talking to: it counts, shows and rings there (\"timer for 10 minutes for the pasta\"). action: start, status (\"how long left?\"), cancel, add (\"five more minutes\"), remove, pause, resume. Several timers are told apart by name. Not for reminders with a text (reminder_set) and not for switching off after a time (sleep_timer).",
		tActionDesc:  "start, status, cancel, add, remove, pause or resume.",
		hoursDesc:    "Hours (start; for add and remove the amount).",
		tMinutesDesc: "Minutes (start; for add and remove the amount).",
		secondsDesc:  "Seconds (start; for add and remove the amount).",
		nameDesc:     "Name of the timer (\"pasta\"): given at start, otherwise to pick one when there are several.",
		allDesc:      "cancel: true cancels all timers of the speaker.",
		noDevice:     "There is no speaker with timers here: timers run on the satellite the user is talking to. Tell the user.",
		tNeedsTime:   "start, add and remove need a duration (hours, minutes or seconds).",
		started:      "Timer set: %s.",
		done:         "Done.",
		tNotFound:    "There is no such timer.",
		tSeveral:     "Several timers are running. Ask the user which one is meant (status shows the names) and call the tool with name.",
		tNone:        "No timer is running.",
		tLine:        "%s: %s left%s",
		tPaused:      " (paused)",
		tFailed:      "The timer does not work: %s. Tell the user honestly.",
		tCanceledAll: "%d timers cancelled.",
		tCanceled:    "Timer cancelled.",
		hourWord:     "hour", hoursWord: "hours", minuteWord: "minute", minutesWord: "minutes",
		secondWord: "second", secondsWord: "seconds", andWord: "and",
	},
}

// Tools are the sleep timer and the countdown timer.
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
		{
			Name: ToolTimer, Description: t.timerDesc,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"action":{"type":"string","enum":["start","status","cancel","add","remove","pause","resume"],"description":%q},"hours":{"type":"integer","description":%q},"minutes":{"type":"integer","description":%q},"seconds":{"type":"integer","description":%q},"name":{"type":"string","description":%q},"all":{"type":"boolean","description":%q}},"required":["action"]}`,
				t.tActionDesc, t.hoursDesc, t.tMinutesDesc, t.secondsDesc, t.nameDesc, t.allDesc),
			Run: c.countdown,
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
