package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
)

// Changing the household's tasks in the conversation
// (docs/spec/aufgaben-aendern.md): the check "tasks.find" finds the task,
// the finish "tasks.edit" drafts the change, proposes it and saves it on yes.

const (
	// CheckFind and FinishEdit are the building blocks' names in a skill.
	CheckFind  = "tasks.find"
	FinishEdit = "tasks.edit"
	// ToolEdit is the call a change of a task passes the hooks as.
	ToolEdit = "task_edit"
)

// automationPattern finds a task's entity id in a field value.
var automationPattern = regexp.MustCompile(`\bautomation\.[a-z0-9_]+\b`)

// listed is a task as mira/automation/list with all names it.
type listed struct {
	EntityID string `json:"entity_id"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Area     string `json:"area"`
	Editable bool   `json:"editable"`
}

// unsupported says whether Home Assistant's refusal means the integration
// does not know the command or field yet.
func unsupported(err error) bool {
	code := haws.CodeOf(err)
	return code == "unknown_command" || code == "invalid_format"
}

// CheckTask is the building block "tasks.find": the field names one task of
// the house, by its entity id, its name or its area. The value comes back
// with the task's entity id. Several that fit equally well are named, so the
// thread asks which one.
func (m *Manager) CheckTask(ctx context.Context, value string) (string, error) {
	t := m.texts()
	raw, err := m.opts.HA.Call(ctx, map[string]any{"type": "mira/automation/list", "all": true})
	if unsupported(err) {
		return "", errors.New(t.needsUpdate)
	}
	if err != nil {
		return "", err
	}
	var res struct {
		Automations []listed `json:"automations"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	if len(res.Automations) == 0 {
		return "", errors.New(t.findNoTasks)
	}
	if id := automationPattern.FindString(value); id != "" {
		for _, a := range res.Automations {
			if a.EntityID == id {
				return value, nil
			}
		}
	}
	// By the words of its name and area: the task with the most of them
	// in the value wins ("das Abendlicht im Wohnzimmer" fits "Abendlicht
	// Wohnzimmer" better than "Abendlicht Küche").
	lower := strings.ToLower(value)
	best, found := 0, []listed(nil)
	for _, a := range res.Automations {
		score := 0
		for _, w := range strings.Fields(strings.ToLower(a.Name + " " + a.Area)) {
			if len([]rune(w)) >= 3 && strings.Contains(lower, w) {
				score++
			}
		}
		switch {
		case score == 0 || score < best:
		case score > best:
			best, found = score, []listed{a}
		default:
			found = append(found, a)
		}
	}
	names := func(list []listed) string {
		out := make([]string, len(list))
		for i, a := range list {
			out[i] = a.Name
			if a.Area != "" {
				out[i] += " (" + a.Area + ")"
			}
		}
		return strings.Join(out, ", ")
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf(t.findNone, names(res.Automations))
	case 1:
		return value + " (" + found[0].EntityID + ")", nil
	}
	return "", fmt.Errorf(t.findSeveral, names(found))
}

// EditFinisher is the building block "tasks.edit".
func (m *Manager) EditFinisher() threads.Finisher { return editFinisher{m} }

type editFinisher struct{ m *Manager }

// edit is what a thread keeps of a proposed change.
type edit struct {
	ID       string         `json:"id"`
	EntityID string         `json:"entity_id"`
	Title    string         `json:"title"`
	Summary  string         `json:"summary"`
	Before   map[string]any `json:"before"`
	After    map[string]any `json:"after"`
}

func (editFinisher) Async() bool { return true }

