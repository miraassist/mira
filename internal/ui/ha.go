package ui

import (
	"encoding/json"
	"fmt"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/excerpt"
	"net/http"
	"slices"
	"sort"
)

// The Mira integration in Home Assistant can take over the persons and the
// rooms: each is a subentry there, edited with HA's own
// entity and area pickers. It reads them once to import what exists, then
// writes all of them on every change. While it manages them, the web UI
// shows those sections read-only, so there is only one place to edit them.

// haSections are the sections the integration replaces as a whole.
// notifier is only touched in its morning_trigger.
var haSections = []string{"persons", "satellites"}

// Room is one area as the integration sees it: its satellite with Mira's
// settings on it (docs/spec/ha-integration.md): the motion sensors and whether
// motion there starts the morning announcement. The service stores it as
// satellites plus notifier.morning_trigger.
type Room struct {
	Area      string   `json:"area"`
	Satellite string   `json:"satellite,omitempty"`
	Presence  []string `json:"presence,omitempty"`
	Morning   bool     `json:"morning"`
	// Mode is "announce" (default) or "indicate"; Indicator is the light
	// that shows waiting messages.
	Mode      string `json:"mode,omitempty"`
	Indicator string `json:"indicator,omitempty"`
	// Others are the room's satellites besides Satellite, its primary, each
	// with its own settings; nil from an older integration
	// (docs/spec/satellites-rooms.md).
	Others []RoomSatellite `json:"others,omitempty"`
}

// RoomSatellite is one more satellite of a room.
type RoomSatellite struct {
	Satellite string   `json:"satellite"`
	Presence  []string `json:"presence,omitempty"`
	Morning   bool     `json:"morning"`
	Indicator string   `json:"indicator,omitempty"`
}

type haConfig struct {
	Persons json.RawMessage `json:"persons"`
	Rooms   []Room          `json:"rooms"`
	Managed bool            `json:"managed"`
	// Exposed is what Home Assistant exposes to Assist; nil from an older
	// integration.
	Exposed []string `json:"exposed,omitempty"`
	// Logins are the login names of the persons' Home Assistant users, by
	// person entity; only an admin may read them, so the integration sends
	// them. nil from an older integration.
	Logins map[string]string `json:"logins,omitempty"`
}

type morningTrigger struct {
	Areas    []string              `json:"areas"`
	Presence map[string]entityList `json:"presence"`
	SpeakAt  map[string]string     `json:"speak_at,omitempty"`
}

// entityList reads one entity id or a list, as the YAML allows both, and
// writes one as itself, so a room with one satellite reads as before.
type entityList []string

func (e entityList) MarshalJSON() ([]byte, error) {
	if len(e) == 1 {
		return json.Marshal(e[0])
	}
	return json.Marshal([]string(e))
}

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

