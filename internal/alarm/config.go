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
	// lightLead is how long before the alarm the light starts to rise, and
	// how long it takes to be full (decision 11); a constant, not a setting.
	lightLead = 10
	// radioStart is how long the run waits for a station to play before it
	// tells the person the alarm could not ring; a safety net, the signal is
	// the player going to playing.
	radioStart = "00:00:30"
)

// Station is a station an alarm wakes with, found when it is set: the
// player that plays it and what play_media gets.
type Station struct {
	Player, ContentID, ContentType, Title string
}

// spec is what one alarm's automation is made of.
type spec struct {
	ID, Title, Time    string
	Days               []string // nil: once, on Date
	Date               string   // a once-alarm's day, YYYY-MM-DD
	Owner, Area        string
	Satellite, Speaker string
	RiseURL, LoopURL   string
	FailedText         string // the speaker or the player cannot be reached, or the station does not play
	Station            *Station
	Lights             []string
	LightAt            string // HH:MM
}

// build is the automation of one alarm (docs/spec/alarm.md, The automation).
// At the alarm time one run does everything in order, so that it keeps the
// volume it read at the start to the end: the speaker reachable, else a
// message to the person; the volume to 90 %; the wake sound round by round,
// after each round waiting for the satellite listening (a reaction), the
// speaker idle (the round over) or the cancel event; after a reaction until
// the satellite is idle again; then the volume back. With a station, the
// station instead of the rounds, until it is stopped. With light, a second
// trigger turns the room's lights on before the alarm time.
//
// The alias ends in a tag unique to the alarm: Home Assistant restores a new
// entity's state from the last one with the same entity id.
func build(s spec) map[string]any {
	condition := map[string]any{"condition": "template",
		"value_template": fmt.Sprintf("{{ now().strftime('%%Y-%%m-%%d') == '%s' }}", s.Date)}
	if len(s.Days) > 0 {
		condition = map[string]any{"condition": "time", "weekday": s.Days}
	}
	variables := map[string]any{
		"mira_title": s.Title, "mira_time": s.Time, "mira_days": strings.Join(s.Days, ","), "mira_date": s.Date,
		"mira_owner": s.Owner, "mira_area": s.Area, "mira_kind": kindAlarm,
		"mira_satellite": s.Satellite, "mira_speaker": s.Speaker,
	}
	triggers := []any{map[string]any{"trigger": "time", "at": s.Time + ":00", "id": "ring"}}
	ring := ringActions(s)
	if s.Station != nil {
		variables["mira_station"] = s.Station.Title
		ring = radioActions(s)
	}
	actions := ring
	if len(s.Lights) > 0 {
		variables["mira_light"] = "on"
		triggers = append(triggers, map[string]any{"trigger": "time", "at": s.LightAt + ":00", "id": "light"})
		actions = []any{map[string]any{
			"choose": []any{map[string]any{
				"conditions": "{{ trigger.id == 'light' }}",
				"sequence": []any{map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": s.Lights},
					"data": map[string]any{"brightness_pct": 100, "transition": lightSeconds(s)}}},
			}},
			"default": ring,
		}}
	}
	return map[string]any{
		"alias":       s.Title + " " + s.ID[max(len(s.ID)-8, 0):],
		"description": "Mira: " + s.Title,
		"variables":   variables,
		"triggers":    triggers,
		"conditions":  []any{condition},
		"mode":        "single",
		"actions":     actions,
	}
}

// lightSeconds is how long the light takes to be full: from its trigger to
// the alarm time.
func lightSeconds(s spec) int {
	var h1, m1, h2, m2 int
	fmt.Sscanf(s.LightAt, "%d:%d", &h1, &m1)
	fmt.Sscanf(s.Time, "%d:%d", &h2, &m2)
	return ((h2*60 + m2) - (h1*60 + m1)) * 60
}

// unreachable sends the person the message that the alarm could not ring
// when the entity cannot be reached, and ends the run.
func unreachable(s spec, entity string) map[string]any {
	return map[string]any{
		"if": []any{map[string]any{"condition": "state", "entity_id": entity, "state": []string{"unavailable", "unknown"}}},
		"then": []any{
			message(s),
			map[string]any{"stop": "the speaker cannot be reached"},
		},
	}
}

func message(s spec) map[string]any {
	receiver := s.Owner
	if receiver == "" {
		receiver = "house"
	}
	return map[string]any{"action": "mira.send_message", "data": map[string]any{"text": s.FailedText, "receiver": receiver}}
}

func ringActions(s spec) []any {
	const waited = "wait.trigger is not none and wait.trigger.id"
	speaker := map[string]any{"entity_id": s.Speaker}
	return []any{
		map[string]any{"variables": map[string]any{
			"before": fmt.Sprintf("{{ state_attr('%s', 'volume_level') }}", s.Speaker),
			"until":  fmt.Sprintf("{{ (now() + timedelta(minutes=%d)).isoformat() }}", ringMinutes),
		}},
		unreachable(s, s.Speaker),
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
	}
}

// radioActions wake with a station (decision 10): the player to 90 %, the
// station, until someone stops it; the volume is not set back, as there is
// no end to wait for. A station that does not start playing gets the person
// the message of decision 13.
func radioActions(s spec) []any {
	player := map[string]any{"entity_id": s.Station.Player}
	return []any{
		unreachable(s, s.Station.Player),
		map[string]any{"action": "media_player.volume_set", "target": player, "data": map[string]any{"volume_level": volume}},
		map[string]any{"action": "media_player.play_media", "target": player, "data": map[string]any{
			"media_content_id": s.Station.ContentID, "media_content_type": s.Station.ContentType,
		}},
		map[string]any{
			"wait_template": fmt.Sprintf("{{ is_state('%s', 'playing') }}", s.Station.Player),
			"timeout":       radioStart,
		},
		map[string]any{
			"if":   "{{ not wait.completed }}",
			"then": []any{message(s)},
		},
	}
}
