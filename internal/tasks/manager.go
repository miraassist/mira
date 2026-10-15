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

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/ha/structure"
	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tools"
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
	// Deliver, when set, makes drafting run in the background and delivers
	// the proposal to whoever asked, like a reminder.
	Deliver func(ctx context.Context, n store.Notice, areaID string) error
	// PlanDelay holds background drafting back until the turn that asked
	// for it has answered.
	PlanDelay time.Duration
	// HAURL is Home Assistant's address, for links into its editor.
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
}

// errCannot is a task the house cannot do; drafting again does not help.
type errCannot struct{ reason string }

func (e errCannot) Error() string { return e.reason }

// Create drafts an automation for a goal, has Home Assistant check it and
// stores it as a proposal. A refused draft is made once more with the
// reason.
func (m *Manager) Create(ctx context.Context, goal, sessionKey, origin string) (store.Task, error) {
	states, err := m.opts.Source.States(ctx)
	if err != nil {
		return store.Task{}, err
	}
	instruction := m.draftInstruction(goal, Catalog(states, m.opts.Snapshot()))
	t := m.texts()
	var d Draft
	for attempt := 0; ; attempt++ {
		raw, err := m.opts.Planner.Ask(ctx, t.draftSystem, instruction, nil)
		if err != nil {
			return store.Task{}, err
		}
		d = Draft{}
		err = json.Unmarshal([]byte(raw), &d)
		if err == nil && strings.TrimSpace(d.Error) != "" {
			return store.Task{}, errCannot{strings.TrimSpace(d.Error)}
		}
		if err == nil {
			err = m.check(ctx, &d, goal)
		}
		if err == nil {
			break
		}
		if attempt == 1 {
			return store.Task{}, fmt.Errorf("no valid automation: %w", err)
		}
		m.log.Info("draft refused, drafting again", "error", err)
		instruction += fmt.Sprintf(t.redraft, err.Error())
	}
	automation, _ := json.Marshal(d.Automation)
	task := store.Task{ID: strings.ToLower(ulid.Make().String()[:10]), Goal: strings.TrimSpace(goal), Title: d.Title,
		Summary: d.Summary, Plan: string(automation), State: store.TaskProposed, SessionKey: sessionKey, Origin: origin}
	if err := m.opts.Store.PutTask(ctx, task); err != nil {
		return store.Task{}, err
	}
	m.log.Info("task proposed", "task", task.ID, "title", task.Title)
	return task, nil
}

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

// Propose drafts in the background and delivers the proposal, or the
// failure, to whoever asked. ctx only carries values.
func (m *Manager) Propose(ctx context.Context, goal string, inv tools.Invocation) {
	origin, area := m.origin(ctx, inv)
	var persons map[string]config.Person
	if m.opts.Persons != nil {
		persons = m.opts.Persons()
	}
	audience := tools.AudienceFor("", inv, persons)
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		time.Sleep(m.opts.PlanDelay)
		t := m.texts()
		n := store.Notice{ID: ulid.Make().String(), Source: store.SourceTask, Importance: 2, Audience: audience}
		task, err := m.Create(ctx, goal, inv.SessionKey, origin)
		var cannot errCannot
		switch {
		case errors.As(err, &cannot):
			m.log.Info("task cannot be done here", "goal", goal, "reason", cannot.reason)
			n.DedupeKey = "task-plan:" + n.ID
			n.Spoken = fmt.Sprintf(t.cannot, strings.TrimSpace(goal), cannot.reason)
		case err != nil:
			m.log.Error("could not draft a task", "goal", goal, "error", err)
			n.DedupeKey = "task-plan:" + n.ID
			n.Spoken = fmt.Sprintf(t.planFailed, strings.TrimSpace(goal))
		default:
			n.DedupeKey = "task-plan:" + task.ID
			n.Spoken = m.proposalText(task)
			n.Details = task.Summary
		}
		if err := m.opts.Deliver(ctx, n, area); err != nil {
			m.log.Error("could not deliver a task proposal", "error", err)
		}
	}()
}

// proposalText is the proposal as said to the user. Actions besides Mira's
// own are named by the code, not left to the summary: the yes covers
// exactly them.
func (m *Manager) proposalText(task store.Task) string {
	t := m.texts()
	summary := task.Summary
	var automation map[string]any
	if json.Unmarshal([]byte(task.Plan), &automation) == nil {
		if other := otherActions(automation["actions"]); len(other) > 0 {
			summary = strings.TrimSpace(summary) + " " + fmt.Sprintf(t.alsoSwitches, strings.Join(other, ", "))
		}
	}
	return fmt.Sprintf(t.proposal, task.Title, summary)
}

// origin is where a turn happens: the satellite's area for voice (a reply
// to a proposal comes in a new session there), the session key otherwise.
func (m *Manager) origin(ctx context.Context, inv tools.Invocation) (origin, area string) {
	if inv.Channel == config.ChannelVoice {
		if sess, err := m.opts.Store.GetSession(ctx, inv.SessionKey); err == nil && sess.AreaID != "" {
			return "area:" + sess.AreaID, sess.AreaID
		}
	}
	return inv.SessionKey, ""
}

