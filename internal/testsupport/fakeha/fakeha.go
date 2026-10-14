// Package fakeha is an in-process stand-in for the Home Assistant WebSocket
// API. It performs the auth handshake, answers registry and state commands
// from data the test sets, records service calls, and can push events into
// live subscriptions — enough to exercise reconnects, the structure block and
// the notifier without a Home Assistant instance.
package fakeha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/coder/websocket"

	haws "github.com/sker65/lares/internal/ha/ws"
)

// ServiceCall is one recorded call_service command.
type ServiceCall struct {
	Domain  string
	Service string
	Data    map[string]any
	Target  map[string]any
}

type Server struct {
	*httptest.Server

	mu sync.Mutex
	// Token is the access token the fake accepts.
	Token string

	areas    []haws.Area
	devices  []haws.Device
	entities []haws.Entity
	states   []haws.State
	exposed  map[string]map[string]any
	// exposedErr makes the exposure command fail, which is the case on
	// Home Assistant versions that do not have it.
	exposedErr bool

	calls []ServiceCall
	// conns holds live connections so events can be pushed and so a test can
	// drop the connection to exercise reconnect handling.
	conns map[*connState]bool
	// connectCount counts successful authentications.
	connectCount int
}

type connState struct {
	conn *websocket.Conn
	mu   sync.Mutex
	// subs maps subscription id to the event type it wants ("" means all).
	subs map[int]string
	// triggers maps subscription id to its trigger definition.
	triggers map[int]map[string]any
}

func New() *Server {
	s := &Server{
		Token:   "test-token",
		conns:   map[*connState]bool{},
		exposed: map[string]map[string]any{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/websocket", s.handle)
	s.Server = httptest.NewServer(mux)
	return s
}

// SetRegistry installs the registry data the fake answers with.
func (s *Server) SetRegistry(areas []haws.Area, devices []haws.Device, entities []haws.Entity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.areas, s.devices, s.entities = areas, devices, entities
}

func (s *Server) SetStates(states []haws.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states = states
}

// SetExposed marks entity ids as exposed to the conversation assistant.
func (s *Server) SetExposed(entityIDs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exposed = map[string]map[string]any{}
	for _, id := range entityIDs {
		s.exposed[id] = map[string]any{"conversation": true}
	}
}

// FailExposedEntities makes the exposure command return an error, imitating a
// Home Assistant version that does not support it.
func (s *Server) FailExposedEntities(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exposedErr = fail
}

// ServiceCalls returns everything that was called so far.
func (s *Server) ServiceCalls() []ServiceCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ServiceCall, len(s.calls))
	copy(out, s.calls)
	return out
}

// ConnectCount is how often a client authenticated successfully. A reconnect
// increments it.
func (s *Server) ConnectCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectCount
}

