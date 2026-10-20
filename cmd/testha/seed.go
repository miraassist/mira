package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// simulated maps each simulated sensor to the input_boolean that drives it
// (see testenv/ha/configuration.yaml).
var simulated = map[string]string{
	"binary_sensor.lecksensor_keller":       "input_boolean.sim_leck_keller",
	"binary_sensor.lecksensor_spulmaschine": "input_boolean.sim_leck_kueche",
	"binary_sensor.bewegung_flur":           "input_boolean.sim_bewegung_flur",
	"binary_sensor.bewegung_bad":            "input_boolean.sim_bewegung_bad",
	"binary_sensor.fenster_bad":             "input_boolean.sim_fenster_bad",
}

var areas = []string{"Wohnzimmer", "Küche", "Bad", "Flur", "Keller", "Schlafzimmer", "Büro", "Garage"}

// house is the test house: entity -> area and German name. Exactly these
// entities are exposed to Assist; everything else is hidden from it.
var house = map[string]struct{ area, name string }{
	"light.ceiling_lights":                  {"Wohnzimmer", "Deckenlicht"},
	"light.living_room_rgbww_lights":        {"Wohnzimmer", "Stehlampe"},
	"switch.decorative_lights":              {"Wohnzimmer", "Lichterkette"},
	"cover.living_room_window":              {"Wohnzimmer", "Rollladen Wohnzimmer"},
	"climate.heatpump":                      {"Wohnzimmer", "Heizung Wohnzimmer"},
	"light.kitchen_lights":                  {"Küche", "Küchenlicht"},
	"cover.kitchen_window":                  {"Küche", "Rollladen Küche"},
	"binary_sensor.lecksensor_spulmaschine": {"Küche", ""},
	"binary_sensor.bewegung_bad":            {"Bad", ""},
	"binary_sensor.fenster_bad":             {"Bad", ""},
	"light.entrance_color_white_lights":     {"Flur", "Flurlicht"},
	"binary_sensor.bewegung_flur":           {"Flur", ""},
	"alarm_control_panel.security":          {"Flur", "Alarmanlage"},
	"binary_sensor.lecksensor_keller":       {"Keller", ""},
	"sensor.power_consumption":              {"Keller", "Stromverbrauch"},
	"light.bed_light":                       {"Schlafzimmer", "Nachttischlampe"},
	"switch.ac":                             {"Schlafzimmer", "Klimaanlage"},
	"light.office_rgbw_lights":              {"Büro", "Schreibtischlampe"},
	"cover.garage_door":                     {"Garage", "Garagentor"},
	"sensor.outside_temperature":            {"", "Außentemperatur"},
	"sensor.outside_humidity":               {"", "Luftfeuchtigkeit außen"},
	"weather.forecast_mira_test":            {"", "Wetter"},
	"todo.einkaufsliste":                    {"", ""},
	"calendar.familie":                      {"", ""},
	"calendar.ben":                          {"", ""},
	"calendar.abfuhrtermine":                {"", ""},
	"todo.einkauf":                          {"", ""},
	// The fake TECHO5 devices (testenv/techo5_fake): their speakers, like
	// the real ones, are exposed so Mira can play on them.
	"media_player.fake_show_speaker": {"Schlafzimmer", "Fake Show Lautsprecher"},
	"media_player.fake_echo_speaker": {"Bad", "Fake Echo Lautsprecher"},
}

// fakeDevices puts the fake TECHO5 devices into their rooms, next to the real
// ones, so tests run without hardware while the real Show stays for voice.
var fakeDevices = map[string]string{"Fake Show": "Schlafzimmer", "Fake Echo": "Bad"}

var persons = []string{"Anna", "Ben"}

// discoverySources are config entry sources from network discovery. In host
// network mode the test HA sees the real house; such entries are disabled so
// the test HA never controls a real device.
var discoverySources = map[string]bool{
	"zeroconf": true, "ssdp": true, "dhcp": true, "bluetooth": true, "homekit": true,
	"usb": true, "mqtt": true, "integration_discovery": true, "discovery": true,
}

