package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// SubscribeTrigger watches entities like a Home Assistant state trigger
// (trigger: state, entity_id, to, from, not_to, not_from) and calls handler
// with an event shaped like the one subscribe_trigger delivers.
//
// It does not use subscribe_trigger: Home Assistant allows that command to
// admins only, and the agent runs as a restricted user. subscribe_entities is
// open to every user and is filtered to the watched entities, so the agent
// still does not see every state change in the house.
//
// Only state changes fire, not attribute changes. A change that happened
// while the connection was down fires once the subscription is restored.
func (c *Client) SubscribeTrigger(ctx context.Context, trigger map[string]any, handler EventHandler) (func(), error) {
	st, err := parseStateTrigger(trigger)
	if err != nil {
		return nil, err
	}
	w := &stateWatch{trigger: st, handler: handler, states: map[string]State{}}
	return c.subscribeCommand(ctx,
		map[string]any{"type": CmdSubscribeEntities, "entity_ids": st.entityIDs}, w.handle)
}

type stateTrigger struct {
	entityIDs                []string
	to, from, notTo, notFrom []string
}

func parseStateTrigger(t map[string]any) (stateTrigger, error) {
	kind, _ := t["trigger"].(string)
	if kind == "" {
		kind, _ = t["platform"].(string)
	}
	if kind != "state" {
		return stateTrigger{}, fmt.Errorf("only state triggers are supported, got %q", kind)
	}
	var st stateTrigger
	var err error
	for key, dst := range map[string]*[]string{
		"entity_id": &st.entityIDs, "to": &st.to, "from": &st.from,
		"not_to": &st.notTo, "not_from": &st.notFrom,
	} {
		if *dst, err = stringList(t[key]); err != nil {
			return stateTrigger{}, fmt.Errorf("trigger %s: %w", key, err)
		}
	}
	if len(st.entityIDs) == 0 {
		return stateTrigger{}, errors.New("state trigger needs an entity_id")
	}
	return st, nil
}

func stringList(v any) ([]string, error) {
	switch v := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{v}, nil
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			s, ok := x.(string)
			if !ok {
				return nil, fmt.Errorf("%v is not a string", x)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected %T", v)
}

func (t stateTrigger) matches(from, to string) bool {
	if from == to {
		return false
	}
	if len(t.from) > 0 && !slices.Contains(t.from, from) {
		return false
	}
	if len(t.to) > 0 && !slices.Contains(t.to, to) {
		return false
	}
	return !slices.Contains(t.notFrom, from) && !slices.Contains(t.notTo, to)
}

// stateWatch keeps the last known state of each watched entity, because
// subscribe_entities sends only what changed.
type stateWatch struct {
	trigger stateTrigger
	handler EventHandler
	mu      sync.Mutex
	states  map[string]State
}

// compressedState is Home Assistant's compact state form in
// subscribe_entities: s = state, a = attributes, lc = last changed.
type compressedState struct {
	S  *string        `json:"s"`
	A  map[string]any `json:"a"`
	LC *float64       `json:"lc"`
}

// entitiesEvent is one subscribe_entities message: a = full states (the
// first message, and again after every resubscribe), c = diffs, r = removed.
type entitiesEvent struct {
	Add    map[string]compressedState `json:"a"`
	Change map[string]struct {
		Plus  compressedState `json:"+"`
		Minus struct {
			A []string `json:"a"`
		} `json:"-"`
	} `json:"c"`
	Remove []string `json:"r"`
}

func (w *stateWatch) handle(ev Event) {
	var msg entitiesEvent
	if err := json.Unmarshal(ev.Raw, &msg); err != nil {
		return
	}
	var fire [][2]State
	w.mu.Lock()
	for id, cs := range msg.Add {
		next := State{EntityID: id, Attributes: cs.A}
		if cs.S != nil {
			next.State = *cs.S
		}
		next.LastChanged = lastChanged(cs.LC)
		if prev, known := w.states[id]; known && w.trigger.matches(prev.State, next.State) {
			fire = append(fire, [2]State{prev, next})
		}
		w.states[id] = next
	}
	for id, diff := range msg.Change {
		prev, known := w.states[id]
		if !known {
			continue
		}
		next := prev
		next.Attributes = make(map[string]any, len(prev.Attributes))
		for k, v := range prev.Attributes {
			next.Attributes[k] = v
		}
		for k, v := range diff.Plus.A {
			next.Attributes[k] = v
		}
		for _, k := range diff.Minus.A {
			delete(next.Attributes, k)
		}
		if diff.Plus.S != nil {
			next.State = *diff.Plus.S
		}
		if diff.Plus.LC != nil {
			next.LastChanged = lastChanged(diff.Plus.LC)
		}
		if w.trigger.matches(prev.State, next.State) {
			fire = append(fire, [2]State{prev, next})
		}
		w.states[id] = next
	}
	for _, id := range msg.Remove {
		delete(w.states, id)
	}
	w.mu.Unlock()

	for _, f := range fire {
		var v TriggerVariables
		v.Trigger.Platform = "state"
		v.Trigger.EntityID = f[1].EntityID
		v.Trigger.FromState, v.Trigger.ToState = &f[0], &f[1]
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		w.handler(Event{Variables: raw})
	}
}

func lastChanged(ts *float64) string {
	if ts == nil {
		return ""
	}
	sec := int64(*ts)
	return time.Unix(sec, int64((*ts-float64(sec))*1e9)).UTC().Format(time.RFC3339Nano)
}
