// Package timer is the sleep timer: "stop in 30 minutes" (docs/spec/geplante-aktionen.md).
// Mira does not keep the timer. She asks the Mira integration to put a
// one-time automation into Home Assistant, which runs it – even when Mira is
// off – and the integration tidies it away afterwards.
package timer

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

const (
	ToolSleep  = "sleep_timer"
	ToolList   = "timer_list"
	ToolExtend = "timer_extend"
	ToolCancel = "timer_cancel"

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

// Timer is one open timer as the integration reports it.
type Timer struct {
	ID      string           `json:"id"`
	Title   string           `json:"title"`
	Due     string           `json:"due"`
	Owner   string           `json:"owner"`
	Area    string           `json:"area"`
	Kind    string           `json:"kind"`
	Actions []map[string]any `json:"actions"`
	Ran     bool             `json:"ran"`
}

type texts struct {
	sleepDesc, minutesDesc, roomDesc, lightsDesc, listDesc, extendDesc, addDesc, idDesc, cancelDesc string
	noRoom, nothingToStop, set, setLights, none, listLine, several, noMatch, extended, cancelled    string
	badMinutes, title, failed                                                                       string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		sleepDesc:     "Stellt einen Schlaf-Timer: nach der Zeit hört im Raum die Wiedergabe auf (Hörbuch, Radio, Musik), auf Wunsch geht auch das Licht aus. Für „Schlaf-Timer 30 Minuten“, „Schalte in einer halben Stunde die Musik aus“. Ein vorhandener Schlaf-Timer im Raum wird ersetzt. Läuft in Home Assistant, auch wenn Mira aus ist.",
		minutesDesc:   "Nach wie vielen Minuten der Timer ablaufen soll.",
		roomDesc:      "Raum; weglassen für den Raum des Nutzers.",
		lightsDesc:    "true nur, wenn der Nutzer sagt, dass auch das Licht ausgehen soll.",
		listDesc:      "Zeigt die offenen Timer des Nutzers im Raum mit der Restzeit. Erinnerungen stehen in reminder_list.",
		extendDesc:    "Verlängert einen Timer („noch zehn Minuten“). Ohne id gilt der Timer des Nutzers im Raum.",
		addDesc:       "Um wie viele Minuten der Timer länger läuft.",
		idDesc:        "Kennung aus timer_list; weglassen, wenn es nur einen Timer gibt.",
		cancelDesc:    "Bricht einen Timer ab („doch nicht“). Ohne id gilt der Timer des Nutzers im Raum.",
		noRoom:        "Ich weiß nicht, in welchem Raum der Timer gelten soll. Frag den Nutzer danach.",
		nothingToStop: "Im Raum %s ist weder ein Lautsprecher noch ein Licht für Mira freigegeben, also gäbe es nichts zu tun. Sag das dem Nutzer.",
		set:           "Schlaf-Timer gestellt: %s (in %d Minuten) hört im Raum %s die Wiedergabe auf.",
		setLights:     "Schlaf-Timer gestellt: %s (in %d Minuten) hört im Raum %s die Wiedergabe auf und das Licht geht aus.",
		none:          "Keine offenen Timer.",
		listLine:      "%s: %s, %s (in %d Minuten), Raum %s",
		several:       "Es gibt mehrere Timer: %s. Frag den Nutzer, welcher es sein soll, und ruf das Werkzeug dann mit id auf.",
		noMatch:       "Dazu gibt es keinen offenen Timer.",
		extended:      "Der Timer läuft jetzt bis %s (in %d Minuten).",
		cancelled:     "Der Timer ist abgebrochen.",
		badMinutes:    "Die Minuten müssen mehr als 0 sein.",
		title:         "Schlaf-Timer %s",
		failed:        "Der Timer konnte nicht angelegt werden: %s. Sag das dem Nutzer ehrlich.",
	},
	i18n.EN: {
		sleepDesc:     "Sets a sleep timer: after the time the playback in the room stops (audiobook, radio, music), and on request the light goes off too. For \"sleep timer 30 minutes\", \"turn the music off in half an hour\". An existing sleep timer in the room is replaced. Runs in Home Assistant, even when Mira is off.",
		minutesDesc:   "After how many minutes the timer ends.",
		roomDesc:      "Room; leave out for the user's room.",
		lightsDesc:    "true only if the user says the light should go off too.",
		listDesc:      "Shows the user's open timers in the room with the time left. Reminders are in reminder_list.",
		extendDesc:    "Extends a timer (\"ten more minutes\"). Without id it is the user's timer in the room.",
		addDesc:       "By how many minutes the timer runs longer.",
		idDesc:        "Id from timer_list; leave out if there is only one timer.",
		cancelDesc:    "Cancels a timer (\"never mind\"). Without id it is the user's timer in the room.",
		noRoom:        "I do not know which room the timer is for. Ask the user.",
		nothingToStop: "In %s no speaker and no light is released for Mira, so there would be nothing to do. Tell the user.",
		set:           "Sleep timer set: %s (in %d minutes) the playback in %s stops.",
		setLights:     "Sleep timer set: %s (in %d minutes) the playback in %s stops and the light goes off.",
		none:          "No open timers.",
		listLine:      "%s: %s, %s (in %d minutes), room %s",
		several:       "There are several timers: %s. Ask the user which one, then call the tool with id.",
		noMatch:       "There is no open timer for that.",
		extended:      "The timer now runs until %s (in %d minutes).",
		cancelled:     "The timer is cancelled.",
		badMinutes:    "The minutes must be more than 0.",
		title:         "Sleep timer %s",
		failed:        "The timer could not be set: %s. Tell the user honestly.",
	},
}

