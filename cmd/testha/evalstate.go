package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// snap is the test HA before a scenario run, to put it back afterwards:
// events, to-do items, alarms on devices, entity states and Mira's tasks.
type snap struct {
	events map[string]map[string]bool // calendar -> uids
	todos  map[string]map[string]bool // list -> uids
	alarms map[string]map[string]bool // ESPHome node -> alarm ids
	states map[string]string
	tasks  map[string]bool
}

const (
	snapFrom = -2
	snapTo   = 60
)

func (r *evalRun) snapshot(ctx context.Context, sc Scenario) (snap, error) {
	s := snap{events: map[string]map[string]bool{}, todos: map[string]map[string]bool{}, alarms: map[string]map[string]bool{},
		states: map[string]string{}, tasks: map[string]bool{}}
	cals, lists, err := r.listsInHA(ctx)
	if err != nil {
		return s, err
	}
	from, _ := day(fmt.Sprint(snapFrom))
	to, _ := day(fmt.Sprintf("+%d", snapTo))
	for _, c := range cals {
		evs, err := r.events(ctx, c, from, to)
		if err != nil {
			return s, err
		}
		s.events[c] = map[string]bool{}
		for _, e := range evs {
			s.events[c][e.UID] = true
		}
	}
	for _, l := range lists {
		items, err := r.todoItems(ctx, l)
		if err != nil {
			return s, err
		}
		s.todos[l] = map[string]bool{}
		for _, it := range items {
			s.todos[l][it.UID] = true
		}
	}
	nodes, err := r.alarmNodes(ctx)
	if err != nil {
		return s, err
	}
	for _, n := range nodes {
		alarms, err := r.alarms(ctx, n)
		if err != nil {
			return s, err
		}
		s.alarms[n] = map[string]bool{}
		for _, a := range alarms {
			s.alarms[n][a.ID] = true
		}
	}
	restore := append(keys(sc.Setup.States), sc.Setup.Restore...)
	if sc.Setup.Home != nil {
		restore = append(restore, "input_boolean.sim_da_admin", "input_boolean.sim_da_anna", "input_boolean.sim_da_ben")
	}
	for _, e := range restore {
		var st struct {
			State string `json:"state"`
		}
		if err := r.ha(ctx, "GET", "/api/states/"+e, nil, &st); err != nil {
			return s, err
		}
		s.states[e] = st.State
	}
	var tpl string
	if err := r.ha(ctx, "POST", "/api/template", map[string]any{"template": `{% for e in label_entities('mira') %}{{ e }};{% endfor %}`}, &tpl); err != nil {
		return s, err
	}
	for _, e := range strings.Split(tpl, ";") {
		if e != "" {
			s.tasks[e] = true
		}
	}
	return s, nil
}