// extra is an entity added to the test house on top of `house` (a lamp, say):
// exposed to Assist, with the area and name given. It comes from TESTHA_EXPOSE
// in testenv/.env (not in git, since it names what is in someone's network):
// "light.lampe|Wohnzimmer|Farblampe;switch.x|Büro". Area and name are optional.
type extra struct{ entity, area, name string }

func parseExtras(s string) []extra {
	var out []extra
	for _, item := range strings.Split(s, ";") {
		parts := strings.Split(item, "|")
		id := strings.TrimSpace(parts[0])
		if id == "" {
			continue
		}
		x := extra{entity: id}
		if len(parts) > 1 {
			x.area = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			x.name = strings.TrimSpace(parts[2])
		}
		out = append(out, x)
	}
	return out
}

func seed(ctx context.Context, admin *haws.Client, token string, extras []extra) error {
	if err := disableDiscovered(ctx, admin); err != nil {
		return err
	}
	areaIDs, err := ensureAreas(ctx, admin)
	if err != nil {
		return err
	}
	if err := ensureLists(ctx, admin, token); err != nil {
		return err
	}
	ents, err := admin.Entities(ctx)
	if err != nil {
		return err
	}
	var expose, hide []string
	extraOf := map[string]extra{}
	for _, x := range extras {
		extraOf[x.entity] = x
	}
	inHouse := 0
	for _, e := range ents {
		want, ok := house[e.EntityID]
		if ok {
			inHouse++
		} else if x, isExtra := extraOf[e.EntityID]; isExtra {
			want, ok = struct{ area, name string }{x.area, x.name}, true
		}
		if !ok {
			hide = append(hide, e.EntityID)
			continue
		}
		expose = append(expose, e.EntityID)
		update := map[string]any{"type": "config/entity_registry/update", "entity_id": e.EntityID}
		if id := areaIDs[want.area]; want.area != "" && id != "" {
			update["area_id"] = id
		}
		if want.name != "" {
			update["name"] = want.name
		}
		if len(update) > 2 {
			if err := call(ctx, admin, update, nil); err != nil {
				return fmt.Errorf("%s: %w", e.EntityID, err)
			}
		}
	}
	if inHouse != len(house) {
		return fmt.Errorf("only %d of %d house entities exist; is the demo integration loaded?", inHouse, len(house))
	}
	for _, x := range extras {
		found := false
		for _, id := range expose {
			found = found || id == x.entity
		}
		if !found {
			fmt.Printf("TESTHA_EXPOSE: %s does not exist (yet); skipped\n", x.entity)
		}
	}
	for should, ids := range map[bool][]string{true: expose, false: hide} {
		if err := call(ctx, admin, map[string]any{
			"type": "homeassistant/expose_entity", "assistants": []string{"conversation"},
			"entity_ids": ids, "should_expose": should,
		}, nil); err != nil {
			return err
		}
	}
	if err := placeFakes(ctx, admin, areaIDs); err != nil {
		return err
	}
	if err := ensurePersons(ctx, admin); err != nil {
		return err
	}
	// The garage door is switched only after a yes (label "Mira:
	// nachfragen"; the integration creates the label).
	if err := call(ctx, admin, map[string]any{"type": "config/entity_registry/update",
		"entity_id": "cover.garage_door", "labels": []string{"mira_confirm"}}, nil); err != nil {
		return err
	}
	if err := ensureDashboard(ctx, admin, extras); err != nil {
		return fmt.Errorf("dashboard: %w", err)
	}
	return ensureMCPServer(ctx, token)
}

func disableDiscovered(ctx context.Context, admin *haws.Client) error {
	var entries []struct {
		EntryID    string  `json:"entry_id"`
		Domain     string  `json:"domain"`
		Source     string  `json:"source"`
		DisabledBy *string `json:"disabled_by"`
	}
	if err := call(ctx, admin, map[string]any{"type": "config_entries/get"}, &entries); err != nil {
		return err
	}
	for _, e := range entries {
		if !discoverySources[e.Source] || e.DisabledBy != nil {
			continue
		}
		if err := call(ctx, admin, map[string]any{
			"type": "config_entries/disable", "entry_id": e.EntryID, "disabled_by": "user",
		}, nil); err != nil {
			return err
		}
		fmt.Printf("disabled discovered integration %s (from %s)\n", e.Domain, e.Source)
	}
	return nil
}

