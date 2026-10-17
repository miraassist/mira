package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

// Source is what the tasks read from Home Assistant.
type Source interface {
	States(ctx context.Context) ([]haws.State, error)
	Statistics(ctx context.Context, ids []string, start time.Time, period string) (map[string][]haws.StatRow, error)
}

// HA is what the tasks change in Home Assistant: Mira's automations go
// through the Mira integration's commands (mira/automation/*), switching
// them on and off is an ordinary service call.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
}

// Model runs one isolated request with a JSON answer: a system message and
// an instruction, no house prompt, no tools.
type Model interface {
	Ask(ctx context.Context, system, instruction string, schema json.RawMessage) (string, error)
}

type Options struct {
	Lang     i18n.Lang
	Store    *store.Store
	Source   Source
	HA       HA
	Snapshot func() structure.Snapshot
	// Model judges (assess, decide); Planner drafts automations. Both may
	// be the same.
	Model, Planner Model
	QuietHours     config.QuietHours
	Persons        func() map[string]config.Person
	// Satellites maps an area to its satellite, for where a task was asked.
	Satellites func() map[string]string
	// Upcoming renders a calendar's events in a window (view upcoming).
	Upcoming func(ctx context.Context, entity string, from, to time.Time) (string, error)
	// HAURL is how Mira reaches Home Assistant. Links into its editor use
	// the URL Home Assistant has configured for itself, this only without.
	HAURL string
	// ConfirmDomains and ConfirmEntities are what Mira only switches after
	// an explicit yes (security config): a task never calls them.
	ConfirmDomains, ConfirmEntities []string
	Clock                           func() time.Time
	Location                        *time.Location
	Logger                          *slog.Logger
}

type Manager struct {
	opts Options
	log  *slog.Logger
}

func New(opts Options) *Manager {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Snapshot == nil {
		opts.Snapshot = func() structure.Snapshot { return structure.Snapshot{} }
	}
	if opts.Planner == nil {
		opts.Planner = opts.Model
	}
	return &Manager{opts: opts, log: opts.Logger.With("component", "tasks")}
}

func (m *Manager) texts() texts { return i18n.Pick(catalog, m.opts.Lang) }

// automationID is the id a confirmed proposal gets in Home Assistant.
func automationID(taskID string) string { return "mira_" + taskID }

// Draft is what the planner makes of a goal.
type Draft struct {
	Title      string         `json:"title"`
	Summary    string         `json:"summary"`
	Automation map[string]any `json:"automation"`
	// Error is the planner's reason when the house cannot do the task (no
	// fitting entity). It ends drafting: a second draft would not help.
	Error string `json:"error"`
	// Question asks the user for a measure the goal leaves open ("what is
	// unusual for you?"): better than a task that only pretends to have
	// a condition.
	Question string `json:"question"`
}

// ErrQuestion ends drafting with a question back to the user.
type ErrQuestion struct{ Question string }

func (e ErrQuestion) Error() string { return e.Question }

// ErrCannot is a task the house cannot do; drafting again does not help.
type ErrCannot struct{ Reason string }

func (e ErrCannot) Error() string { return e.Reason }

// DraftAutomation drafts an automation for a request and the fields a
// thread collected, and has Home Assistant check it. A refused draft is
// made once more with the reason. The planner may instead ask back
// (errQuestion) or say the house cannot do it (errCannot).
// Origin is where and from whom a task came: the area of the satellite it
// was asked at, and the person when Mira knew who spoke. Either may be
// empty.
type Origin struct {
	Area, Person string
}