func (h *Handler) getHA(w http.ResponseWriter, r *http.Request) {
	out := haConfig{Managed: h.opts.Config.HAManaged()}
	var err error
	out.Persons, err = h.opts.Config.Section("persons")
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
	var sats map[string]entityList
	var notifier struct {
		MorningTrigger morningTrigger        `json:"morning_trigger"`
		RoomMode       map[string]string     `json:"room_mode"`
		Indicators     map[string]entityList `json:"indicators"`
	}
	raw, err := h.opts.Config.Section("satellites")
	if err == nil {
		err = json.Unmarshal(raw, &sats)
	}
	if err == nil {
		raw, err = h.opts.Config.Section("notifier")
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
	speakAt := notifier.MorningTrigger.SpeakAt
	for area, list := range sats {
		r := room(area)
		for i, sat := range list {
			if i == 0 {
				r.Satellite = sat
				continue
			}
			other := RoomSatellite{Satellite: sat}
			for _, sensor := range notifier.MorningTrigger.Presence[area] {
				if speakAt[sensor] == sat {
					other.Presence = append(other.Presence, sensor)
				}
			}
			r.Others = append(r.Others, other)
		}
	}
	for _, area := range notifier.MorningTrigger.Areas {
		room(area).Morning = true
	}
	for area, sensors := range notifier.MorningTrigger.Presence {
		// The primary's are those that speak at no other satellite.
		r := room(area)
		for _, sensor := range sensors {
			if speakAt[sensor] == "" {
				r.Presence = append(r.Presence, sensor)
			}
		}
	}
	for area, mode := range notifier.RoomMode {
		room(area).Mode = mode
	}
	for area, lights := range notifier.Indicators {
		if len(lights) > 0 {
			room(area).Indicator = lights[0]
		}
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
	sections, unknown, err := h.haSections(in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(unknown) > 0 {
		// A newer integration than this Mira: what she does not know waits
		// for her update (docs/spec/versions.md).
		h.log.Info("fields from Home Assistant this Mira does not know, left out", "fields", unknown)
	}
	if err := h.opts.Config.SetHASections(r.Context(), sections); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if in.Exposed != nil && h.opts.SetExposed != nil {
		if err := h.opts.SetExposed(r.Context(), in.Exposed); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if in.Logins != nil && h.opts.SetLogins != nil {
		if err := h.opts.SetLogins(r.Context(), in.Logins); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	h.log.Info("config changed by Home Assistant", "rooms", len(in.Rooms), "exposed", len(in.Exposed))
	h.getHA(w, r)
}

// haSections turns what the integration sends into config sections.
func (h *Handler) haSections(in haConfig) (map[string]json.RawMessage, []string, error) {
	if len(in.Persons) == 0 || string(in.Persons) == "null" {
		in.Persons = json.RawMessage("{}")
	}
	persons, unknown, err := knownPersonFields(in.Persons)
	if err != nil {
		return nil, nil, err
	}
	sats := map[string]entityList{}
	mt := morningTrigger{Areas: []string{}, Presence: map[string]entityList{}, SpeakAt: map[string]string{}}
	modes, indicators := map[string]string{}, map[string]entityList{}
	seen := map[string]bool{}
	for _, room := range in.Rooms {
		if room.Area == "" {
			return nil, nil, fmt.Errorf("a room has no area")
		}
		if seen[room.Area] {
			return nil, nil, fmt.Errorf("area %s is configured twice", room.Area)
		}
		seen[room.Area] = true
		if room.Satellite != "" {
			sats[room.Area] = entityList{room.Satellite}
		}
		presence := append([]string{}, room.Presence...)
		morning := room.Morning
		lights := entityList{}
		if room.Indicator != "" {
			lights = append(lights, room.Indicator)
		}
		// The others of the room: their sensors speak at them, unless the
		// primary or another one has the sensor too; their lights show the
		// room's state (docs/spec/satellites-rooms.md).
		owners := map[string][]string{}
		for _, o := range room.Others {
			if o.Satellite == "" || room.Satellite == "" {
				continue
			}
			sats[room.Area] = append(sats[room.Area], o.Satellite)
			for _, sensor := range o.Presence {
				owners[sensor] = append(owners[sensor], o.Satellite)
				if !slices.Contains(presence, sensor) {
					presence = append(presence, sensor)
				}
			}
			morning = morning || o.Morning
			if o.Indicator != "" && !slices.Contains(lights, o.Indicator) {
				lights = append(lights, o.Indicator)
			}
		}
		for sensor, at := range owners {
			if len(at) == 1 && !slices.Contains(room.Presence, sensor) {
				mt.SpeakAt[sensor] = at[0]
			}
		}
		if len(presence) > 0 {
			mt.Presence[room.Area] = presence
		}
		if morning {
			mt.Areas = append(mt.Areas, room.Area)
		}
		if room.Mode != "" {
			modes[room.Area] = room.Mode
		}
		if len(lights) > 0 {
			indicators[room.Area] = lights
		}
	}
	// The notifier keeps everything else; only its morning trigger comes
	// from the rooms.
	raw, err := h.opts.Config.Section("notifier")
	if err != nil {
		return nil, nil, err
	}
	var notifier map[string]any
	if err := json.Unmarshal(raw, &notifier); err != nil {
		return nil, nil, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	notifier["morning_trigger"] = mt
	notifier["room_mode"] = modes
	notifier["indicators"] = indicators
	notifierJSON, err := json.Marshal(notifier)
	if err != nil {
		return nil, nil, err
	}
	satsJSON, _ := json.Marshal(sats)
	return map[string]json.RawMessage{
		"persons": persons, "satellites": satsJSON, "notifier": notifierJSON,
	}, unknown, nil
}

// knownPersonFields keeps of each person what mira.yaml has a key for, and
// says which other fields it left out. Persons become part of mira.yaml,
// which is read strictly; a field a newer integration sends must not stop
// this Mira (docs/spec/versions.md).
func knownPersonFields(raw json.RawMessage) (json.RawMessage, []string, error) {
	var persons map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &persons); err != nil {
		return nil, nil, fmt.Errorf("persons %q: %w", excerpt.Bytes(raw), err)
	}
	known, seen := config.PersonFields(), map[string]bool{}
	for _, p := range persons {
		for k := range p {
			if !known[k] {
				delete(p, k)
				seen[k] = true
			}
		}
	}
	out, err := json.Marshal(persons)
	if err != nil {
		return nil, nil, err
	}
	unknown := make([]string, 0, len(seen))
	for k := range seen {
		unknown = append(unknown, k)
	}
	sort.Strings(unknown)
	return out, unknown, nil
}

// deleteHA hands the sections back to the web UI. Their values hold until
// Mira restarts.
func (h *Handler) deleteHA(w http.ResponseWriter, r *http.Request) {
	h.opts.Config.ReleaseHA()
	h.log.Info("Home Assistant no longer manages persons and rooms")
	writeJSON(w, http.StatusOK, map[string]bool{"managed": false})
}
