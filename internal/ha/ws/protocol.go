// Package ws is the agent's direct WebSocket connection to Home Assistant.
//
// It is used for everything the MCP server does not cover: reading the area,
// device and entity registries, subscribing to events and entity states, and
// calling services such as assist_satellite.start_conversation. Tool calls on
// behalf of the model never go through here — those are MCP.
package ws

import "encoding/json"

// Message types of the Home Assistant WebSocket API.
const (
	typeAuthRequired = "auth_required"
	typeAuth         = "auth"
	typeAuthOK       = "auth_ok"
	typeAuthInvalid  = "auth_invalid"
	typeResult       = "result"
	typeEvent        = "event"
	typePong         = "pong"
)

// Command names used by the agent.
const (
	CmdAreaRegistryList   = "config/area_registry/list"
	CmdDeviceRegistryList = "config/device_registry/list"
	CmdEntityRegistryList = "config/entity_registry/list"
	CmdCallService        = "call_service"
	CmdSubscribeEvents    = "subscribe_events"
	CmdSubscribeEntities  = "subscribe_entities"
	CmdUnsubscribeEvents  = "unsubscribe_events"
	CmdPing               = "ping"
)

// incoming is one frame from Home Assistant. The fields are a union over the
// message types; Type says which of them are meaningful.
type incoming struct {
	ID      int             `json:"id"`
	Type    string          `json:"type"`
	Success *bool           `json:"success,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *wsError        `json:"error,omitempty"`
	Event   json.RawMessage `json:"event,omitempty"`
	Version string          `json:"ha_version,omitempty"`
}

type wsError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Area is one row of the area registry.
type Area struct {
	AreaID  string   `json:"area_id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	FloorID string   `json:"floor_id"`
	Icon    string   `json:"icon"`
	Labels  []string `json:"labels"`
}

// Device is one row of the device registry.
type Device struct {
	ID           string `json:"id"`
	AreaID       string `json:"area_id"`
	Name         string `json:"name"`
	NameByUser   string `json:"name_by_user"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	DisabledBy   string `json:"disabled_by"`
	// ConfigurationURL is where the device's own web interface is.
	ConfigurationURL string `json:"configuration_url"`
}

// DisplayName is the user's name for the device if they set one.
func (d Device) DisplayName() string {
	if d.NameByUser != "" {
		return d.NameByUser
	}
	return d.Name
}

// Entity is one row of the entity registry.
type Entity struct {
	EntityID     string         `json:"entity_id"`
	AreaID       string         `json:"area_id"`
	DeviceID     string         `json:"device_id"`
	Name         string         `json:"name"`
	OriginalName string         `json:"original_name"`
	Platform     string         `json:"platform"`
	UniqueID     string         `json:"unique_id"`
	Aliases      []string       `json:"aliases"`
	Labels       []string       `json:"labels"`
	DisabledBy   string         `json:"disabled_by"`
	HiddenBy     string         `json:"hidden_by"`
	EntityCat    string         `json:"entity_category"`
	Options      map[string]any `json:"options"`
}

// DisplayName prefers the user's override over the integration's name.
func (e Entity) DisplayName() string {
	if e.Name != "" {
		return e.Name
	}
	return e.OriginalName
}

// Usable reports whether the entity is something the agent should ever see:
// disabled, hidden and diagnostic entities are noise in the structure block.
func (e Entity) Usable() bool {
	return e.DisabledBy == "" && e.HiddenBy == "" && e.EntityCat == ""
}

// State is one entry of get_states.
type State struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes"`
	LastChanged string         `json:"last_changed"`
}

// FriendlyName reads the attribute Home Assistant shows in the UI.
func (s State) FriendlyName() string {
	if v, ok := s.Attributes["friendly_name"].(string); ok {
		return v
	}
	return ""
}

// Event is what a subscription delivers. For subscribe_events it is a Home
// Assistant event (EventType, Data). For subscribe_trigger Home Assistant
// sends only the trigger variables, so Variables is set and the rest is empty.
type Event struct {
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	Origin    string          `json:"origin"`
	TimeFired string          `json:"time_fired"`
	Variables json.RawMessage `json:"variables"`
	// Raw is the whole event payload, for subscriptions whose events are not
	// shaped like a bus event (subscribe_entities).
	Raw json.RawMessage `json:"-"`
}

// TriggerVariables is the part of a state trigger's variables the agent
// reads: variables.trigger.{entity_id, from_state, to_state}.
type TriggerVariables struct {
	Trigger struct {
		Platform  string `json:"platform"`
		EntityID  string `json:"entity_id"`
		FromState *State `json:"from_state"`
		ToState   *State `json:"to_state"`
	} `json:"trigger"`
}

// StateChangedData is the payload of a state_changed event.
type StateChangedData struct {
	EntityID string `json:"entity_id"`
	OldState *State `json:"old_state"`
	NewState *State `json:"new_state"`
}

// Target is the target block of a call_service command.
type Target struct {
	EntityID []string `json:"entity_id,omitempty"`
	DeviceID []string `json:"device_id,omitempty"`
	AreaID   []string `json:"area_id,omitempty"`
}
