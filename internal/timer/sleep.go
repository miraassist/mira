package timer

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/tools"
)

// Timer is one open sleep timer as the integration reports it.
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

func (c *controller) sleepTimer(ctx context.Context, inv tools.Invocation) (string, error) {
	var a struct {
		Action    string `json:"action"`
		Minutes   int    `json:"minutes"`
		Room      string `json:"room"`
		LightsOff bool   `json:"lights_off"`
		ID        string `json:"id"`
	}
	if len(inv.Args) > 0 {
		if err := json.Unmarshal(inv.Args, &a); err != nil {
			return "", err
		}
	}
	switch a.Action {
	case "", "set":
		return c.sleepSet(ctx, inv, a.Minutes, a.Room, a.LightsOff)
	case "list":
		return c.sleepList(ctx, inv)
	case "extend":
		return c.sleepExtend(ctx, inv, a.Minutes, a.ID)
	case "cancel":
		return c.sleepCancel(ctx, inv, a.ID)
	}
	return c.t.badAction, nil
}

func (c *controller) sleepSet(ctx context.Context, inv tools.Invocation, minutes int, room string, lightsOff bool) (string, error) {
	if minutes < 1 {
		return c.t.badMinutes, nil
	}
	area, ok := c.area(inv, room)
	if !ok {
		return c.t.noRoom, nil
	}
	name := c.o.AreaName(area)
	var actions []map[string]any
	if players := c.o.Players(area); len(players) > 0 {
		actions = append(actions, map[string]any{"action": "media_player.media_stop", "target": map[string]any{"entity_id": players}})
	}
	lights := lightsOff && len(c.o.Lights(area)) > 0
	if lights {
		actions = append(actions, map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": c.o.Lights(area)}})
	}
	if len(actions) == 0 {
		return fmt.Sprintf(c.t.nothingToStop, name), nil
	}
	due := c.o.Clock().Add(time.Duration(minutes) * time.Minute)
	msg := map[string]any{
		"type": "mira/once/save", "title": fmt.Sprintf(c.t.title, name), "due": due.Format(time.RFC3339),
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
	return fmt.Sprintf(format, c.clock(due), minutes, name), nil
}

func (c *controller) sleepList(ctx context.Context, inv tools.Invocation) (string, error) {
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

func (c *controller) sleepExtend(ctx context.Context, inv tools.Invocation, add int, id string) (string, error) {
	if add < 1 {
		return c.t.badMinutes, nil
	}
	tm, answer, err := c.pick(ctx, inv, id)
	if tm == nil {
		return answer, err
	}
	due, _ := time.Parse(time.RFC3339, tm.Due)
	if now := c.o.Clock(); due.Before(now) {
		due = now // due but not yet run (Home Assistant was off)
	}
	due = due.Add(time.Duration(add) * time.Minute)
	if _, err := c.o.HA.Call(ctx, map[string]any{
		"type": "mira/once/save", "title": tm.Title, "due": due.Format(time.RFC3339), "owner": tm.Owner,
		"area": tm.Area, "kind": tm.Kind, "actions": tm.Actions, "forbidden": c.forbidden(), "automation_id": tm.ID,
	}); err != nil {
		return fmt.Sprintf(c.t.failed, err), nil
	}
	return fmt.Sprintf(c.t.extended, c.clock(due), c.minutesLeft(due)), nil
}

func (c *controller) sleepCancel(ctx context.Context, inv tools.Invocation, id string) (string, error) {
	tm, answer, err := c.pick(ctx, inv, id)
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

// mine are the open sleep timers of the person (anonymous: of the room).
func (c *controller) mine(ctx context.Context, inv tools.Invocation) ([]Timer, error) {
	open, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	var out []Timer
	for _, tm := range open {
		if tm.Owner == inv.Person && (inv.Person != "" || tm.Area == inv.Area) {
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
