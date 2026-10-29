package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// dashboardPath is the URL path of the test HA's dashboard (it needs a hyphen).
const dashboardPath = "mira-sim"

// ensureDashboard makes the dashboard "Mira Simulation": one view to switch
// the simulated sensors and presence, one with the house as Mira changes it
// (lights, covers, garage, alarm, calendars, lists). Its config is written
// anew on every call; make another dashboard for changes of your own.
func ensureDashboard(ctx context.Context, admin *haws.Client, extras []extra) error {
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
	// The domains the dashboard's cards show, and the extras: Home Assistant
	// filters, the dashboard asks only for what it may show.
	filter := haws.StateFilter{Domains: []string{"alarm_control_panel", "assist_satellite", "binary_sensor", "button",
		"calendar", "climate", "cover", "input_boolean", "light", "person", "select", "sensor", "switch", "todo"}}
	for _, x := range extras {
		filter.EntityIDs = append(filter.EntityIDs, x.entity)
	}
	states, err := admin.GetStates(ctx, filter)
	if err != nil {
		return err
	}
	have := map[string]haws.State{}
	for _, st := range states {
		have[st.EntityID] = st
	}
	return call(ctx, admin, map[string]any{
		"type": "lovelace/config/save", "url_path": dashboardPath, "config": dashboardConfig(have, extras),
	}, nil)
}

// satelliteCards makes a card for each voice satellite the house has (the
// Show, an Echo): its state, the alarm and timer controls, the assistant and
// the wake word. Only the entities that exist go in.
func satelliteCards(have map[string]haws.State) []any {
	const suffix = "_assist_satellit"
	var ids []string
	for id := range have {
		if strings.HasPrefix(id, "assist_satellite.") && strings.HasSuffix(id, suffix) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	var cards []any
	for _, id := range ids {
		slug := strings.TrimSuffix(strings.TrimPrefix(id, "assist_satellite."), suffix)
		title := strings.TrimSuffix(have[id].FriendlyName(), " Assist-Satellit")
		var pairs []string
		for _, r := range [][2]string{
			{id, "Assistent-Satellit"},
			{"binary_sensor." + slug + "_presence", "Anwesenheit"},
			{"binary_sensor." + slug + "_alarm_sounding", "Wecker klingelt"},
			{"binary_sensor." + slug + "_timer_sounding", "Timer klingelt"},
			{"button." + slug + "_stop_alarm_or_timer", "Wecker oder Timer stoppen"},
			{"button." + slug + "_snooze_alarm", "Schlummern"},
			{"select." + slug + "_assistent", "Assistent"},
			{"select." + slug + "_aktivierungswort", "Aktivierungswort"},
		} {
			if _, ok := have[r[0]]; ok {
				pairs = append(pairs, r[0], r[1])
			}
		}
		cards = append(cards, card(title, rows(pairs...)))
	}
	return cards
}

// extraCard lists what TESTHA_EXPOSE added to the house, if it exists.
func extraCard(have map[string]haws.State, extras []extra) []any {
	var pairs []string
	for _, x := range extras {
		if _, ok := have[x.entity]; ok {
			pairs = append(pairs, x.entity, x.name)
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	return []any{card("Zusätzlich im Haus", rows(pairs...))}
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

func dashboardConfig(have map[string]haws.State, extras []extra) map[string]any {
	sim := []any{
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
	}
	sim = append(sim, satelliteCards(have)...)
	house := []any{
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
	}
	house = append(house, extraCard(have, extras)...)
	house = append(house,
		map[string]any{"type": "todo-list", "entity": "todo.einkauf", "title": "Einkauf"},
		map[string]any{"type": "todo-list", "entity": "todo.einkaufsliste", "title": "Einkaufsliste"},
		map[string]any{"type": "calendar", "title": "Kalender", "initial_view": "listWeek",
			"entities": []any{"calendar.familie", "calendar.ben"}},
	)
	return map[string]any{
		"title": "Mira Simulation",
		"views": []any{
			map[string]any{"title": "Simulation", "path": "sim", "icon": "mdi:flask", "cards": sim},
			map[string]any{"title": "Haus", "path": "haus", "icon": "mdi:home", "cards": house},
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
	if err := ensureDashboard(ctx, admin, parseExtras(e["TESTHA_EXPOSE"])); err != nil {
		return err
	}
	fmt.Printf("dashboard ready: %s/%s\n", haURL, dashboardPath)
	return nil
}
