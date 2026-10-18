package main

import (
	"context"
	"fmt"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// dashboardPath is the URL path of the test HA's dashboard (it needs a hyphen).
const dashboardPath = "mira-sim"

// ensureDashboard makes the dashboard "Mira Simulation": one view to switch
// the simulated sensors and presence, one with the house as Mira changes it
// (lights, covers, garage, alarm, calendars, lists). Its config is written
// anew on every call; make another dashboard for changes of your own.
func ensureDashboard(ctx context.Context, admin *haws.Client) error {
	var existing []struct {
		URLPath string `json:"url_path"`
	}
	if err := call(ctx, admin, map[string]any{"type": "lovelace/dashboards/list"}, &existing); err != nil {
		return err
	}
	found := false
	for _, d := range existing {
		found = found || d.URLPath == dashboardPath
	}
	if !found {
		if err := call(ctx, admin, map[string]any{
			"type": "lovelace/dashboards/create", "url_path": dashboardPath, "mode": "storage",
			"title": "Mira Simulation", "icon": "mdi:flask", "show_in_sidebar": true, "require_admin": false,
		}, nil); err != nil {
			return err
		}
	}
	return call(ctx, admin, map[string]any{
		"type": "lovelace/config/save", "url_path": dashboardPath, "config": dashboardConfig(),
	}, nil)
}

// rows turns pairs of entity and name into the rows of an entities card.
func rows(pairs ...string) []any {
	var out []any
	for i := 0; i+1 < len(pairs); i += 2 {
		row := map[string]any{"entity": pairs[i]}
		if pairs[i+1] != "" {
			row["name"] = pairs[i+1]
		}
		out = append(out, row)
	}
	return out
}

func card(title string, entities []any) map[string]any {
	return map[string]any{"type": "entities", "title": title, "show_header_toggle": false, "entities": entities}
}

func dashboardConfig() map[string]any {
	return map[string]any{
		"title": "Mira Simulation",
		"views": []any{
			map[string]any{
				"title": "Simulation", "path": "sim", "icon": "mdi:flask",
				"cards": []any{
					card("Sensoren auslösen (Schalter)", rows(
						"input_boolean.sim_leck_keller", "Leck Keller",
						"input_boolean.sim_leck_kueche", "Leck Spülmaschine",
						"input_boolean.sim_bewegung_flur", "Bewegung Flur",
						"input_boolean.sim_bewegung_bad", "Bewegung Bad",
						"input_boolean.sim_fenster_bad", "Fenster Bad offen",
					)),
					card("Was Mira sieht (Sensoren)", rows(
						"binary_sensor.lecksensor_keller", "Lecksensor Keller",
						"binary_sensor.lecksensor_spulmaschine", "Lecksensor Spülmaschine",
						"binary_sensor.bewegung_flur", "Bewegung Flur",
						"binary_sensor.bewegung_bad", "Bewegung Bad",
						"binary_sensor.fenster_bad", "Fenster Bad",
						"sensor.power_consumption", "Stromverbrauch",
					)),
					card("Wer ist zu Hause (Schalter)", rows(
						"input_boolean.sim_da_admin", "Admin zu Hause",
						"input_boolean.sim_da_anna", "Anna zu Hause",
						"input_boolean.sim_da_ben", "Ben zu Hause",
						"person.admin", "",
						"person.anna", "",
						"person.ben", "",
					)),
					card("Mira", rows(
						"switch.mira_ruhemodus", "Ruhemodus",
						"sensor.mira_wartende_nachrichten", "Wartende Nachrichten",
						"button.mira_profil_verdichten", "Gedächtnis verdichten",
					)),
					card("Show", rows(
						"assist_satellite.show_assist_satellit", "Assistent-Satellit",
						"binary_sensor.show_presence", "Anwesenheit",
						"binary_sensor.show_alarm_sounding", "Wecker klingelt",
						"binary_sensor.show_timer_sounding", "Timer klingelt",
						"button.show_stop_alarm_or_timer", "Wecker oder Timer stoppen",
						"button.show_snooze_alarm", "Schlummern",
						"select.show_assistent", "Assistent",
						"select.show_aktivierungswort", "Aktivierungswort",
					)),
				},
			},
			map[string]any{
				"title": "Haus", "path": "haus", "icon": "mdi:home",
				"cards": []any{
					card("Licht und Schalter", rows(
						"light.ceiling_lights", "Deckenlicht",
						"light.living_room_rgbww_lights", "Stehlampe",
						"light.kitchen_lights", "Küchenlicht",
						"light.entrance_color_white_lights", "Flurlicht",
						"light.bed_light", "Nachttischlampe",
						"light.office_rgbw_lights", "Schreibtischlampe",
						"switch.decorative_lights", "Lichterkette",
						"switch.ac", "Klimaanlage",
					)),
					card("Rollläden, Tor, Alarm, Heizung", rows(
						"cover.living_room_window", "Rollladen Wohnzimmer",
						"cover.kitchen_window", "Rollladen Küche",
						"cover.garage_door", "Garagentor",
						"alarm_control_panel.security", "Alarmanlage",
						"climate.heatpump", "Heizung Wohnzimmer",
						"sensor.outside_temperature", "Außentemperatur",
					)),
					map[string]any{"type": "todo-list", "entity": "todo.einkauf", "title": "Einkauf"},
					map[string]any{"type": "todo-list", "entity": "todo.einkaufsliste", "title": "Einkaufsliste"},
					map[string]any{"type": "calendar", "title": "Kalender", "initial_view": "listWeek",
						"entities": []any{"calendar.familie", "calendar.ben"}},
				},
			},
		},
	}
}

func dashboardCmd(ctx context.Context, dir string) error {
	e := loadEnv(dir)
	admin, stop, err := connect(ctx, e["TESTHA_ADMIN_TOKEN"])
	if err != nil {
		return fmt.Errorf("no admin token; run testha up first: %w", err)
	}
	defer stop()
	if err := ensureDashboard(ctx, admin); err != nil {
		return err
	}
	fmt.Printf("dashboard ready: %s/%s\n", haURL, dashboardPath)
	return nil
}