// ConfirmLatest saves the newest proposal made at origin.
func (m *Manager) ConfirmLatest(ctx context.Context, origin string) (store.Task, error) {
	newest, err := m.newestProposal(ctx, origin, 0)
	if err != nil {
		return store.Task{}, err
	}
	return m.Activate(ctx, newest.ID)
}

// newestProposal is the newest proposal made at origin, younger than
// within when within > 0.
func (m *Manager) newestProposal(ctx context.Context, origin string, within time.Duration) (store.Task, error) {
	list, err := m.opts.Store.Tasks(ctx)
	if err != nil {
		return store.Task{}, err
	}
	now := m.opts.Clock()
	var newest store.Task
	for _, t := range list {
		if t.State != store.TaskProposed || t.Origin != origin || (within > 0 && now.Sub(t.CreatedAt) >= within) {
			continue
		}
		if !t.CreatedAt.Before(newest.CreatedAt) {
			newest = t
		}
	}
	if newest.ID == "" {
		return store.Task{}, store.ErrNotFound
	}
	return newest, nil
}

// proposalFresh is how long a plain yes still answers a proposal.
const proposalFresh = 10 * time.Minute

// Shortcut confirms a proposal in code: a plain yes where a proposal
// younger than proposalFresh waits. On the list to discuss: fixed phrases
// are language-bound.
func (m *Manager) Shortcut(ctx context.Context, inv tools.Invocation, text string) (string, bool) {
	t := m.texts()
	if !isYes(t.yesPhrases, text) {
		return "", false
	}
	origin, _ := m.origin(ctx, inv)
	newest, err := m.newestProposal(ctx, origin, proposalFresh)
	if err != nil {
		return "", false
	}
	task, err := m.Activate(ctx, newest.ID)
	if err != nil {
		m.log.Error("could not save a confirmed task", "task", newest.ID, "error", err)
		return fmt.Sprintf(t.saveFailed, err), true
	}
	return fmt.Sprintf(t.confirmed, task.Title), true
}

// isYes reports whether text is nothing but agreement, one of the
// "|"-separated phrases, ignoring case and punctuation.
func isYes(phrases, text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.Map(func(r rune) rune {
		if strings.ContainsRune(".,!?;:„“\"'", r) {
			return -1
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	for _, p := range strings.Split(phrases, "|") {
		if text == p {
			return true
		}
	}
	return false
}

// Activate saves a proposal as a Home Assistant automation. From then on
// Home Assistant holds the task; the proposal is dropped.
func (m *Manager) Activate(ctx context.Context, id string) (store.Task, error) {
	t, err := m.opts.Store.GetTask(ctx, id)
	if err != nil {
		return store.Task{}, err
	}
	var automation map[string]any
	if err := json.Unmarshal([]byte(t.Plan), &automation); err != nil {
		return store.Task{}, err
	}
	if _, err := m.opts.HA.Call(ctx, map[string]any{
		"type": "mira/automation/save", "config": automation, "automation_id": automationID(t.ID),
		"forbidden": m.forbidden(),
	}); err != nil {
		return store.Task{}, err
	}
	if err := m.opts.Store.DeleteTask(ctx, t.ID); err != nil {
		m.log.Warn("could not drop a saved proposal", "task", t.ID, "error", err)
	}
	m.log.Info("task saved", "task", t.ID, "automation", automationID(t.ID))
	return t, nil
}

// Item is a task as the UI and task_list show it: a proposal or one of
// Mira's automations.
type Item struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Goal          string          `json:"goal"`
	Summary       string          `json:"summary,omitempty"`
	State         string          `json:"state"` // proposed, on, off, unavailable
	EntityID      string          `json:"entity_id,omitempty"`
	LastTriggered string          `json:"last_triggered,omitempty"`
	Automation    json.RawMessage `json:"automation,omitempty"`
	EditURL       string          `json:"edit_url,omitempty"`
}

// List returns open proposals and Mira's automations. Proposals come even
// when Home Assistant cannot be asked; the error says so.
func (m *Manager) List(ctx context.Context) ([]Item, error) {
	var out []Item
	proposals, err := m.opts.Store.Tasks(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range proposals {
		if t.State == store.TaskProposed {
			out = append(out, Item{ID: t.ID, Title: t.Title, Goal: t.Goal, Summary: t.Summary,
				State: store.TaskProposed, Automation: json.RawMessage(t.Plan)})
		}
	}
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
	for _, a := range res.Automations {
		cfg, _ := json.Marshal(a.Config)
		alias, _ := a.Config["alias"].(string)
		goal, _ := a.Config["description"].(string)
		item := Item{ID: a.ID, Title: alias, Goal: goal, State: a.State, EntityID: a.EntityID,
			LastTriggered: a.LastTriggered, Automation: cfg}
		if m.opts.HAURL != "" {
			item.EditURL = strings.TrimRight(m.opts.HAURL, "/") + "/config/automation/edit/" + a.ID
		}
		out = append(out, item)
	}
	return out, nil
}

// Delete drops a proposal or deletes one of Mira's automations.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if err := m.opts.Store.DeleteTask(ctx, id); err == nil {
		m.log.Info("proposal dropped", "task", id)
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
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

// Try runs the Mira actions of a task now, without notices: what would
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
			if it.State == store.TaskProposed {
				key = automationID(id)
			}
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
