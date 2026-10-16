package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

// The Mira integration in Home Assistant can take over the persons and the
// rooms: each is a subentry there, edited with HA's own
// entity and area pickers. It reads them once to import what exists, then
// writes all of them on every change. While it manages them, the web UI
// shows those sections read-only, so there is only one place to edit them.

// kvHAManaged marks the sections as managed by Home Assistant.
const kvHAManaged = "ui_managed_by_ha"

// haSections are the sections the integration replaces as a whole.
// notifier is only touched in its morning_trigger.
var haSections = []string{"persons", "satellites"}

// Room is one area as the integration sees it: its satellite with Mira's
// settings on it (docs/spec/ha-anbindung.md): the motion sensors and whether
// motion there starts the morning announcement. The agent stores it as
// satellites plus notifier.morning_trigger.
type Room struct {
	Area      string   `json:"area"`
	Satellite string   `json:"satellite,omitempty"`
	Presence  []string `json:"presence,omitempty"`
	Morning   bool     `json:"morning"`
	// Mode is "announce" (default) or "indicate"; Indicator is the light
	// that shows waiting notices.
	Mode      string `json:"mode,omitempty"`
	Indicator string `json:"indicator,omitempty"`
}

type haConfig struct {
	Persons json.RawMessage `json:"persons"`
	Rooms   []Room          `json:"rooms"`
	Managed bool            `json:"managed"`
}

type morningTrigger struct {
	Areas    []string              `json:"areas"`
	Presence map[string]entityList `json:"presence"`
}

// entityList reads one entity id or a list, as the YAML allows both.
type entityList []string

func (e *entityList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*e = entityList{one}
		if one == "" {
			*e = nil
		}
		return nil
	}
	var list []string
	err := json.Unmarshal(b, &list)
	*e = list
	return err
}

func (h *Handler) managedByHA(r *http.Request) bool {
	v, err := h.opts.Store.GetKV(r.Context(), kvHAManaged)
	return err == nil && v == "1"
}

func (h *Handler) getHA(w http.ResponseWriter, r *http.Request) {
	out := haConfig{Managed: h.managedByHA(r)}
	var err error
	out.Persons, _, err = h.opts.Config.Section("persons")
	if err == nil {
		out.Rooms, err = h.rooms()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// rooms joins satellites and the morning trigger into one list by area.
func (h *Handler) rooms() ([]Room, error) {
	var sats map[string]string
	var notifier struct {
		MorningTrigger morningTrigger    `json:"morning_trigger"`
		RoomMode       map[string]string `json:"room_mode"`
		Indicators     map[string]string `json:"indicators"`
	}
	raw, _, err := h.opts.Config.Section("satellites")
	if err == nil {
		err = json.Unmarshal(raw, &sats)
	}
	if err == nil {
		raw, _, err = h.opts.Config.Section("notifier")
	}
	if err == nil {
		err = json.Unmarshal(raw, &notifier)
	}
	if err != nil {
		return nil, err
	}
	byArea := map[string]*Room{}
	room := func(area string) *Room {
		if byArea[area] == nil {
			byArea[area] = &Room{Area: area}
		}
		return byArea[area]
	}
	for area, sat := range sats {
		room(area).Satellite = sat
	}
	for _, area := range notifier.MorningTrigger.Areas {
		room(area).Morning = true
	}
	for area, sensors := range notifier.MorningTrigger.Presence {
		room(area).Presence = sensors
	}
	for area, mode := range notifier.RoomMode {
		room(area).Mode = mode
	}
	for area, light := range notifier.Indicators {
		room(area).Indicator = light
	}
	out := make([]Room, 0, len(byArea))
	for _, r := range byArea {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Area < out[j].Area })
	return out, nil
}

func (h *Handler) putHA(w http.ResponseWriter, r *http.Request) {
	var in haConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {persons, rooms}: "+err.Error())
		return
	}
	sections, err := h.haSections(in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.opts.Config.SetSections(r.Context(), sections); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.opts.Store.SetKV(r.Context(), kvHAManaged, "1"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.log.Info("config changed by Home Assistant", "rooms", len(in.Rooms))
	h.getHA(w, r)
}

// haSections turns what the integration sends into config sections.
func (h *Handler) haSections(in haConfig) (map[string]json.RawMessage, error) {
	if len(in.Persons) == 0 || string(in.Persons) == "null" {
		in.Persons = json.RawMessage("{}")
	}
	sats := map[string]string{}
	mt := morningTrigger{Areas: []string{}, Presence: map[string]entityList{}}
	modes, indicators := map[string]string{}, map[string]string{}
	seen := map[string]bool{}
	for _, room := range in.Rooms {
		if room.Area == "" {
			return nil, fmt.Errorf("a room has no area")
		}
		if seen[room.Area] {
			return nil, fmt.Errorf("area %s is configured twice", room.Area)
		}
		seen[room.Area] = true
		if room.Satellite != "" {
			sats[room.Area] = room.Satellite
		}
		if len(room.Presence) > 0 {
			mt.Presence[room.Area] = room.Presence
		}
		if room.Morning {
			mt.Areas = append(mt.Areas, room.Area)
		}
		if room.Mode != "" {
			modes[room.Area] = room.Mode
		}
		if room.Indicator != "" {
			indicators[room.Area] = room.Indicator
		}
	}
	// The notifier keeps everything else; only its morning trigger comes
	// from the rooms.
	raw, _, err := h.opts.Config.Section("notifier")
	if err != nil {
		return nil, err
	}
	var notifier map[string]any
	if err := json.Unmarshal(raw, &notifier); err != nil {
		return nil, err
	}
	notifier["morning_trigger"] = mt
	notifier["room_mode"] = modes
	notifier["indicators"] = indicators
	notifierJSON, err := json.Marshal(notifier)
	if err != nil {
		return nil, err
	}
	satsJSON, _ := json.Marshal(sats)
	return map[string]json.RawMessage{
		"persons": in.Persons, "satellites": satsJSON, "notifier": notifierJSON,
	}, nil
}

// deleteHA hands the sections back to the web UI. Their values stay.
func (h *Handler) deleteHA(w http.ResponseWriter, r *http.Request) {
	if err := h.opts.Store.DeleteKV(r.Context(), kvHAManaged); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.log.Info("Home Assistant no longer manages persons and rooms")
	writeJSON(w, http.StatusOK, map[string]bool{"managed": false})
}
