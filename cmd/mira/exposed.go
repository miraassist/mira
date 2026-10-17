package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/miraassist/mira/internal/store"
)

// kvExposed is what Home Assistant exposes to Assist, as the Mira
// integration last sent it (docs/spec/ha-anbindung.md). It is kept, so it
// holds across restarts of the agent until the integration sends anew.
const kvExposed = "ha_exposed"

func (a *app) setExposed(ctx context.Context, ids []string) error {
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if err := a.store.SetKV(ctx, kvExposed, string(raw)); err != nil {
		return err
	}
	a.structure.Invalidate()
	return nil
}

// exposed is the kept list; false when the integration never sent one.
func (a *app) exposed(ctx context.Context) (map[string]bool, bool) {
	raw, err := a.store.GetKV(ctx, kvExposed)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			a.log.Warn("could not read what Assist sees", "error", err)
		}
		return nil, false
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, false
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, true
}