// restore removes what a run added and sets entities back.
func (r *evalRun) restore(ctx context.Context, s snap) error {
	admin, stop, err := connect(ctx, r.haToken)
	if err != nil {
		return err
	}
	defer stop()
	from, _ := day(fmt.Sprint(snapFrom))
	to, _ := day(fmt.Sprintf("+%d", snapTo))
	var errs []string
	for c, had := range s.events {
		evs, err := r.events(ctx, c, from, to)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, e := range evs {
			if !had[e.UID] {
				if err := call(ctx, admin, map[string]any{"type": "calendar/event/delete", "entity_id": c, "uid": e.UID}, nil); err != nil {
					errs = append(errs, err.Error())
				}
			}
		}
	}
	for l, had := range s.todos {
		items, err := r.todoItems(ctx, l)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, it := range items {
			if !had[it.UID] {
				if err := r.ha(ctx, "POST", "/api/services/todo/remove_item", map[string]any{"entity_id": l, "item": it.UID}, nil); err != nil {
					errs = append(errs, err.Error())
				}
			}
		}
	}
	for n, had := range s.alarms {
		alarms, err := r.alarms(ctx, n)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, a := range alarms {
			if !had[a.ID] {
				if err := r.ha(ctx, "POST", "/api/services/esphome/"+n+"_alarm_delete_id", map[string]any{"id": a.ID}, nil); err != nil {
					errs = append(errs, err.Error())
				}
			}
		}
	}
	for e, state := range s.states {
		if err := r.setState(ctx, e, state); err != nil {
			errs = append(errs, err.Error())
		}
	}
	var tpl string
	if err := r.ha(ctx, "POST", "/api/template", map[string]any{"template": `{% for e in label_entities('mira') %}{{ e }}={{ state_attr(e, 'id') }};{% endfor %}`}, &tpl); err == nil {
		for _, part := range strings.Split(tpl, ";") {
			if e, id, ok := strings.Cut(part, "="); ok && !s.tasks[e] && id != "None" {
				if err := r.ha(ctx, "DELETE", "/api/config/automation/config/"+id, nil, nil); err != nil {
					errs = append(errs, err.Error())
				}
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// setUp puts in what the scenario starts with.
func (r *evalRun) setUp(ctx context.Context, sc Scenario) error {
	body := map[string]any{"facts": sc.Setup.Facts}
	if err := r.agent(ctx, "POST", "/v1/eval/reset", body, nil); err != nil {
		return err
	}
	for _, e := range sc.Setup.Events {
		d, err := day(e.Day)
		if err != nil {
			return err
		}
		data := map[string]any{"entity_id": e.Calendar, "summary": e.Summary}
		if e.Start == "" {
			data["start_date"], data["end_date"] = d.Format("2006-01-02"), d.AddDate(0, 0, 1).Format("2006-01-02")
		} else {
			end := e.End
			if end == "" {
				end = e.Start
			}
			data["start_date_time"] = d.Format("2006-01-02") + " " + e.Start + ":00"
			data["end_date_time"] = d.Format("2006-01-02") + " " + end + ":00"
		}
		if err := r.ha(ctx, "POST", "/api/services/calendar/create_event", data, nil); err != nil {
			return err
		}
	}
	for _, t := range sc.Setup.Todo {
		data := map[string]any{"entity_id": t.List, "item": t.Item}
		if t.Due != "" {
			d, err := day(t.Due)
			if err != nil {
				return err
			}
			data["due_date"] = d.Format("2006-01-02")
		}
		if err := r.ha(ctx, "POST", "/api/services/todo/add_item", data, nil); err != nil {
			return err
		}
	}
	for e, state := range sc.Setup.States {
		if err := r.setState(ctx, e, state); err != nil {
			return err
		}
	}
	if sc.Setup.Home != nil {
		for _, p := range []string{"admin", "anna", "ben"} {
			want := "off"
			for _, h := range sc.Setup.Home {
				if strings.EqualFold(h, p) {
					want = "on"
				}
			}
			if err := r.setState(ctx, "input_boolean.sim_da_"+p, want); err != nil {
				return err
			}
		}
		time.Sleep(2 * time.Second) // the sim automation moves the trackers
	}
	return nil
}

// setState switches an on/off entity (or a cover) to a state. An entity
// that does not exist is an error: Home Assistant would ignore it silently.
func (r *evalRun) setState(ctx context.Context, entity, state string) error {
	if err := r.ha(ctx, "GET", "/api/states/"+entity, nil, nil); err != nil {
		return fmt.Errorf("%s: %w", entity, err)
	}
	domain, _, _ := strings.Cut(entity, ".")
	service := map[string]string{"on": "turn_on", "off": "turn_off", "open": "open_cover", "closed": "close_cover"}[state]
	if service == "" {
		return nil // not something to switch back
	}
	if domain != "cover" {
		domain = "homeassistant"
	}
	return r.ha(ctx, "POST", "/api/services/"+domain+"/"+service, map[string]any{"entity_id": entity}, nil)
}

func (r *evalRun) listsInHA(ctx context.Context) (cals, lists []string, err error) {
	var tpl string
	if err := r.ha(ctx, "POST", "/api/template", map[string]any{"template": `{% for s in states.calendar %}{{ s.entity_id }};{% endfor %}|{% for s in states.todo %}{{ s.entity_id }};{% endfor %}`}, &tpl); err != nil {
		return nil, nil, err
	}
	c, l, _ := strings.Cut(tpl, "|")
	return split(c), split(l), nil
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type todoItem struct {
	UID     string `json:"uid"`
	Summary string `json:"summary"`
	Due     string `json:"due"`
}

func (r *evalRun) todoItems(ctx context.Context, list string) ([]todoItem, error) {
	var out struct {
		Response map[string]struct {
			Items []todoItem `json:"items"`
		} `json:"service_response"`
	}
	err := r.ha(ctx, "POST", "/api/services/todo/get_items?return_response", map[string]any{"entity_id": list, "status": []string{"needs_action", "completed"}}, &out)
	return out.Response[list].Items, err
}

type alarm struct {
	ID   string `json:"id"`
	Time string `json:"time"`
}

// alarmNodes are the ESPHome nodes with alarms (a TECHO5 Echo Show).
func (r *evalRun) alarmNodes(ctx context.Context) ([]string, error) {
	var services []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if err := r.ha(ctx, "GET", "/api/services", nil, &services); err != nil {
		return nil, err
	}
	var out []string
	for _, d := range services {
		if d.Domain != "esphome" {
			continue
		}
		for name := range d.Services {
			// Only the fake devices (testenv/techo5_fake): evals must not
			// depend on real hardware being on.
			if node, ok := strings.CutSuffix(name, "_alarms_list"); ok && strings.HasPrefix(node, "fake_") {
				out = append(out, node)
			}
		}
	}
	return out, nil
}

func (r *evalRun) alarms(ctx context.Context, node string) ([]alarm, error) {
	var out struct {
		Response struct {
			Alarms []alarm `json:"alarms"`
		} `json:"service_response"`
	}
	err := r.ha(ctx, "POST", "/api/services/esphome/"+node+"_alarms_list?return_response", map[string]any{}, &out)
	return out.Response.Alarms, err
}
