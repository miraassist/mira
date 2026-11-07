// Package automation saves and deletes Mira's automations, her tasks and
// timers, the way Home Assistant's automation editor does: through its
// automation config API, with the owner's token Mira has
// (docs/spec/hacs-default.md, 3). The Mira integration only checks them
// before and labels them after.
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

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

// ErrNotLabelled: the automation is saved and loaded, but has not got its
// label, so it does not show among Mira's tasks or timers.
var ErrNotLabelled = errors.New("the automation is saved but not labelled as Mira's")

// Save writes the automation, waits for Home Assistant to load it (the
// reload with its id returns once it is loaded: the signal that its entity
// exists, check C1) and labels it. Each failure is logged here with what
// failed; the caller tells the user.
func Save(ctx context.Context, ha HA, log *slog.Logger, id string, config map[string]any, label string) error {
	if err := ha.SaveAutomationConfig(ctx, id, config); err != nil {
		log.Warn("could not save an automation", "automation", id, "error", err)
		return err
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

// Delete removes the automation; the caller made sure it is Mira's.
func Delete(ctx context.Context, ha HA, log *slog.Logger, id string) error {
	if err := ha.DeleteAutomationConfig(ctx, id); err != nil {
		log.Warn("could not delete an automation", "automation", id, "error", err)
		return err
	}
	return nil
}