// Tools are the four timer tools.
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
			Schema: fmt.Sprintf(`{"type":"object","properties":{"minutes":{"type":"integer","description":%q},"room":{"type":"string","description":%q},"lights_off":{"type":"boolean","description":%q}},"required":["minutes"]}`,
				t.minutesDesc, t.roomDesc, t.lightsDesc),
			Run: c.sleep,
		},
		{
			Name: ToolList, Description: t.listDesc, ReadOnly: true,
			Schema: `{"type":"object","properties":{}}`,
			Run:    c.list,
		},
		{
			Name: ToolExtend, Description: t.extendDesc,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"add_minutes":{"type":"integer","description":%q},"id":{"type":"string","description":%q}},"required":["add_minutes"]}`,
				t.addDesc, t.idDesc),
			Run: c.extend,
		},
		{
			Name: ToolCancel, Description: t.cancelDesc,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"id":{"type":"string","description":%q}}}`, t.idDesc),
			Run:    c.cancel,
		},
	}
}

type controller struct {
	o Options
	t texts
}

func (c *controller) sleep(ctx context.Context, inv tools.Invocation) (string, error) {
	var a struct {
		Minutes   int    `json:"minutes"`
		Room      string `json:"room"`
		LightsOff bool   `json:"lights_off"`
	}
	if err := json.Unmarshal(inv.Args, &a); err != nil {
		return "", err
	}
	if a.Minutes < 1 {
		return c.t.badMinutes, nil
	}
	area, ok := c.area(inv, a.Room)
	if !ok {
		return c.t.noRoom, nil
	}
	room := c.o.AreaName(area)
	var actions []map[string]any
	if players := c.o.Players(area); len(players) > 0 {
		actions = append(actions, map[string]any{"action": "media_player.media_stop", "target": map[string]any{"entity_id": players}})
	}
	lights := a.LightsOff && len(c.o.Lights(area)) > 0
	if lights {
		actions = append(actions, map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": c.o.Lights(area)}})
	}
	if len(actions) == 0 {
		return fmt.Sprintf(c.t.nothingToStop, room), nil
	}
	due := c.o.Clock().Add(time.Duration(a.Minutes) * time.Minute)
	msg := map[string]any{
		"type": "mira/once/save", "title": fmt.Sprintf(c.t.title, room), "due": due.Format(time.RFC3339),
		"owner": inv.Person, "area": area, "kind": kindSleep, "actions": actions, "forbidden": c.forbidden(),
	}
	// A second sleep timer in the room replaces the first.
	if open, err := c.open(ctx); err == nil {
		for _, tm := range open {
			if tm.Kind == kindSleep && tm.Owner == inv.Person && tm.Area == area {
				msg["automation_id"] = tm.ID
			}
		}
	}
	if _, err := c.o.HA.Call(ctx, msg); err != nil {
		return fmt.Sprintf(c.t.failed, err), nil
	}
	format := c.t.set
	if lights {
		format = c.t.setLights
	}
	return fmt.Sprintf(format, c.clock(due), a.Minutes, room), nil
}

func (c *controller) list(ctx context.Context, inv tools.Invocation) (string, error) {
	mine, err := c.mine(ctx, inv)
	if err != nil {
		return "", err
	}
	if len(mine) == 0 {
		return c.t.none, nil
	}
	var b strings.Builder
	for _, tm := range mine {
		due, _ := time.Parse(time.RFC3339, tm.Due)
		fmt.Fprintf(&b, "- "+c.t.listLine+"\n", tm.ID, tm.Title, c.clock(due), c.minutesLeft(due), c.o.AreaName(tm.Area))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (c *controller) extend(ctx context.Context, inv tools.Invocation) (string, error) {
	var a struct {
		AddMinutes int    `json:"add_minutes"`
		ID         string `json:"id"`
	}
	if err := json.Unmarshal(inv.Args, &a); err != nil {
		return "", err
	}
	if a.AddMinutes < 1 {
		return c.t.badMinutes, nil
	}
	tm, answer, err := c.pick(ctx, inv, a.ID)
	if tm == nil {
		return answer, err
	}
	due, _ := time.Parse(time.RFC3339, tm.Due)
	if now := c.o.Clock(); due.Before(now) {
		due = now // due but not yet run (Home Assistant was off)
	}
	due = due.Add(time.Duration(a.AddMinutes) * time.Minute)
	if _, err := c.o.HA.Call(ctx, map[string]any{
		"type": "mira/once/save", "title": tm.Title, "due": due.Format(time.RFC3339), "owner": tm.Owner,
		"area": tm.Area, "kind": tm.Kind, "actions": tm.Actions, "forbidden": c.forbidden(), "automation_id": tm.ID,
	}); err != nil {
		return fmt.Sprintf(c.t.failed, err), nil
	}
	return fmt.Sprintf(c.t.extended, c.clock(due), c.minutesLeft(due)), nil
}

func (c *controller) cancel(ctx context.Context, inv tools.Invocation) (string, error) {
	var a struct {
		ID string `json:"id"`
	}
	if len(inv.Args) > 0 {
		if err := json.Unmarshal(inv.Args, &a); err != nil {
			return "", err
		}
	}
	tm, answer, err := c.pick(ctx, inv, a.ID)
	if tm == nil {
		return answer, err
	}
	if _, err := c.o.HA.Call(ctx, map[string]any{"type": "mira/once/delete", "automation_id": tm.ID}); err != nil {
		return fmt.Sprintf(c.t.failed, err), nil
	}
	return c.t.cancelled, nil
}

// pick chooses the timer meant: the given id, else the only one of the
// user in the room. With several, it returns the question for the model.
func (c *controller) pick(ctx context.Context, inv tools.Invocation, id string) (*Timer, string, error) {
	mine, err := c.mine(ctx, inv)
	if err != nil {
		return nil, "", err
	}
	if id != "" {
		for i := range mine {
			if mine[i].ID == id {
				return &mine[i], "", nil
			}
		}
		return nil, c.t.noMatch, nil
	}
	switch len(mine) {
	case 0:
		return nil, c.t.noMatch, nil
	case 1:
		return &mine[0], "", nil
	}
	var names []string
	for _, tm := range mine {
		due, _ := time.Parse(time.RFC3339, tm.Due)
		names = append(names, fmt.Sprintf("%s (%s, %s)", tm.ID, tm.Title, c.clock(due)))
	}
	return nil, fmt.Sprintf(c.t.several, strings.Join(names, "; ")), nil
}

// mine are the open timers of the person (anonymous: of the room), the room
// first when the user is in one.
func (c *controller) mine(ctx context.Context, inv tools.Invocation) ([]Timer, error) {
	open, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	var out []Timer
	for _, tm := range open {
		own := tm.Owner == inv.Person && (inv.Person != "" || tm.Area == inv.Area)
		if own {
			out = append(out, tm)
		}
	}
	slices.SortFunc(out, func(a, b Timer) int { return strings.Compare(a.Due, b.Due) })
	return out, nil
}

func (c *controller) open(ctx context.Context) ([]Timer, error) {
	raw, err := c.o.HA.Call(ctx, map[string]any{"type": "mira/once/list"})
	if err != nil {
		return nil, err
	}
	var res struct {
		Timers []Timer `json:"timers"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	var out []Timer
	for _, tm := range res.Timers {
		if !tm.Ran {
			out = append(out, tm)
		}
	}
	return out, nil
}

func (c *controller) area(inv tools.Invocation, room string) (string, bool) {
	if r := strings.TrimSpace(room); r != "" {
		area := c.o.Area(strings.ToLower(r))
		return area, area != ""
	}
	return inv.Area, inv.Area != ""
}

func (c *controller) forbidden() map[string]any {
	if c.o.Forbidden == nil {
		return map[string]any{"domains": []string{}, "entities": []string{}}
	}
	return c.o.Forbidden()
}

func (c *controller) clock(t time.Time) string {
	return t.In(c.o.Location).Format("15:04")
}

func (c *controller) minutesLeft(due time.Time) int {
	left := int(due.Sub(c.o.Clock()).Round(time.Minute) / time.Minute)
	return max(left, 0)
}