func (f editFinisher) Work(ctx context.Context, th store.Thread) (threads.Result, error) {
	m, t := f.m, f.m.texts()
	entity := automationPattern.FindString(th.Fields["aufgabe"])
	if entity == "" {
		return threads.Result{}, errors.New("the thread names no task")
	}
	raw, err := m.opts.HA.Call(ctx, map[string]any{"type": "mira/automation/get", "entity_id": entity})
	switch {
	case unsupported(err):
		return threads.Result{Cancel: t.needsUpdate}, nil
	case haws.CodeOf(err) == "not_editable":
		return threads.Result{Cancel: fmt.Sprintf(t.notEditable, th.Fields["aufgabe"])}, nil
	case err != nil:
		return threads.Result{}, err
	}
	var task struct {
		ID     string         `json:"id"`
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(raw, &task); err != nil {
		return threads.Result{}, err
	}
	before := withoutID(task.Config)
	// As a draft gets it: otherwise an unchanged task would differ.
	tagTask(before["actions"])
	name, _ := before["alias"].(string)
	if name == "" {
		name = entity
	}

	from := Origin{Person: th.Person}
	from.Area, _ = strings.CutPrefix(th.Place, "area:")
	if from.Area == th.Place {
		from.Area = ""
	}
	states, err := m.exposedStates(ctx)
	if err != nil {
		return threads.Result{}, err
	}
	change := strings.TrimSpace(th.Fields["aenderung"])
	goal := fmt.Sprintf(t.editIntro, name, change)
	current, _ := json.MarshalIndent(before, "", "  ")
	instruction := m.draftInstruction(goal, Catalog(states, m.opts.Snapshot()), m.memory(ctx, Request{Goal: goal, Owner: from.Person}))
	instruction += m.originText(from)
	instruction += fmt.Sprintf(t.editRules, current)

	d, err := m.draft(ctx, instruction, change, task.ID)
	var question ErrQuestion
	var cannot ErrCannot
	switch {
	case errors.As(err, &question):
		return threads.Result{Question: question.Question}, nil
	case errors.As(err, &cannot):
		return threads.Result{Cancel: fmt.Sprintf(t.cannot, th.Request, cannot.Reason)}, nil
	case errors.Is(err, errNoValid):
		// Refused twice, such as a new critical action: the reason is said.
		_, reason, _ := strings.Cut(err.Error(), ": ")
		return threads.Result{Cancel: fmt.Sprintf(t.cannot, th.Request, reason)}, nil
	case err != nil:
		return threads.Result{}, err
	}
	after := withoutID(d.Automation)
	if sameConfig(before, after) {
		return threads.Result{Cancel: fmt.Sprintf(t.editNothing, name)}, nil
	}
	e := edit{ID: task.ID, EntityID: entity, Title: name, Summary: strings.TrimSpace(d.Summary), Before: before, After: after}
	content, _ := json.Marshal(e)
	m.log.Info("task change drafted", "automation", task.ID, "person", th.Person)
	return threads.Result{Proposal: m.editProposal(e), Content: content}, nil
}

func (f editFinisher) Commit(ctx context.Context, th store.Thread) (string, error) {
	m := f.m
	var e edit
	if err := json.Unmarshal(th.Content, &e); err != nil || e.ID == "" || e.After == nil {
		return "", errors.New("the thread holds no change")
	}
	// The yes is said; what the hooks refuse stays refused (an unknown
	// voice, a critical target after web content).
	if m.opts.Hooks != nil {
		args, _ := json.Marshal(m.editTargets(e))
		if d := m.opts.Hooks(ctx, th, args); d.Outcome == tools.Deny {
			return "", errors.New(d.Reason)
		}
	}
	if _, err := m.opts.HA.Call(ctx, map[string]any{
		"type": "mira/automation/save", "config": e.After, "automation_id": e.ID,
		"forbidden": m.forbidden(), "keep_labels": true,
	}); err != nil {
		return "", err
	}
	m.log.Info("task edited", "automation", e.ID, "person", th.Person, "change", e.Summary)
	return fmt.Sprintf(m.texts().edited, e.Title), nil
}

// editProposal says the change: the drafting model's sentence, and what the
// code sees itself: the conditions before and after, what it newly switches
// and what no longer. The yes covers exactly that.
func (m *Manager) editProposal(e edit) string {
	t := m.texts()
	parts := []string{e.Summary}
	condsBefore, condsAfter := m.describeConditions(e.Before["conditions"]), m.describeConditions(e.After["conditions"])
	if !slices.Equal(condsBefore, condsAfter) {
		parts = append(parts, fmt.Sprintf(t.conditionsChange, orNone(condsBefore, t.noneShort), orNone(condsAfter, t.noneShort)))
	}
	was, now := otherActions(e.Before["actions"]), otherActions(e.After["actions"])
	if added := missingFrom(now, was); len(added) > 0 {
		parts = append(parts, fmt.Sprintf(t.newlySwitches, strings.Join(added, ", ")))
	}
	if gone := missingFrom(was, now); len(gone) > 0 {
		parts = append(parts, fmt.Sprintf(t.noLongerSwitches, strings.Join(gone, ", ")))
	}
	return fmt.Sprintf(t.editProposal, e.Title, strings.Join(parts, " "))
}

// editTargets is what the hooks see of a change: the entities the task's
// actions reach, before and after, and the domain of a step that names no
// entity (an area, a device, a label), so a lock behind an area is seen.
func (m *Manager) editTargets(e edit) map[string]any {
	entities, domains := map[string]bool{}, map[string]bool{}
	for _, s := range append(switchedSteps(e.Before["actions"]), switchedSteps(e.After["actions"])...) {
		named := len(s.targets) > 0
		for _, target := range s.targets {
			if strings.Contains(target, ".") {
				entities[target] = true
			} else {
				named = false
			}
		}
		if !named {
			domain, _, _ := strings.Cut(strings.Fields(s.name)[0], ".")
			domains[domain] = true
		}
	}
	args := map[string]any{"task": e.EntityID}
	if len(entities) > 0 {
		args["entity_id"] = slices.Sorted(maps.Keys(entities))
	}
	if len(domains) > 0 {
		args["domain"] = slices.Sorted(maps.Keys(domains))
	}
	return args
}

// withoutID is a task's configuration without its id, which the
// integration keeps itself.
func withoutID(config map[string]any) map[string]any {
	out := maps.Clone(config)
	delete(out, "id")
	return out
}

// sameConfig compares two configurations as JSON values (numbers read back
// alike whether they came from YAML or the model).
func sameConfig(a, b map[string]any) bool {
	var x, y any
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	_ = json.Unmarshal(ra, &x)
	_ = json.Unmarshal(rb, &y)
	return reflect.DeepEqual(x, y)
}

// missingFrom are the items of a that b does not have.
func missingFrom(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func orNone(list []string, none string) string {
	if len(list) == 0 {
		return none
	}
	return strings.Join(list, "; ")
}
