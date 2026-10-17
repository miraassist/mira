package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
)

// Actions is what an action finish needs of Home Assistant
// (docs/spec/skill-aktionen.md).
type Actions interface {
	// Has reports whether Home Assistant offers the action, e.g. a
	// satellite's esphome.show_alarm_set.
	Has(ctx context.Context, action string) (bool, error)
	Call(ctx context.Context, action string, data map[string]any) error
	// Place gives the placeholders of an area: area, area_name, satellite,
	// satellite.device.
	Place(ctx context.Context, area string) map[string]string
}

// undoWindow is how long after an action a correction can take it back.
const undoWindow = 15 * time.Minute

// done is what an action finish keeps: what it did, and how to take it
// back.
type done struct {
	Action string         `json:"action"`
	Data   map[string]any `json:"data"`
	Undo   *done          `json:"undo,omitempty"`
}

// actionFinish calls the Home Assistant action a skill names, at once or
// after a yes.
type actionFinish struct {
	m  *Manager
	sk skills.Skill
}

func (actionFinish) Async() bool { return false }

func (f actionFinish) Work(ctx context.Context, th store.Thread) (Result, error) {
	t := f.m.texts()
	d, err := f.render(ctx, th)
	if err != nil {
		return Result{Cancel: err.Error()}, nil
	}
	ok, err := f.m.opts.Actions.Has(ctx, d.Action)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{Cancel: fmt.Sprintf(t.noAction, d.Action)}, nil
	}
	// The thread keeps what was (or will be) called, with its undo, for a
	// correction.
	content, _ := json.Marshal(d)
	if f.sk.Target.Confirm {
		return Result{Proposal: fmt.Sprintf(t.actionProposal, th.Request, describeData(d.Data)), Content: content}, nil
	}
	if err := f.call(ctx, th, d); err != nil {
		return Result{}, err
	}
	return Result{Done: fmt.Sprintf(t.actionDone, d.Action, describeData(d.Data)), Content: content}, nil
}

func (f actionFinish) Commit(ctx context.Context, th store.Thread) (string, error) {
	var d done
	if err := json.Unmarshal(th.Content, &d); err != nil || d.Action == "" {
		return "", errors.New("the thread holds no action")
	}
	if err := f.call(ctx, th, d); err != nil {
		return "", err
	}
	return fmt.Sprintf(f.m.texts().actionDone, d.Action, describeData(d.Data)), nil
}

func (f actionFinish) call(ctx context.Context, th store.Thread, d done) error {
	if err := f.m.opts.Actions.Call(ctx, d.Action, d.Data); err != nil {
		return err
	}
	f.m.log.Info("thread action called", "thread", th.ID, "action", d.Action)
	return nil
}

// render fills the placeholders of the finish and its undo.
func (f actionFinish) render(ctx context.Context, th store.Thread) (done, error) {
	vars := map[string]string{}
	for k, v := range th.Fields {
		if !notNeeded(v) {
			vars[k] = v
		}
	}
	if area, ok := strings.CutPrefix(th.Place, "area:"); ok {
		for k, v := range f.m.opts.Actions.Place(ctx, area) {
			vars[k] = v
		}
	}
	if th.Person != "" {
		vars["person"] = th.Person
	}
	d, err := renderAction(*f.sk.Target.Finish.Action, vars)
	if err != nil {
		return done{}, fmt.Errorf(f.m.texts().notHere, err)
	}
	if u := f.sk.Target.Undo; u != nil {
		if undo, err := renderAction(*u, vars); err == nil {
			d.Undo = &undo
		}
	}
	return d, nil
}

var placeholder = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)

// renderAction fills an action's placeholders. Data stays even when it comes
// out empty (an optional field nobody filled): ESPHome actions want every
// field, and an empty one means something (days "" is once). A placeholder
// in the action itself that cannot be filled is an error: it would call the
// wrong thing.
func renderAction(a skills.Action, vars map[string]string) (done, error) {
	var missing []string
	fill := func(s string) string {
		return placeholder.ReplaceAllStringFunc(s, func(m string) string {
			name := placeholder.FindStringSubmatch(m)[1]
			v, ok := vars[name]
			if !ok || v == "" {
				missing = append(missing, name)
			}
			return v
		})
	}
	d := done{Action: fill(a.Action), Data: map[string]any{}}
	if len(missing) > 0 {
		return done{}, fmt.Errorf("%s", strings.Join(missing, ", "))
	}
	for k, v := range a.Data {
		if s, ok := v.(string); ok {
			d.Data[k] = strings.TrimSpace(fill(s))
			continue
		}
		d.Data[k] = v
	}
	return d, nil
}

func describeData(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, data[k]))
	}
	return strings.Join(parts, ", ")
}

// Undo takes back the newest action a thread did at this place in the last
// minutes, for a correction; the model then starts the thread anew.
func (m *Manager) Undo(ctx context.Context, place string) (string, error) {
	t := m.texts()
	recent, err := m.opts.Store.RecentThreads(ctx, place, m.opts.Clock().Add(-undoWindow))
	if err != nil {
		return "", err
	}
	for _, th := range recent {
		if th.State != store.ThreadDone || len(th.Content) == 0 {
			continue
		}
		var d done
		if json.Unmarshal(th.Content, &d) != nil || d.Undo == nil {
			continue
		}
		if err := m.opts.Actions.Call(ctx, d.Undo.Action, d.Undo.Data); err != nil {
			return "", err
		}
		m.logEntry(&th, "mira", t.undoneLogged)
		th.State = store.ThreadCancelled
		th.Content = nil
		if err := m.opts.Store.PutThread(ctx, th); err != nil {
			return "", err
		}
		m.log.Info("thread action undone", "thread", th.ID, "action", d.Undo.Action)
		return fmt.Sprintf(t.undone, th.Request), nil
	}
	return t.nothingToUndo, nil
}