func (m *Manager) DraftAutomation(ctx context.Context, request string, fields map[string]string, from Origin) (Draft, error) {
	states, err := m.opts.Source.States(ctx)
	if err != nil {
		return Draft{}, err
	}
	goal := strings.TrimSpace(request)
	// What Mira knows may hold the measure the request leaves open: with it
	// the planner builds a fixed condition instead of asking back.
	instruction := m.draftInstruction(goal, Catalog(states, m.opts.Snapshot()), m.memory(ctx, Request{Goal: goal, Owner: from.Person}))
	instruction += m.originText(from)
	instruction += m.fieldsText(fields)
	t := m.texts()
	var d Draft
	for attempt := 0; ; attempt++ {
		raw, err := m.opts.Planner.Ask(ctx, t.draftSystem, instruction, nil)
		if err != nil {
			return Draft{}, err
		}
		d = Draft{}
		err = json.Unmarshal([]byte(raw), &d)
		if err == nil && strings.TrimSpace(d.Error) != "" {
			return Draft{}, ErrCannot{strings.TrimSpace(d.Error)}
		}
		if err == nil && strings.TrimSpace(d.Question) != "" {
			return Draft{}, ErrQuestion{strings.TrimSpace(d.Question)}
		}
		if err == nil {
			err = m.check(ctx, &d, goal)
		}
		if err == nil {
			break
		}
		if attempt == 1 {
			return Draft{}, fmt.Errorf("no valid automation: %w", err)
		}
		m.log.Info("draft refused, drafting again", "error", err)
		instruction += fmt.Sprintf(t.redraft, err.Error())
	}
	// A person's task judges with their memory: the owner is set here, not
	// left to the model.
	if from.Person != "" {
		setOwner(d.Automation, from.Person)
	}
	m.log.Info("automation drafted", "title", d.Title, "area", from.Area, "person", from.Person)
	return d, nil
}

// originText tells the planner where and from whom the task came, so "here"
// and "me" resolve.
func (m *Manager) originText(from Origin) string {
	t := m.texts()
	var b strings.Builder
	if from.Area != "" {
		name := m.opts.Snapshot().AreaNames[from.Area]
		if name == "" {
			name = from.Area
		}
		sat := ""
		if m.opts.Satellites != nil {
			sat = m.opts.Satellites()[from.Area]
		}
		fmt.Fprintf(&b, t.draftArea, name, from.Area, sat)
	}
	if from.Person != "" {
		name := from.Person
		if m.opts.Persons != nil {
			if p, ok := m.opts.Persons()[from.Person]; ok && p.Name != "" {
				name = p.Name
			}
		}
		fmt.Fprintf(&b, t.draftPerson, name)
	}
	return b.String()
}

// setOwner makes every mira.assess and mira.decide in an automation judge
// for the person, wherever it sits (also inside choose or if).
func setOwner(node any, person string) {
	switch v := node.(type) {
	case map[string]any:
		if a, _ := v["action"].(string); a == "mira.assess" || a == "mira.decide" {
			data, _ := v["data"].(map[string]any)
			if data == nil {
				data = map[string]any{}
				v["data"] = data
			}
			data["owner"] = person
		}
		for _, child := range v {
			setOwner(child, person)
		}
	case []any:
		for _, child := range v {
			setOwner(child, person)
		}
	}
}

// fieldsText renders a thread's fields for the planner.
func (m *Manager) fieldsText(fields map[string]string) string {
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.texts().draftFields)
	for _, k := range sortedKeys(fields) {
		fmt.Fprintf(&b, "\n- %s: %s", k, fields[k])
	}
	return b.String()
}

// SaveAutomation creates or replaces one of Mira's automations.
func (m *Manager) SaveAutomation(ctx context.Context, id string, automation map[string]any) error {
	_, err := m.opts.HA.Call(ctx, map[string]any{
		"type": "mira/automation/save", "config": automation, "automation_id": id,
		"forbidden": m.forbidden(),
	})
	if err == nil {
		m.log.Info("task saved", "automation", id)
	}
	return err
}

// NewAutomationID is the id a new task gets in Home Assistant.
func NewAutomationID() string { return automationID(strings.ToLower(ulid.Make().String()[:10])) }

