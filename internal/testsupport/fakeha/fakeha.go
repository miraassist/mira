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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	haws "github.com/miraassist/mira/internal/ha/ws"
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

	calls []ServiceCall
	// commands are the integration's commands ("mira/…") the agent sent.
	commands []map[string]any
	// conns holds live connections so events can be pushed and so a test can
	// drop the connection to exercise reconnect handling.
	conns map[*connState]bool
	// connectCount counts successful authentications.
	connectCount int
}

type connState struct {
	conn *websocket.Conn
	mu   sync.Mutex
	// lastID is the highest command id seen; like Home Assistant, a lower
	// or equal one is refused (id_reuse).
	lastID int
	// subs maps subscription id to the event type it wants ("" means all).
	subs map[int]string
	// entitySubs maps subscription id to what a subscribe_entities
	// subscription watches: its entity ids and included domains.
	entitySubs map[int]entityFilter
}

func New() *Server {
	s := &Server{
		Token: "test-token",
		conns: map[*connState]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/websocket", s.handle)
	s.Server = httptest.NewServer(mux)
	return s
}

// Commands are the integration's commands ("mira/…") received so far.
func (s *Server) Commands() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.commands...)
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

// SetState changes one entity's state and sends the change to every
// subscribe_entities subscription watching it, in Home Assistant's compressed
// diff form. It returns how many subscriptions got it.
// entityFilter is subscribe_entities' filter as Home Assistant applies it:
// entity_ids, and include.domains.
type entityFilter struct{ ids, domains []string }

func (f entityFilter) match(entityID string) bool {
	domain, _, _ := strings.Cut(entityID, ".")
	return slices.Contains(f.ids, entityID) || slices.Contains(f.domains, domain)
}

func (s *Server) SetState(entityID, state string, attributes map[string]any) int {
	s.mu.Lock()
	// Copy on write: dispatch reads the old slice without the lock.
	states := slices.Clone(s.states)
	found := false
	for i := range states {
		if states[i].EntityID == entityID {
			states[i].State, found = state, true
			if attributes != nil {
				states[i].Attributes = attributes
			}
		}
	}
	if !found {
		states = append(states, haws.State{EntityID: entityID, State: state, Attributes: attributes})
	}
	s.states = states
	conns := make([]*connState, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	plus := map[string]any{"s": state, "lc": float64(time.Now().Unix())}
	if attributes != nil {
		plus["a"] = attributes
	}
	sent := 0
	for _, cs := range conns {
		cs.mu.Lock()
		for id, want := range cs.entitySubs {
			if !want.match(entityID) {
				continue
			}
			event := map[string]any{"c": map[string]any{entityID: map[string]any{"+": plus}}}
			if !found { // a new entity comes whole, as in Home Assistant
				event = map[string]any{"a": map[string]any{entityID: map[string]any{"s": state, "a": attributes, "lc": plus["lc"]}}}
			}
			frame := map[string]any{"id": id, "type": "event", "event": event}
			if writeJSON(cs.conn, frame) == nil {
				sent++
			}
		}
		cs.mu.Unlock()
	}
	return sent
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
		n += len(cs.subs) + len(cs.entitySubs)
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
	cs := &connState{conn: conn, subs: map[int]string{}, entitySubs: map[int]entityFilter{}}

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

	if id <= cs.lastID {
		fail("id_reuse", "Identifier values have to increase.")
		return
	}
	cs.lastID = id

	s.mu.Lock()
	areas, devices, entities := s.areas, s.devices, s.entities
	s.mu.Unlock()

	switch cmdType {
	case "auth/current_user":
		reply(map[string]any{"id": "mira-user", "name": "mira", "is_admin": false})
	case haws.CmdAreaRegistryList:
		reply(areas)
	case haws.CmdDeviceRegistryList:
		reply(devices)
	case haws.CmdEntityRegistryList:
		reply(entities)
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
	case "subscribe_trigger":
		// Admin-only in Home Assistant; the agent runs as a restricted user.
		fail("unauthorized", "Unauthorized")
	case haws.CmdSubscribeEntities:
		strs := func(v any) []string {
			list, _ := v.([]any)
			out := make([]string, 0, len(list))
			for _, x := range list {
				if s, ok := x.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
		want := entityFilter{ids: strs(cmd["entity_ids"])}
		if include, ok := cmd["include"].(map[string]any); ok {
			want.domains = strs(include["domains"])
		}
		// Registering, reading the states and sending them happen under the
		// connection lock, which SetState holds while sending diffs: like in
		// Home Assistant, no change falls between the snapshot and the diffs.
		cs.mu.Lock()
		defer cs.mu.Unlock()
		cs.entitySubs[id] = want
		reply(nil)
		s.mu.Lock()
		states := s.states
		s.mu.Unlock()
		add := map[string]any{}
		for _, st := range states {
			if want.match(st.EntityID) {
				add[st.EntityID] = map[string]any{"s": st.State, "a": st.Attributes, "lc": 1.0}
			}
		}
		writeJSON(cs.conn, map[string]any{"id": id, "type": "event", "event": map[string]any{"a": add}})
	case haws.CmdUnsubscribeEvents:
		sub := intOf(cmd["subscription"])
		cs.mu.Lock()
		delete(cs.subs, sub)
		delete(cs.entitySubs, sub)
		cs.mu.Unlock()
		reply(nil)
	default:
		if strings.HasPrefix(cmdType, "mira/") {
			// Mira's integration answers its own commands; the fake takes
			// them and records them.
			s.mu.Lock()
			s.commands = append(s.commands, cmd)
			s.mu.Unlock()
			reply(map[string]any{})
			return
		}
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
