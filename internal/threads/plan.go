package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// Plans (docs/spec/vorgaenge.md, Pläne): a thread of a target with plan:
// true carries a Markdown text the model writes, for a run that lasts
// longer than a conversation (cooking). The core understands only its
// checkboxes: it shows the step before, the current and the next one in the
// context, ticks them off on request, and ends the thread a while after the
// last one.

const (
	// maxPlan bounds a plan.
	maxPlan = 8000
	// shortPlan is how many steps a plan may have for the context to show
	// all its open ones (the answering machine's messages); a longer one
	// (a recipe) shows the step before, the current and the next.
	shortPlan = 10
	// maxStepLine bounds a step as the context shows it.
	maxStepLine = 200
)

// planContent is a plan thread's content.
type planContent struct {
	Plan string `json:"plan"`
	// Foreign: written in a turn that had third-party text before it (a
	// web page); it stays so.
	Foreign bool `json:"foreign,omitempty"`
	// DoneAt is when the last checkbox was ticked; the thread ends the
	// target's done_after later.
	DoneAt *time.Time `json:"done_at,omitempty"`
}

func (m *Manager) isPlan(th store.Thread) bool {
	sk, ok := m.targets[th.Target]
	return ok && sk.Target.Plan
}

func planOf(th store.Thread) planContent {
	var c planContent
	if len(th.Content) > 0 {
		_ = json.Unmarshal(th.Content, &c)
	}
	return c
}

func setPlan(th *store.Thread, c planContent) {
	raw, _ := json.Marshal(c)
	th.Content = raw
}

// box is one checkbox of a plan.
type box struct {
	line int
	done bool
	text string
}

var reBox = regexp.MustCompile(`^(\s*[-*+]\s+\[)([ xX])(\]\s*)(.*)$`)

func boxes(plan string) []box {
	var out []box
	for i, line := range strings.Split(plan, "\n") {
		if m := reBox.FindStringSubmatch(line); m != nil {
			out = append(out, box{line: i, done: m[2] != " ", text: strings.TrimSpace(m[4])})
		}
	}
	return out
}

// tick sets checkbox n (from 1) and returns the plan.
func tick(plan string, n int, done bool) (string, error) {
	bs := boxes(plan)
	if n < 1 || n > len(bs) {
		return "", fmt.Errorf("step %d: the plan has %d steps", n, len(bs))
	}
	lines := strings.Split(plan, "\n")
	mark := " "
	if done {
		mark = "x"
	}
	m := reBox.FindStringSubmatch(lines[bs[n-1].line])
	lines[bs[n-1].line] = m[1] + mark + m[3] + m[4]
	return strings.Join(lines, "\n"), nil
}

// finished notes when all checkboxes became done, and forgets it when one
// is open again.
func (m *Manager) finished(c *planContent) {
	bs := boxes(c.Plan)
	all := len(bs) > 0
	for _, b := range bs {
		all = all && b.done
	}
	switch {
	case all && c.DoneAt == nil:
		now := m.opts.Clock()
		c.DoneAt = &now
	case !all:
		c.DoneAt = nil
	}
}

// SetPlan writes a plan from Mira's own code rather than the model, where
// the plan is facts already (the answering machine's messages,
// docs/spec/anrufbeantworter.md): it takes the target's plan thread
// running at the turn's place, or starts one, writes text as its plan and
// links the turn to it. It returns the thread's id.
func (m *Manager) SetPlan(ctx context.Context, inv tools.Invocation, target, request, text string) (string, error) {
	sk, ok := m.targets[target]
	if !ok || !sk.Target.Plan {
		return "", fmt.Errorf(m.texts().noTarget, target)
	}
	place, _ := m.startPlace(ctx, inv, target)
	var th store.Thread
	if open, err := m.openAt(ctx, m.turnPlaces(ctx, inv)); err == nil {
		for _, o := range open {
			if o.Target == target {
				th = o
				break
			}
		}
	}
	if th.ID == "" {
		th = store.Thread{ID: strings.ToLower(ulid.Make().String()), Target: target, Place: place, Person: inv.Person,
			State: store.ThreadCollecting, Request: strings.TrimSpace(request), Fields: map[string]string{},
			Area: areaOf(m.withTurnArea(ctx, inv))}
		m.logEntry(&th, "user", th.Request)
		if err := m.save(ctx, &th); err != nil {
			return "", err
		}
		m.log.Info("thread started", "thread", th.ID, "target", target, "place", place)
	}
	th, err := m.change(ctx, th.ID, func(th *store.Thread) error {
		c := planOf(*th)
		c.Plan = strings.TrimSpace(text)
		m.finished(&c)
		setPlan(th, c)
		m.extend(th)
		return nil
	})
	if err != nil {
		return "", err
	}
	m.link(ctx, inv.SessionKey, th)
	return th.ID, nil
}

// PlanThread is the id of the target's plan thread the turn belongs to or
// that runs at its place; "" without one.
func (m *Manager) PlanThread(ctx context.Context, inv tools.Invocation, target string) string {
	th, err := m.current(ctx, inv)
	if err != nil || th.Target != target {
		return ""
	}
	return th.ID
}

// PlanText is the plan of the target's thread the turn belongs to or that
// runs at its places; "" without one.
func (m *Manager) PlanText(ctx context.Context, inv tools.Invocation, target string) string {
	th, err := m.current(ctx, inv)
	if err != nil || th.Target != target {
		return ""
	}
	return planOf(th).Plan
}