// check completes a draft and lets Home Assistant validate it.
func (m *Manager) check(ctx context.Context, d *Draft, goal string) error {
	if strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.Summary) == "" || len(d.Automation) == 0 {
		return errors.New("title, summary and automation are required")
	}
	if triggers, _ := d.Automation["triggers"].([]any); len(triggers) == 0 {
		return errors.New("the automation needs at least one trigger")
	}
	if d.Automation["alias"] == nil {
		d.Automation["alias"] = d.Title
	}
	if d.Automation["description"] == nil {
		d.Automation["description"] = strings.TrimSpace(goal)
	}
	tagTask(d.Automation["actions"])
	raw, err := m.opts.HA.Call(ctx, map[string]any{"type": "mira/automation/validate", "config": d.Automation,
		"forbidden": m.forbidden()})
	if err != nil {
		return err
	}
	var res struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	if !res.Valid {
		return errors.New(res.Error)
	}
	return nil
}

// forbidden is what the integration refuses in a task's actions.
func (m *Manager) forbidden() map[string]any {
	return map[string]any{"domains": nonNil(m.opts.ConfirmDomains), "entities": nonNil(m.opts.ConfirmEntities)}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// otherActions lists what an automation does besides Mira's own actions,
// as "service (targets)": the proposal says it out loud, so the user agrees
// to exactly what will be switched.
func otherActions(actions any) []string {
	var out []string
	switch a := actions.(type) {
	case []any:
		for _, step := range a {
			out = append(out, otherActions(step)...)
		}
	case map[string]any:
		name, _ := a["action"].(string)
		if name == "" {
			name, _ = a["service"].(string)
		}
		if name != "" && !strings.HasPrefix(name, "mira.") {
			var targets []string
			for _, holder := range []any{a["target"], a["data"]} {
				h, _ := holder.(map[string]any)
				for _, key := range []string{"entity_id", "area_id", "device_id"} {
					switch v := h[key].(type) {
					case string:
						targets = append(targets, v)
					case []any:
						for _, x := range v {
							targets = append(targets, fmt.Sprint(x))
						}
					}
				}
			}
			if len(targets) > 0 {
				name += " (" + strings.Join(targets, ", ") + ")"
			}
			out = append(out, name)
		}
		for _, key := range []string{"then", "else", "sequence", "parallel", "default"} {
			out = append(out, otherActions(a[key])...)
		}
		if options, ok := a["choose"].([]any); ok {
			for _, o := range options {
				if om, ok := o.(map[string]any); ok {
					out = append(out, otherActions(om["sequence"])...)
				}
			}
		}
	}
	return out
}

// tagTask makes every mira.assess and mira.decide in the actions pass the
// automation's id, so its judgements share a log and earlier verdicts.
func tagTask(actions any) {
	for _, call := range miraCalls(actions) {
		if call.data["task"] == nil {
			call.data["task"] = "{{ this.attributes.id }}"
		}
	}
}

// proposalText is the proposal as said to the user. Actions besides Mira's
// own are named by the code, not left to the summary: the yes covers
// exactly them.
func (m *Manager) ProposalText(d Draft) string {
	t := m.texts()
	parts := []string{strings.TrimSpace(d.Summary)}
	if automation := d.Automation; automation != nil {
		if other := otherActions(automation["actions"]); len(other) > 0 {
			parts = append(parts, fmt.Sprintf(t.alsoSwitches, strings.Join(other, ", ")))
		}
		// Whether a fixed condition holds is said by the code, from the
		// automation: a summary must not suggest one that is not there.
		conds := m.describeConditions(automation["conditions"])
		judges := len(miraCalls(automation["actions"])) > 0
		switch {
		case len(conds) > 0:
			parts = append(parts, fmt.Sprintf(t.conditionsAre, strings.Join(conds, "; ")))
		case judges:
			parts = append(parts, t.judgesItself)
		default:
			parts = append(parts, t.noCondition)
		}
	}
	return fmt.Sprintf(t.proposal, d.Title, strings.Join(parts, " "))
}

// describeConditions renders an automation's conditions short enough to
// say: "zwischen 00:00 und 06:00 Uhr", "sensor.x über 30".
func (m *Manager) describeConditions(conditions any) []string {
	t := m.texts()
	list, _ := conditions.([]any)
	var out []string
	for _, c := range list {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		str := func(k string) string {
			switch v := cm[k].(type) {
			case string:
				return v
			case []any:
				parts := make([]string, len(v))
				for i, x := range v {
					parts[i] = fmt.Sprint(x)
				}
				return strings.Join(parts, ", ")
			case nil:
				return ""
			default:
				return fmt.Sprint(v)
			}
		}
		clock := func(k string) string { // "06:00:00" -> "06:00"
			v := str(k)
			if len(v) == 8 && strings.Count(v, ":") == 2 {
				return v[:5]
			}
			return v
		}
		var d string
		switch kind := str("condition"); kind {
		case "time":
			after, before := clock("after"), clock("before")
			switch {
			case after != "" && before != "":
				d = fmt.Sprintf(t.condBetween, after, before)
			case after != "":
				d = fmt.Sprintf(t.condAfter, after)
			case before != "":
				d = fmt.Sprintf(t.condBefore, before)
			}
			if wd := str("weekday"); wd != "" {
				d = strings.TrimSpace(d + " (" + wd + ")")
			}
		case "state":
			d = str("entity_id") + " = " + str("state")
		case "numeric_state":
			d = str("entity_id")
			if v := str("above"); v != "" {
				d += " " + fmt.Sprintf(t.condAbove, v)
			}
			if v := str("below"); v != "" {
				d += " " + fmt.Sprintf(t.condBelow, v)
			}
		case "template":
			d = t.condTemplate
		default:
			d = kind
		}
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// Item is a task as the UI and task_list show it: a proposal or one of
// Mira's automations.
type Item struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Goal          string          `json:"goal"`
	Summary       string          `json:"summary,omitempty"`
	State         string          `json:"state"` // on, off, unavailable
	EntityID      string          `json:"entity_id,omitempty"`
	LastTriggered string          `json:"last_triggered,omitempty"`
	Automation    json.RawMessage `json:"automation,omitempty"`
	EditURL       string          `json:"edit_url,omitempty"`
}

// List returns Mira's automations. Proposals still being planned live in
// their thread.
func (m *Manager) List(ctx context.Context) ([]Item, error) {
	var out []Item
	raw, err := m.opts.HA.Call(ctx, map[string]any{"type": "mira/automation/list"})
	if err != nil {
		return out, fmt.Errorf("Mira's automations: %w", err)
	}
	var res struct {
		Automations []struct {
			ID            string         `json:"id"`
			EntityID      string         `json:"entity_id"`
			State         string         `json:"state"`
			LastTriggered string         `json:"last_triggered"`
			Config        map[string]any `json:"config"`
		} `json:"automations"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return out, err
	}
	base := ""
	if len(res.Automations) > 0 {
		base = m.homeURL(ctx)
	}
	for _, a := range res.Automations {
		cfg, _ := json.Marshal(a.Config)
		alias, _ := a.Config["alias"].(string)
		goal, _ := a.Config["description"].(string)
		item := Item{ID: a.ID, Title: alias, Goal: goal, State: a.State, EntityID: a.EntityID,
			LastTriggered: a.LastTriggered, Automation: cfg}
		if base != "" {
			item.EditURL = base + "/config/automation/edit/" + a.ID
		}
		out = append(out, item)
	}
	return out, nil
}

// homeURL is where a browser finds Home Assistant: its external or internal
// URL, else the address Mira uses, which may be localhost for an agent next
// to it.
func (m *Manager) homeURL(ctx context.Context) string {
	var cfg struct {
		External string `json:"external_url"`
		Internal string `json:"internal_url"`
	}
	if raw, err := m.opts.HA.Call(ctx, map[string]any{"type": "get_config"}); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	for _, u := range []string{cfg.External, cfg.Internal, m.opts.HAURL} {
		if u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	return ""
}

// Delete deletes one of Mira's automations.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if _, err := m.opts.HA.Call(ctx, map[string]any{"type": "mira/automation/delete", "automation_id": id}); err != nil {
		return err
	}
	m.log.Info("task deleted", "automation", id)
	return nil
}

// SetEnabled switches one of Mira's automations on or off.
func (m *Manager) SetEnabled(ctx context.Context, id string, on bool) error {
	items, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.ID == id && it.EntityID != "" {
			service := "turn_off"
			if on {
				service = "turn_on"
			}
			return m.opts.HA.CallService(ctx, "automation", service, nil, &haws.Target{EntityID: []string{it.EntityID}})
		}
	}
	return store.ErrNotFound
}

// TryResult is what a dry run says.
type TryResult struct {
	Verdicts  []Verdict  `json:"verdicts,omitempty"`
	Decisions []Decision `json:"decisions,omitempty"`
}

// Try runs the Mira actions of a task now, without messages: what would
// Mira say with the data as it is? Text that needs a trigger's templates
// is replaced.
func (m *Manager) Try(ctx context.Context, id string) (TryResult, error) {
	items, err := m.List(ctx)
	if err != nil && len(items) == 0 {
		return TryResult{}, err
	}
	var automation map[string]any
	key := id
	for _, it := range items {
		if it.ID == id {
			_ = json.Unmarshal(it.Automation, &automation)
		}
	}
	if automation == nil {
		return TryResult{}, store.ErrNotFound
	}
	t := m.texts()
	var res TryResult
	for _, call := range miraCalls(automation["actions"]) {
		raw, _ := json.Marshal(call.data)
		var r Request
		_ = json.Unmarshal(raw, &r)
		r.Task = key
		r.Observation = withoutTemplates(r.Observation, t.tryObservation)
		off := false
		r.Notify = &off
		switch call.action {
		case "mira.assess":
			v, err := m.Assess(ctx, r)
			if err != nil {
				return res, err
			}
			res.Verdicts = append(res.Verdicts, v)
		case "mira.decide":
			d, err := m.Decide(ctx, r)
			if err != nil {
				return res, err
			}
			res.Decisions = append(res.Decisions, d)
		}
	}
	return res, nil
}

type miraCall struct {
	action string
	data   map[string]any
}

// miraCalls finds the mira.assess and mira.decide steps of an automation's
// actions, also inside choose, if, sequence and parallel. A step without
// data gets an empty map, so callers can fill it in.
func miraCalls(actions any) []miraCall {
	var out []miraCall
	switch a := actions.(type) {
	case []any:
		for _, step := range a {
			out = append(out, miraCalls(step)...)
		}
	case map[string]any:
		if name, _ := a["action"].(string); name == "mira.assess" || name == "mira.decide" {
			data, _ := a["data"].(map[string]any)
			if data == nil {
				data = map[string]any{}
				a["data"] = data
			}
			out = append(out, miraCall{name, data})
		}
		for _, key := range []string{"then", "else", "sequence", "parallel", "default"} {
			out = append(out, miraCalls(a[key])...)
		}
		if options, ok := a["choose"].([]any); ok {
			for _, o := range options {
				if om, ok := o.(map[string]any); ok {
					out = append(out, miraCalls(om["sequence"])...)
				}
			}
		}
	}
	return out
}

// withoutTemplates replaces text that needs Home Assistant's templates.
func withoutTemplates(s, instead string) string {
	if strings.Contains(s, "{{") || strings.Contains(s, "{%") {
		return instead
	}
	return s
}

func inQuietHours(q config.QuietHours, t time.Time) bool {
	from, to := q.From.MinutesOfDay(), q.To.MinutesOfDay()
	m := t.Hour()*60 + t.Minute()
	if from == to {
		return false
	}
	if from < to {
		return m >= from && m < to
	}
	return m >= from || m < to
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
