package alarm

import (
	"fmt"
	"strings"
)

const (
	kindAlarm = "alarm"
	// volume is what an alarm rings at (docs/spec/alarm.md, decision 7).
	volume = 0.9
	// ringMinutes is how long it rings without a reaction (decision 5).
	ringMinutes = 10
)

// spec is what one alarm's automation is made of.
type spec struct {
	ID, Title, Time    string
	Days               []string // nil: once
	Owner, Area        string
	Satellite, Speaker string
	RiseURL, LoopURL   string
	FailedText         string
}

// build is the automation of one alarm (docs/spec/alarm.md, The automation).
// One run does everything in order, so that it keeps the volume it read at
// the start to the end: the speaker reachable, else a message to the person;
// the volume to 90 %; the wake sound round by round, after each round
// waiting for the satellite listening (a reaction), the speaker idle (the
// round over) or the cancel event; after a reaction until the satellite is
// idle again; then the volume back.
//
// The alias ends in a tag unique to the alarm: Home Assistant restores a new
// entity's state from the last one with the same entity id, and a once-alarm
// must not inherit "already rang" from an earlier one.
func build(s spec) map[string]any {
	const waited = "wait.trigger is not none and wait.trigger.id"
	condition := map[string]any{"condition": "template", "value_template": "{{ this.attributes.last_triggered is none }}"}
	if len(s.Days) > 0 {
		condition = map[string]any{"condition": "time", "weekday": s.Days}
	}
	receiver := s.Owner
	if receiver == "" {
		receiver = "house"
	}
	speaker := map[string]any{"entity_id": s.Speaker}
	return map[string]any{
		"alias":       s.Title + " " + s.ID[max(len(s.ID)-8, 0):],
		"description": "Mira: " + s.Title,
		"variables": map[string]any{
			"mira_title": s.Title, "mira_time": s.Time, "mira_days": strings.Join(s.Days, ","),
			"mira_owner": s.Owner, "mira_area": s.Area, "mira_kind": kindAlarm,
			"mira_satellite": s.Satellite, "mira_speaker": s.Speaker,
		},
		"triggers":   []any{map[string]any{"trigger": "time", "at": s.Time + ":00", "id": "ring"}},
		"conditions": []any{condition},
		"mode":       "single",
		"actions": []any{
			map[string]any{"variables": map[string]any{
				"before": fmt.Sprintf("{{ state_attr('%s', 'volume_level') }}", s.Speaker),
				"until":  fmt.Sprintf("{{ (now() + timedelta(minutes=%d)).isoformat() }}", ringMinutes),
			}},
			map[string]any{
				"if": []any{map[string]any{"condition": "state", "entity_id": s.Speaker, "state": []string{"unavailable", "unknown"}}},
				"then": []any{
					map[string]any{"action": "mira.send_message", "data": map[string]any{"text": s.FailedText, "receiver": receiver}},
					map[string]any{"stop": "the speaker cannot be reached"},
				},
			},
			map[string]any{"action": "media_player.volume_set", "target": speaker, "data": map[string]any{"volume_level": volume}},
			map[string]any{"repeat": map[string]any{
				"sequence": []any{
					map[string]any{"action": "media_player.play_media", "target": speaker, "data": map[string]any{
						"media_content_id":   fmt.Sprintf("{{ '%s' if repeat.first else '%s' }}", s.RiseURL, s.LoopURL),
						"media_content_type": "music",
						"announce":           true,
					}},
					map[string]any{
						"wait_for_trigger": []any{
							map[string]any{"trigger": "state", "entity_id": s.Satellite, "to": "listening", "id": "reaction"},
							map[string]any{"trigger": "state", "entity_id": s.Speaker, "to": "idle", "id": "round_over"},
							map[string]any{"trigger": "event", "event_type": CancelEvent,
								"event_data": map[string]any{"automation": s.ID}, "id": "cancel"},
						},
						// A safety net only: a round is 24 s or 10.4 s.
						"timeout": "00:01:00",
					},
				},
				"until": fmt.Sprintf("{{ (%s in ['reaction', 'cancel']) or now() >= as_datetime(until) }}", waited),
			}},
			map[string]any{
				"if":   fmt.Sprintf("{{ %s == 'cancel' }}", waited),
				"then": []any{map[string]any{"action": "media_player.media_stop", "target": speaker}},
			},
			map[string]any{
				"if": fmt.Sprintf("{{ %s == 'reaction' }}", waited),
				"then": []any{map[string]any{
					"wait_for_trigger": []any{map[string]any{"trigger": "state", "entity_id": s.Satellite, "to": "idle"}},
					// A safety net: a turn that never ends.
					"timeout": "00:02:00",
				}},
			},
			map[string]any{
				"if":   "{{ before is number }}",
				"then": []any{map[string]any{"action": "media_player.volume_set", "target": speaker, "data": map[string]any{"volume_level": "{{ before }}"}}},
			},
		},
	}
}