func ensureAreas(ctx context.Context, admin *haws.Client) (map[string]string, error) {
	existing, err := admin.Areas(ctx)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, a := range existing {
		ids[a.Name] = a.AreaID
	}
	for _, name := range areas {
		if ids[name] != "" {
			continue
		}
		var created struct {
			AreaID string `json:"area_id"`
		}
		if err := call(ctx, admin, map[string]any{"type": "config/area_registry/create", "name": name}, &created); err != nil {
			return nil, err
		}
		ids[name] = created.AreaID
	}
	return ids, nil
}

func ensurePersons(ctx context.Context, admin *haws.Client) error {
	var list struct {
		Storage []struct {
			Name string `json:"name"`
		} `json:"storage"`
	}
	if err := call(ctx, admin, map[string]any{"type": "person/list"}, &list); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, p := range list.Storage {
		have[p.Name] = true
	}
	for _, name := range persons {
		if have[name] {
			continue
		}
		if err := call(ctx, admin, map[string]any{"type": "person/create", "name": name}, nil); err != nil {
			return err
		}
	}
	return linkSimTrackers(ctx, admin)
}

// linkSimTrackers gives every person the test tracker device_tracker.sim_<id>,
// which "Sim zu Hause <Person>" switches (configuration.yaml), so presence
// and the "alone at home" rule can be tried out.
func linkSimTrackers(ctx context.Context, admin *haws.Client) error {
	var list struct {
		Storage []struct {
			ID             string   `json:"id"`
			Name           string   `json:"name"`
			UserID         *string  `json:"user_id"`
			DeviceTrackers []string `json:"device_trackers"`
			Picture        *string  `json:"picture"`
		} `json:"storage"`
	}
	if err := call(ctx, admin, map[string]any{"type": "person/list"}, &list); err != nil {
		return err
	}
	for _, p := range list.Storage {
		tracker := "device_tracker.sim_" + p.ID
		if slices.Contains(p.DeviceTrackers, tracker) {
			continue
		}
		update := map[string]any{"type": "person/update", "person_id": p.ID, "name": p.Name,
			"user_id": p.UserID, "picture": p.Picture, "device_trackers": append(p.DeviceTrackers, tracker)}
		if err := call(ctx, admin, update, nil); err != nil {
			return err
		}
	}
	return nil
}

// ensureMCPServer sets up HA's MCP server with the Assist API, the endpoint
// Mira uses as a non-admin user (/api/mcp/assist).
func ensureMCPServer(ctx context.Context, token string) error {
	var entries []struct {
		Domain string `json:"domain"`
	}
	if err := api(ctx, "GET", "/api/config/config_entries/entry", token, nil, &entries); err != nil {
		return err
	}
	for _, e := range entries {
		if e.Domain == "mcp_server" {
			return nil
		}
	}
	var flow struct {
		FlowID string `json:"flow_id"`
		Type   string `json:"type"`
	}
	if err := api(ctx, "POST", "/api/config/config_entries/flow", token, map[string]any{"handler": "mcp_server"}, &flow); err != nil {
		return err
	}
	if err := api(ctx, "POST", "/api/config/config_entries/flow/"+flow.FlowID, token, map[string]any{"llm_hass_api": []string{"assist"}}, &flow); err != nil {
		return err
	}
	if flow.Type != "create_entry" {
		return fmt.Errorf("mcp_server setup ended with %q", flow.Type)
	}
	return nil
}

// placeFakes gives the fake TECHO5 devices their areas. Missing fakes (the
// integration not loaded) are reported, not an error.
func placeFakes(ctx context.Context, admin *haws.Client, areaIDs map[string]string) error {
	devices, err := admin.Devices(ctx)
	if err != nil {
		return err
	}
	for name, area := range fakeDevices {
		found := false
		for _, d := range devices {
			if d.Name != name {
				continue
			}
			found = true
			if d.AreaID == areaIDs[area] {
				continue
			}
			if err := call(ctx, admin, map[string]any{"type": "config/device_registry/update", "device_id": d.ID, "area_id": areaIDs[area]}, nil); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		if !found {
			fmt.Printf("fake device %q missing; is techo5_fake loaded?\n", name)
		}
	}
	return nil
}