// TickPlan ticks, from Mira's own code, the first open step of a thread's
// plan that contains match (a message's "(id 0:13)"); nothing when there is
// none.
func (m *Manager) TickPlan(ctx context.Context, threadID, match string) error {
	_, err := m.change(ctx, threadID, func(th *store.Thread) error {
		c := planOf(*th)
		for i, b := range boxes(c.Plan) {
			if b.done || !strings.Contains(b.text, match) {
				continue
			}
			plan, err := tick(c.Plan, i+1, true)
			if err != nil {
				return err
			}
			c.Plan = plan
			m.finished(&c)
			setPlan(th, c)
			m.extend(th)
			return nil
		}
		return nil
	})
	return err
}

// Plan runs an action of the plan tool on the turn's plan thread: write,
// check, uncheck or read.
func (m *Manager) Plan(ctx context.Context, inv tools.Invocation, action, text string, step int) (string, error) {
	t := m.texts()
	th, err := m.current(ctx, inv)
	if err != nil || !m.isPlan(th) {
		return "", errors.New(t.noPlanThread)
	}
	switch action {
	case "read":
		c := planOf(th)
		if c.Plan == "" {
			return "", errors.New(t.planNone)
		}
		return m.planBlock(c, c.Plan), nil
	case "write", "check", "uncheck":
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
	// Applied to the plan as it is now: another run may have ticked a step
	// meanwhile (docs/spec/agenten.md, Shared state).
	th, err = m.change(ctx, th.ID, func(th *store.Thread) error {
		c := planOf(*th)
		switch action {
		case "write":
			text = strings.TrimSpace(text)
			if text == "" {
				return errors.New(t.planEmpty)
			}
			if n := len([]rune(text)); n > maxPlan {
				return fmt.Errorf(t.planTooLong, n, maxPlan)
			}
			c.Plan = text
			c.Foreign = c.Foreign || inv.Foreign
			m.logEntry(th, "mira", fmt.Sprintf(t.planWritten, len(boxes(text))))
		case "check", "uncheck":
			if c.Plan == "" {
				return errors.New(t.planNone)
			}
			plan, err := tick(c.Plan, step, action == "check")
			if err != nil {
				return err
			}
			c.Plan = plan
		}
		m.finished(&c)
		setPlan(th, c)
		m.extend(th)
		return nil
	})
	if err != nil {
		return "", err
	}
	return m.planStatus(th), nil
}

// planBlock is plan text as the model sees it: marked as data when foreign.
func (m *Manager) planBlock(c planContent, text string) string {
	if !c.Foreign {
		return text
	}
	return m.texts().planForeign + "\n<<<\n" + text + "\n>>>"
}

// planStatus is a plan thread's state for the model: the window around the
// current step, or only a reference for a plan without checkboxes.
func (m *Manager) planStatus(th store.Thread) string {
	t := m.texts()
	c := planOf(th)
	if c.Plan == "" {
		return t.planNone
	}
	bs := boxes(c.Plan)
	if len(bs) == 0 {
		return fmt.Sprintf(t.planNoSteps, len([]rune(c.Plan)))
	}
	done, cur, next := 0, -1, -1
	for i, b := range bs {
		switch {
		case b.done:
			done++
		case cur < 0:
			cur = i
		case next < 0:
			next = i
		}
	}
	prev := -1
	end := cur
	if end < 0 {
		end = len(bs)
	}
	for i := end - 1; i >= 0; i-- {
		if bs[i].done {
			prev = i
			break
		}
	}
	var lines []string
	step := func(format string, i int) {
		if i >= 0 {
			lines = append(lines, fmt.Sprintf(format, i+1, clip(bs[i].text, maxStepLine)))
		}
	}
	lines = append(lines, fmt.Sprintf(t.planProgress, done, len(bs)))
	step(t.planPrevious, prev)
	step(t.planCurrent, cur)
	if len(bs) <= shortPlan {
		// A short plan shows every open step after the current one: the
		// model plays or reads on without reading the plan first.
		for i := cur + 1; cur >= 0 && i < len(bs); i++ {
			if !bs[i].done {
				step(t.planNext, i)
			}
		}
	} else {
		step(t.planNext, next)
	}
	if c.DoneAt != nil {
		lines = append(lines, fmt.Sprintf(t.planAllDone, c.DoneAt.Add(m.targets[th.Target].Target.DoneAfterDuration()).In(time.Local).Format("15:04")))
	}
	text := strings.Join(lines, "\n")
	if c.Foreign {
		text = m.planBlock(c, text)
	}
	return text
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// PlanView is a running plan, for the web page.
type PlanView struct {
	ID, Target, Place, Request string
	Plan                       string
	Done, Steps                int
	Finished                   bool
	Started, Updated           time.Time
}

// Plans lists the running plans.
func (m *Manager) Plans(ctx context.Context) ([]PlanView, error) {
	open, err := m.opts.Store.OpenThreads(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []PlanView
	for _, th := range open {
		if !m.isPlan(th) {
			continue
		}
		c := planOf(th)
		v := PlanView{ID: th.ID, Target: th.Target, Place: th.Place, Request: th.Request, Plan: c.Plan,
			Finished: c.DoneAt != nil, Started: th.CreatedAt, Updated: th.UpdatedAt}
		for _, b := range boxes(c.Plan) {
			v.Steps++
			if b.done {
				v.Done++
			}
		}
		out = append(out, v)
	}
	return out, nil
}
