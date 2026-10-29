package ws

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Mira reads states only as Home Assistant filters them: subscribe_entities
// with the entities or domains she needs, never every state in the house
// (docs/spec/ha-anbindung.md, Watching states). get_states has no filter,
// so the client does not offer it.

// StateFilter says which states to read: these entities, and every entity
// of these domains. Home Assistant applies it.
type StateFilter struct {
	EntityIDs []string
	Domains   []string
}

func (f StateFilter) empty() bool { return len(f.EntityIDs) == 0 && len(f.Domains) == 0 }

func (f StateFilter) command() map[string]any {
	cmd := map[string]any{"type": CmdSubscribeEntities}
	if len(f.EntityIDs) > 0 {
		cmd["entity_ids"] = f.EntityIDs
	}
	if len(f.Domains) > 0 {
		cmd["include"] = map[string]any{"domains": f.Domains}
	}
	return cmd
}

// GetStates reads the states that match f as they are now: it subscribes
// with f, takes Home Assistant's first answer and unsubscribes. Without an
// entity or a domain there is nothing to read.
func (c *Client) GetStates(ctx context.Context, f StateFilter) ([]State, error) {
	if f.empty() {
		return nil, nil
	}
	var table stateTable
	first := make(chan struct{})
	var once sync.Once
	cancel, err := c.subscribeCommand(ctx, f.command(), func(ev Event) {
		table.apply(ev.Raw)
		once.Do(func() { close(first) })
	})
	if err != nil {
		return nil, err
	}
	defer cancel()
	select {
	case <-first:
		return table.all(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// StateWatch keeps the states that match a filter, as Home Assistant
// reports them, for as long as its subscription runs.
type StateWatch struct {
	table  stateTable
	mu     sync.Mutex
	ready  bool
	cancel func()
}

// WatchStates keeps the states that match f up to date: a subscribe_entities
// with f that stays, restored after a reconnect like every subscription.
// changed, if given, runs after every message; it runs on the connection's
// read loop, so it must not block.
func (c *Client) WatchStates(ctx context.Context, f StateFilter, changed func()) (*StateWatch, error) {
	if f.empty() {
		return nil, errors.New("a watch needs entities or domains")
	}
	w := &StateWatch{}
	cancel, err := c.subscribeCommand(ctx, f.command(), func(ev Event) {
		w.table.apply(ev.Raw)
		w.mu.Lock()
		w.ready = true
		w.mu.Unlock()
		if changed != nil {
			changed()
		}
	})
	if err != nil {
		return nil, err
	}
	w.cancel = cancel
	return w, nil
}

// Ready says whether Home Assistant has sent the states yet.
func (w *StateWatch) Ready() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ready
}

// States are the states it keeps.
func (w *StateWatch) States() []State { return w.table.all() }

// State is one entity's state; false when it has none.
func (w *StateWatch) State(id string) (State, bool) { return w.table.get(id) }

// Stop ends the subscription.
func (w *StateWatch) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

// WatchState calls fn with one entity's state as it is now and on every
// change of it, from subscribe_entities filtered to that entity: Home
// Assistant sends what changed, nothing is polled and no other entity's
// state comes along. An entity Home Assistant does not know is "".
//
// fn runs on the connection's read loop, so it must not block.
func (c *Client) WatchState(ctx context.Context, entity string, fn func(state string)) (func(), error) {
	var table stateTable
	var mu sync.Mutex
	last, first := "", true
	return c.subscribeCommand(ctx, StateFilter{EntityIDs: []string{entity}}.command(), func(ev Event) {
		table.apply(ev.Raw)
		s, _ := table.get(entity)
		mu.Lock()
		changed := first || s.State != last
		last, first = s.State, false
		mu.Unlock()
		if changed {
			fn(s.State)
		}
	})
}

// WaitState waits until ok accepts the entity's state, for at most limit,
// and reports whether it did. It watches the entity (WatchState) instead of
// asking for states again and again.
func (c *Client) WaitState(ctx context.Context, entity string, limit time.Duration, ok func(state string) bool) bool {
	done := make(chan struct{})
	var once sync.Once
	cancel, err := c.WatchState(ctx, entity, func(s string) {
		if ok(s) {
			once.Do(func() { close(done) })
		}
	})
	if err != nil {
		return false
	}
	defer cancel()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}
