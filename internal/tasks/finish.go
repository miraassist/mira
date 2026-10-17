package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
)

// entityPattern finds entity ids in a field value ("light.bad und
// sensor.wasserzahler").
var entityPattern = regexp.MustCompile(`\b[a-z_]+\.[a-z0-9_]+\b`)

// CheckEntities is the building block "entities": a field must name at
// least one entity of the house, by id ("light.schlafzimmer") or by a name
// Home Assistant knows for it – display name, alias, device name – and
// every id it names must exist. The value comes back with the ids of the
// entities found, so the planner works with ids.
func (m *Manager) CheckEntities(ctx context.Context, value string) (string, error) {
	states, err := m.opts.Source.States(ctx)
	if err != nil {
		return "", err
	}
	snap := m.opts.Snapshot()
	known := map[string]bool{}
	for _, e := range Catalog(states, snap) {
		known[e.ID] = true
	}
	ids := entityPattern.FindAllString(value, -1)
	var unknown []string
	for _, id := range ids {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		return "", fmt.Errorf(m.texts().unknownEntities, strings.Join(unknown, ", "))
	}
	if len(ids) > 0 {
		return value, nil
	}
	// By name or alias: the longest name wins, so "Licht Schlafzimmer"
	// does not also match a bare "Licht".
	lower := strings.ToLower(value)
	var found []string
	for id, names := range snap.EntityNames {
		if !known[id] {
			continue
		}
		for _, n := range names {
			if n != id && len(n) >= 3 && strings.Contains(lower, n) {
				found = append(found, id)
				break
			}
		}
	}
	if len(found) == 0 {
		return "", errors.New(m.texts().noEntityNamed)
	}
	sort.Strings(found)
	return value + " (" + strings.Join(found, ", ") + ")", nil
}

// Finisher is the building block "tasks.automation": draft an automation
// from the thread's request and fields, propose it, and save it on yes.
func (m *Manager) Finisher() threads.Finisher { return automationFinisher{m} }

type automationFinisher struct{ m *Manager }

func (automationFinisher) Async() bool { return true }

func (f automationFinisher) Work(ctx context.Context, th store.Thread) (threads.Result, error) {
	from := Origin{Person: th.Person}
	from.Area, _ = strings.CutPrefix(th.Place, "area:")
	if from.Area == th.Place {
		from.Area = "" // not at a satellite: Telegram or the app chat
	}
	d, err := f.m.DraftAutomation(ctx, th.Request, th.Fields, from)
	var question ErrQuestion
	var cannot ErrCannot
	switch {
	case errors.As(err, &question):
		return threads.Result{Question: question.Question}, nil
	case errors.As(err, &cannot):
		return threads.Result{Cancel: fmt.Sprintf(f.m.texts().cannot, th.Request, cannot.Reason)}, nil
	case err != nil:
		return threads.Result{}, err
	}
	content, _ := json.Marshal(d)
	return threads.Result{Proposal: f.m.ProposalText(d), Content: content}, nil
}

func (f automationFinisher) Commit(ctx context.Context, th store.Thread) (string, error) {
	var d Draft
	if err := json.Unmarshal(th.Content, &d); err != nil || d.Automation == nil {
		return "", errors.New("the thread holds no draft")
	}
	if err := f.m.SaveAutomation(ctx, NewAutomationID(), d.Automation); err != nil {
		return "", err
	}
	return fmt.Sprintf(f.m.texts().confirmed, d.Title), nil
}
