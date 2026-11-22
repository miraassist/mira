package timer

import (
	"fmt"
	"time"
)

// maxPerOwnerArea is how many open timers one person may have in one room.
const maxPerOwnerArea = 10

// slack is how far the clock may be behind the trigger.
const slack = 2 * time.Second

// once is what one timer's automation is made of.
type once struct {
	ID, Title, Owner, Area, Kind string
	Due                          time.Time
	Actions                      []map[string]any
}

// buildOnce is the automation of one timer (docs/spec/scheduled-actions.md,
// The automation). It fires at the due second and again when Home Assistant
// starts, but its condition is "not before the due time, and never ran" – so
// a timer that was due while Home Assistant was down runs as soon as it is
// back, and never twice.
//
// The alias ends in a tag unique to the timer: Home Assistant makes the
// entity id from the alias and restores a new entity's state from the last
// one with that id, even a removed one; a second "Schlaf-Timer Wohnzimmer"
// would inherit "already ran" from the first and never run.
func buildOnce(o once, loc *time.Location) map[string]any {
	local := o.Due.In(loc).Truncate(time.Second)
	stamp := local.Format(time.RFC3339)
	actions := make([]any, len(o.Actions))
	for i, a := range o.Actions {
		actions[i] = a
	}
	return map[string]any{
		"alias":       o.Title + " " + o.ID[max(len(o.ID)-8, 0):],
		"description": "Mira: " + o.Title,
		"variables": map[string]any{
			"mira_title": o.Title, "mira_due": stamp, "mira_owner": o.Owner, "mira_area": o.Area, "mira_kind": o.Kind,
		},
		"triggers": []any{
			map[string]any{"trigger": "time", "at": local.Format("15:04:05")},
			map[string]any{"trigger": "homeassistant", "event": "start"},
		},
		"conditions": []any{map[string]any{
			"condition": "template",
			"value_template": fmt.Sprintf("{{ now() >= as_datetime('%s') - timedelta(seconds=%d) and this.attributes.last_triggered is none }}",
				stamp, int(slack.Seconds())),
		}},
		"actions": actions,
		"mode":    "single",
	}
}
