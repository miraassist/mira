package threads

import (
	"context"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// A thread of the house that Mira's code keeps for a target: one at a time,
// started when something comes, taken up from any room
// (docs/spec/messages.md, decisions 13 and 23: the answering machine).

// HouseThread is the open thread of the house for a target; false without
// one.
func (m *Manager) HouseThread(ctx context.Context, target string) (store.Thread, bool, error) {
	open, err := m.opts.Store.OpenThreads(ctx, store.PlaceHouse)
	if err != nil {
		return store.Thread{}, false, err
	}
	for _, th := range open {
		if th.Target == target {
			return th, true, nil
		}
	}
	return store.Thread{}, false, nil
}

// HousePlan writes the plan of the house's thread for a target, starting
// it when none is open. A deadline that is not zero becomes its expiry
// until a turn takes it up (decision 11); zero keeps what it has.
func (m *Manager) HousePlan(ctx context.Context, target, request, plan string, deadline time.Time) (store.Thread, bool, error) {
	th, ok, err := m.HouseThread(ctx, target)
	if err != nil {
		return store.Thread{}, false, err
	}
	if !ok {
		th, err = m.StartAt(ctx, Begin{Target: target, Request: request, Plan: plan, Deadline: deadline})
		return th, true, err
	}
	th, err = m.change(ctx, th.ID, func(th *store.Thread) error {
		c := planOf(*th)
		c.Plan = strings.TrimSpace(plan)
		m.finished(&c)
		setPlan(th, c)
		if !deadline.IsZero() {
			th.Deadline = deadline
		}
		m.extend(th)
		return nil
	})
	return th, false, err
}

// TakeHousePlan is HousePlan from a turn: it writes the plan and makes the
// house's thread the turn's own (the answering machine's voicemail_list).
func (m *Manager) TakeHousePlan(ctx context.Context, inv tools.Invocation, target, request, plan string) (string, error) {
	th, _, err := m.HousePlan(ctx, target, request, plan, time.Time{})
	if err != nil {
		return "", err
	}
	m.link(ctx, inv.SessionKey, th)
	return th.ID, nil
}

// HouseDone ticks every step of the house's thread for a target: what it
// held is done elsewhere (the box's count went to 0, decision 21). The plan
// then stays the time a done plan stays. It returns the thread, false
// without one.
func (m *Manager) HouseDone(ctx context.Context, target string) (store.Thread, bool, error) {
	th, ok, err := m.HouseThread(ctx, target)
	if err != nil || !ok {
		return th, ok, err
	}
	became := false
	th, err = m.change(ctx, th.ID, func(th *store.Thread) error {
		c := planOf(*th)
		was := c.DoneAt != nil
		c.Plan = strings.ReplaceAll(c.Plan, "- [ ]", "- [x]")
		m.finished(&c)
		became = !was && c.DoneAt != nil
		setPlan(th, c)
		m.extend(th)
		return nil
	})
	if err == nil && became {
		m.done(ctx, th)
	}
	return th, true, err
}

// HousePlanText is the plan of the house's thread for a target; "" without
// one, or when it cannot be read (logged).
func (m *Manager) HousePlanText(ctx context.Context, target string) string {
	th, ok, err := m.HouseThread(ctx, target)
	if err != nil {
		m.log.Warn("could not read the house's thread; its plan counts as empty", "target", target, "error", err)
		return ""
	}
	if !ok {
		return ""
	}
	return planOf(th).Plan
}
