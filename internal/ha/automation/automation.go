// Package automation saves and deletes Mira's automations, her tasks and
// timers, the way Home Assistant's automation editor does: through its
// automation config API, with the admin's token Mira was given
// (docs/spec/hacs-default.md, 3 and 4). The Mira integration only checks
// them before and labels them after. Mira notes the ones she created and
// deletes only those.
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/miraassist/mira/internal/excerpt"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// HA is what saving and deleting need of the Home Assistant client.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
	SaveAutomationConfig(ctx context.Context, id string, config map[string]any) error
	DeleteAutomationConfig(ctx context.Context, id string) error
}

// The labels the integration puts on a saved automation: a task ("Mira"), a
// timer ("Mira: einmalig"); Keep labels nothing, for a household task
// changed by Mira, which keeps its own.
const (
	LabelTask = "tasks"
	LabelOnce = "once"
	Keep      = ""
)

// Own is Mira's note of the automations she created (the store's table
// own_automations, docs/spec/hacs-default.md, decision 7).
type Own interface {
	RecordOwnAutomation(ctx context.Context, id, kind string) error
	IsOwnAutomation(ctx context.Context, id string) (bool, error)
	ForgetOwnAutomation(ctx context.Context, id string) error
	OwnAutomationsTakenOver(ctx context.Context) (bool, error)
	TakeOverOwnAutomations(ctx context.Context, ids map[string]string) error
}

// The kinds an own automation is noted with.
const (
	KindTask = "task"
	KindOnce = "once"
)

// ErrNotOwn: the automation is not one Mira created; it is not deleted.
var ErrNotOwn = errors.New("the automation was not created by Mira")

// ErrNotLabelled: the automation is saved and loaded, but has not got its
// label, so it does not show among Mira's tasks or timers.
var ErrNotLabelled = errors.New("the automation is saved but not labelled as Mira's")

// Save writes the automation, waits for Home Assistant to load it (the
// reload with its id returns once it is loaded: the signal that its entity
// exists, check C1) and labels it. A new one (a label given) is noted as
// Mira's as soon as Home Assistant took it. Each failure is logged here with
// what failed; the caller tells the user.
func Save(ctx context.Context, ha HA, own Own, log *slog.Logger, id string, config map[string]any, label string) error {
	if err := ha.SaveAutomationConfig(ctx, id, config); err != nil {
		log.Warn("could not save an automation", "automation", id, "error", err)
		return err
	}
	if label != Keep {
		kind := KindTask
		if label == LabelOnce {
			kind = KindOnce
		}
		if err := own.RecordOwnAutomation(ctx, id, kind); err != nil {
			// docs/spec/hacs-default.md, 4: saved all the same; Mira cannot
			// delete it later.
			log.Warn("an automation was saved but not noted as Mira's; she cannot delete it later", "automation", id, "error", err)
		}
	}
	if err := ha.CallService(ctx, "automation", "reload", map[string]any{"id": id}, nil); err != nil {
		log.Warn("an automation was saved but home assistant did not load it", "automation", id, "error", err)
		return fmt.Errorf("saved, but Home Assistant did not load it: %w", err)
	}
	if label == Keep {
		return nil
	}
	if _, err := ha.Call(ctx, map[string]any{"type": "mira/automation/label", "automation_id": id, "label": label}); err != nil {
		log.Warn("an automation was saved but not labelled as Mira's", "automation", id, "label", label, "error", err)
		return fmt.Errorf("%w: %w", ErrNotLabelled, err)
	}
	return nil
}

// Delete removes the automation if Mira created it; any other is ErrNotOwn.
func Delete(ctx context.Context, ha HA, own Own, log *slog.Logger, id string) error {
	mine, err := own.IsOwnAutomation(ctx, id)
	if err != nil {
		log.Warn("could not read whether mira created an automation; nothing deleted", "automation", id, "error", err)
		return err
	}
	if !mine {
		return ErrNotOwn
	}
	if err := ha.DeleteAutomationConfig(ctx, id); err != nil {
		log.Warn("could not delete an automation", "automation", id, "error", err)
		return err
	}
	if err := own.ForgetOwnAutomation(ctx, id); err != nil {
		log.Warn("an automation was deleted but its note stays; harmless", "automation", id, "error", err)
	}
	return nil
}

// TakeOver notes, once, the automations labelled as Mira's before she kept
// her own note of them: her tasks (mira/automation/list) and her timers
// (mira/once/list). Done before anything is deleted; a failure is tried
// again at the next look.
func TakeOver(ctx context.Context, ha HA, own Own, log *slog.Logger) error {
	done, err := own.OwnAutomationsTakenOver(ctx)
	if err != nil || done {
		return err
	}
	ids := map[string]string{}
	for _, l := range []struct{ cmd, key, kind string }{
		{"mira/automation/list", "automations", KindTask},
		{"mira/once/list", "timers", KindOnce},
	} {
		raw, err := ha.Call(ctx, map[string]any{"type": l.cmd})
		if err != nil {
			return fmt.Errorf("%s: %w", l.cmd, err)
		}
		var res map[string][]struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return fmt.Errorf("%s answered %q: %w", l.cmd, excerpt.Bytes(raw), err)
		}
		for _, a := range res[l.key] {
			if a.ID != "" {
				ids[a.ID] = l.kind
			}
		}
	}
	if err := own.TakeOverOwnAutomations(ctx, ids); err != nil {
		return err
	}
	log.Info("took over the automations labelled as mira's from before", "count", len(ids))
	return nil
}