// DropConnections closes every live connection, so the client has to reconnect.
func (s *Server) DropConnections() {
	s.mu.Lock()
	conns := make([]*connState, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.conns = map[*connState]bool{}
	s.mu.Unlock()
	for _, c := range conns {
		c.conn.CloseNow()
	}
}

// PushEvent delivers an event to every subscription that asked for its type.
// It returns how many subscriptions received it.
func (s *Server) PushEvent(eventType string, data any) int {
	payload, _ := json.Marshal(data)
	s.mu.Lock()
	conns := make([]*connState, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	sent := 0
	for _, cs := range conns {
		cs.mu.Lock()
		targets := make([]int, 0, len(cs.subs))
		for id, want := range cs.subs {
			if want == "" || want == eventType {
				targets = append(targets, id)
			}
		}
		cs.mu.Unlock()
		for _, id := range targets {
			frame := map[string]any{
				"id": id, "type": "event",
				"event": map[string]any{
					"event_type": eventType,
					"data":       json.RawMessage(payload),
					"origin":     "LOCAL",
					"time_fired": time.Now().UTC().Format(time.RFC3339),
				},
			}
			if writeJSON(cs.conn, frame) == nil {
				sent++
			}
		}
	}
	return sent
}

// PushTrigger delivers a trigger event to every trigger subscription, in the
// shape Home Assistant uses: the event carries only variables and context.
func (s *Server) PushTrigger(variables any) int {
	s.mu.Lock()
	conns := make([]*connState, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	sent := 0
	entity := triggerEntity(variables)
	for _, cs := range conns {
		cs.mu.Lock()
		ids := make([]int, 0, len(cs.triggers))
		for id, trig := range cs.triggers {
			// Like Home Assistant, only triggers watching this entity fire.
			if entity == "" || watches(trig, entity) {
				ids = append(ids, id)
			}
		}
		cs.mu.Unlock()
		for _, id := range ids {
			frame := map[string]any{
				"id": id, "type": "event",
				"event": map[string]any{
					"variables": variables,
					"context":   map[string]any{"id": "ctx"},
				},
			}
			if writeJSON(cs.conn, frame) == nil {
				sent++
			}
		}
	}
	return sent
}

// triggerEntity reads variables.trigger.entity_id, if present.
func triggerEntity(variables any) string {
	v, _ := variables.(map[string]any)
	t, _ := v["trigger"].(map[string]any)
	id, _ := t["entity_id"].(string)
	return id
}

// watches reports whether a trigger definition names the entity.
func watches(trigger map[string]any, entity string) bool {
	switch ids := trigger["entity_id"].(type) {
	case string:
		return ids == entity
	case []any:
		for _, id := range ids {
			if id == entity {
				return true
			}
		}
		return false
	}
	return true // no entity filter: fires for everything
}

// SubscriptionCount reports how many live subscriptions exist, which is how a
// test checks that they survived a reconnect.
func (s *Server) SubscriptionCount() int {
	s.mu.Lock()
	conns := make([]*connState, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	n := 0
	for _, cs := range conns {
		cs.mu.Lock()
		n += len(cs.subs) + len(cs.triggers)
		cs.mu.Unlock()
	}
	return n
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(32 << 20)
	ctx := context.Background()
	cs := &connState{conn: conn, subs: map[int]string{}, triggers: map[int]map[string]any{}}

	writeJSON(conn, map[string]any{"type": "auth_required", "ha_version": "2026.10.0"})

	var auth struct {
		Type        string `json:"type"`
		AccessToken string `json:"access_token"`
	}
	if err := readJSON(ctx, conn, &auth); err != nil {
		conn.CloseNow()
		return
	}
	s.mu.Lock()
	ok := auth.AccessToken == s.Token
	s.mu.Unlock()
	if !ok {
		writeJSON(conn, map[string]any{"type": "auth_invalid",
			"error": map[string]any{"code": "invalid_auth", "message": "wrong token"}})
		conn.CloseNow()
		return
	}
	writeJSON(conn, map[string]any{"type": "auth_ok", "ha_version": "2026.10.0"})

	s.mu.Lock()
	s.conns[cs] = true
	s.connectCount++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, cs)
		s.mu.Unlock()
		conn.CloseNow()
	}()

	for {
		var cmd map[string]any
		if err := readJSON(ctx, conn, &cmd); err != nil {
			return
		}
		s.dispatch(cs, cmd)
	}
}

func (s *Server) dispatch(cs *connState, cmd map[string]any) {
	id := intOf(cmd["id"])
	cmdType, _ := cmd["type"].(string)

	reply := func(result any) {
		writeJSON(cs.conn, map[string]any{"id": id, "type": "result", "success": true, "result": result})
	}
	fail := func(code, msg string) {
		writeJSON(cs.conn, map[string]any{"id": id, "type": "result", "success": false,
			"error": map[string]any{"code": code, "message": msg}})
	}

	s.mu.Lock()
	areas, devices, entities, states := s.areas, s.devices, s.entities, s.states
	exposed, exposedErr := s.exposed, s.exposedErr
	s.mu.Unlock()

	switch cmdType {
	case haws.CmdAreaRegistryList:
		reply(areas)
	case haws.CmdDeviceRegistryList:
		reply(devices)
	case haws.CmdEntityRegistryList:
		reply(entities)
	case haws.CmdGetStates:
		reply(states)
	case haws.CmdExposedEntities:
		if exposedErr {
			fail("unknown_command", "command not supported")
			return
		}
		reply(map[string]any{"exposed_entities": exposed})
	case haws.CmdPing:
		writeJSON(cs.conn, map[string]any{"id": id, "type": "pong"})
	case haws.CmdCallService:
		call := ServiceCall{}
		call.Domain, _ = cmd["domain"].(string)
		call.Service, _ = cmd["service"].(string)
		call.Data, _ = cmd["service_data"].(map[string]any)
		call.Target, _ = cmd["target"].(map[string]any)
		s.mu.Lock()
		s.calls = append(s.calls, call)
		s.mu.Unlock()
		reply(map[string]any{"context": map[string]any{"id": "ctx"}})
	case haws.CmdSubscribeEvents:
		eventType, _ := cmd["event_type"].(string)
		cs.mu.Lock()
		cs.subs[id] = eventType
		cs.mu.Unlock()
		reply(nil)
	case haws.CmdSubscribeTrigger:
		trigger, _ := cmd["trigger"].(map[string]any)
		cs.mu.Lock()
		cs.triggers[id] = trigger
		cs.mu.Unlock()
		reply(nil)
	case haws.CmdUnsubscribeEvents:
		sub := intOf(cmd["subscription"])
		cs.mu.Lock()
		delete(cs.subs, sub)
		delete(cs.triggers, sub)
		cs.mu.Unlock()
		reply(nil)
	default:
		fail("unknown_command", "fakeha does not implement "+cmdType)
	}
}

func intOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	}
	return 0
}

func readJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSON(conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}
