package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/miraassist/mira/internal/store"
)

// kvExposed is what Home Assistant exposes to Assist, as the Mira
// integration last sent it (docs/spec/ha-integration.md). It is kept, so it
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
	// At once: a turn right after a new list was exposed must see it
	// (docs/spec/context.md, 1.3).
	a.structure.Now()
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
		// docs/spec/fallbacks.md, row 32.
		a.log.Warn("the kept list of what Assist sees cannot be read; it counts as not sent", "error", err)
		return nil, false
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, true
}

// exposedHash is a fingerprint of the kept list, for /v1/status: the
// integration compares it with its own and sends the list again when they
// differ (a late-discovered device, an agent that started with an empty
// database). "" until the integration sent its settings since the start:
// Mira holds them in memory only, and the empty fingerprint makes it send
// them again. The integration computes the same: sha256 of the sorted ids
// joined by newlines.
func (a *app) exposedHash(ctx context.Context) string {
	a.liveMu.Lock()
	sent := a.haSent
	a.liveMu.Unlock()
	if !sent {
		return ""
	}
	raw, err := a.store.GetKV(ctx, kvExposed)
	if err != nil {
		// docs/spec/fallbacks.md, row 32: the integration sends the list again.
		if !errors.Is(err, store.ErrNotFound) {
			a.log.Warn("could not read what Assist sees for its fingerprint; the integration sends it again", "error", err)
		}
		return ""
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		a.log.Warn("the kept list of what Assist sees cannot be read; the integration sends it again", "error", err)
		return ""
	}
	slices.Sort(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:])
}

// kvLogins are the login names of the persons' Home Assistant users, by
// person entity, as the integration last sent them. A calendar named like a
// person's login is theirs (docs/spec/calendar.md).
const kvLogins = "ha_logins"

func (a *app) setLogins(ctx context.Context, logins map[string]string) error {
	raw, err := json.Marshal(logins)
	if err != nil {
		return err
	}
	return a.store.SetKV(ctx, kvLogins, string(raw))
}

// logins is the kept map; empty when the integration never sent one.
func (a *app) logins(ctx context.Context) map[string]string {
	out := map[string]string{}
	raw, err := a.store.GetKV(ctx, kvLogins)
	if err != nil {
		// docs/spec/fallbacks.md, row 32.
		if !errors.Is(err, store.ErrNotFound) {
			a.log.Warn("could not read the persons' logins; no calendar counts as a person's", "error", err)
		}
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		a.log.Warn("the kept logins of the persons cannot be read; no calendar counts as a person's", "error", err)
		return map[string]string{}
	}
	return out
}
