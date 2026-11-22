package timer

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/ha/automation"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/tools"
)

// Timer is one sleep timer as the integration reports it.
type Timer struct {
	ID       string           `json:"id"`
	EntityID string           `json:"entity_id"`
	Title    string           `json:"title"`
	Due      string           `json:"due"`
	Owner    string           `json:"owner"`
	Area     string           `json:"area"`
	Kind     string           `json:"kind"`
	Actions  []map[string]any `json:"actions"`
	Ran      bool             `json:"ran"`
	Running  int              `json:"running"`
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
	o := once{Title: fmt.Sprintf(c.t.title, name), Due: due, Owner: inv.Person, Area: area, Kind: kindSleep, Actions: actions}
	// A second sleep timer in the room replaces the first.
	replace := ""
	if open, err := c.open(ctx); err == nil {
		for _, tm := range open {
			if tm.Kind == kindSleep && tm.Owner == inv.Person && tm.Area == area {
				replace = tm.ID
			}
		}
	}
	if err := c.save(ctx, o, replace); err != nil {
		return c.failed(err), nil
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
	if err := c.save(ctx, once{Title: tm.Title, Due: due, Owner: tm.Owner, Area: tm.Area, Kind: tm.Kind,
		Actions: tm.Actions}, tm.ID); err != nil {
		return c.failed(err), nil
	}
	return fmt.Sprintf(c.t.extended, c.clock(due), c.minutesLeft(due)), nil
}

func (c *controller) sleepCancel(ctx context.Context, inv tools.Invocation, id string) (string, error) {
	tm, answer, err := c.pick(ctx, inv, id)
	if tm == nil {
		return answer, err
	}
	if err := automation.Delete(ctx, c.o.HA, c.o.Own, c.log, tm.ID); err != nil {
		return c.failed(err), nil
	}
	c.Poke()
	return c.t.cancelled, nil
}

// save builds the timer's automation, has the integration check it
// (mira/automation/validate), saves it through Home Assistant's automation
// config API with the label "Mira: einmalig", and has it watched. replace is
// the id of the timer it replaces (extending, a second sleep timer in the
// room); its unchanged actions are not checked again. Each refusal is logged
// here with what was refused.
func (c *controller) save(ctx context.Context, o once, replace string) error {
	if !o.Due.After(c.o.Clock()) {
		return fmt.Errorf("due %s is in the past", o.Due.Format(time.RFC3339))
	}
	if len(o.Actions) == 0 {
		return errors.New("a timer needs at least one action")
	}
	open, err := c.open(ctx)
	if err != nil {
		return err
	}
	if replace == "" {
		same := 0
		for _, tm := range open {
			if tm.Owner == o.Owner && tm.Area == o.Area {
				same++
			}
		}
		if same >= maxPerOwnerArea {
			return fmt.Errorf("too many timers: at most %d per person and room", maxPerOwnerArea)
		}
	} else if !slices.ContainsFunc(open, func(tm Timer) bool { return tm.ID == replace }) {
		return fmt.Errorf("no timer %s", replace)
	}
	o.ID = cmp.Or(replace, c.o.NewID())
	config := buildOnce(o, c.o.Location)
	check := map[string]any{"type": "mira/automation/validate", "config": config, "forbidden": c.forbidden()}
	if replace != "" {
		check["automation_id"] = replace
	}
	raw, err := c.o.HA.Call(ctx, check)
	if err != nil {
		return err
	}
	var checked struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &checked); err != nil {
		c.log.Warn("the integration's answer to checking a timer cannot be read", "answer", excerpt.Bytes(raw), "error", err)
		return fmt.Errorf("unexpected answer of the integration: %s", excerpt.Bytes(raw))
	}
	if !checked.Valid {
		c.log.Warn("the integration refused a timer's automation", "timer", o.Title, "error", checked.Error)
		return errors.New(checked.Error)
	}
	if err := automation.Save(ctx, c.o.HA, c.o.Own, c.log, o.ID, config, automation.LabelOnce); err != nil {
		return err
	}
	c.Poke()
	return nil
}

// failed is the tool's answer when Home Assistant refused: without an
// admin's token, where to give Mira an access again.
func (c *controller) failed(err error) string {
	if errors.Is(err, haws.ErrNotAdmin) {
		c.log.Warn("home assistant refused mira's token for a timer; the repair for an access is open", "error", err)
		return c.t.needAccess
	}
	return fmt.Sprintf(c.t.failed, err)
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
	all, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	var out []Timer
	for _, tm := range all {
		if !tm.Ran {
			out = append(out, tm)
		}
	}
	return out, nil
}

// all are the one-time automations, those that ran too: Mira's automations
// labelled "Mira: einmalig" with a due time, as their variables say.
func (c *controller) all(ctx context.Context) ([]Timer, error) {
	raw, err := c.o.HA.Call(ctx, map[string]any{"type": "mira/automation/list", "label": automation.LabelOnce})
	if err != nil {
		return nil, err
	}
	var res struct {
		Automations []struct {
			ID            string `json:"id"`
			EntityID      string `json:"entity_id"`
			Current       int    `json:"current"`
			LastTriggered string `json:"last_triggered"`
			Config        struct {
				Alias     string           `json:"alias"`
				Variables map[string]any   `json:"variables"`
				Actions   []map[string]any `json:"actions"`
			} `json:"config"`
		} `json:"automations"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	var out []Timer
	for _, a := range res.Automations {
		v := func(k string) string { s, _ := a.Config.Variables[k].(string); return s }
		if v("mira_due") == "" {
			continue
		}
		out = append(out, Timer{ID: a.ID, EntityID: a.EntityID, Title: cmp.Or(v("mira_title"), a.Config.Alias),
			Due: v("mira_due"), Owner: v("mira_owner"), Area: v("mira_area"), Kind: v("mira_kind"),
			Actions: a.Config.Actions, Ran: a.LastTriggered != "", Running: a.Current})
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
