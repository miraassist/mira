// Package fakemcp is a real MCP server over Streamable HTTP with tools the
// test defines. Calls are recorded so a test can check what the agent did.
//
// HA and Search build servers that look like Home Assistant's MCP server and
// a web search server, which is what most agent tests need.
package fakemcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Call is one recorded tool invocation.
type Call struct {
	Tool string
	Args map[string]any
}

// Handler answers a tool call. Returning isError reports a tool-level failure.
type Handler func(args map[string]any) (text string, isError bool)

type Server struct {
	*httptest.Server
	MCP *sdk.Server

	// Token, when set, is required as a bearer token.
	Token string

	mu    sync.Mutex
	calls []Call
}

// New starts an empty server. Add tools with AddTool.
func New(name string) *Server {
	s := &Server{MCP: sdk.NewServer(&sdk.Implementation{Name: name, Version: "test"}, nil)}
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s.MCP }, nil)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" && r.Header.Get("Authorization") != "Bearer "+s.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	return s
}

// URL is the endpoint for the MCP client.
func (s *Server) Endpoint() string { return s.Server.URL + "/" }

// AddTool registers a tool. Schema is a JSON Schema object as a string.
func (s *Server) AddTool(name, description, schema string, readOnly bool, h Handler) {
	tool := &sdk.Tool{Name: name, Description: description, InputSchema: json.RawMessage(schema)}
	if readOnly {
		tool.Annotations = &sdk.ToolAnnotations{ReadOnlyHint: true}
	}
	s.MCP.AddTool(tool, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var args map[string]any
		if len(req.Params.Arguments) > 0 {
			_ = json.Unmarshal(req.Params.Arguments, &args)
		}
		s.mu.Lock()
		s.calls = append(s.calls, Call{Tool: name, Args: args})
		s.mu.Unlock()
		text, isErr := h(args)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}, IsError: isErr}, nil
	})
}

// RemoveTool unregisters a tool, which notifies connected clients.
func (s *Server) RemoveTool(name string) { s.MCP.RemoveTools(name) }

// Calls returns every call so far.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// ResetCalls forgets the recorded calls.
func (s *Server) ResetCalls() {
	s.mu.Lock()
	s.calls = nil
	s.mu.Unlock()
}

// The schema Home Assistant uses for its intent tools, reduced to what the
// agent's policy looks at.
const intentSchema = `{"type":"object","properties":{"name":{"type":"string"},"area":{"type":"string"},"floor":{"type":"string"},"domain":{"type":"array","items":{"type":"string"}},"device_class":{"type":"array","items":{"type":"string"}}}}`

// HA builds a server shaped like Home Assistant's MCP server: GetLiveContext
// returns the given text, HassTurnOn and HassTurnOff record and succeed.
func HA(liveContext string) *Server {
	s := New("home-assistant")
	s.AddTool("GetLiveContext", "Provides real-time information about the CURRENT state, value, or mode of devices, sensors, entities, or areas.",
		`{"type":"object","properties":{}}`, false, func(map[string]any) (string, bool) {
			return liveContext, false
		})
	s.AddTool("HassTurnOn", "Turns on/opens/presses a device or entity. For locks, this performs a 'lock' action.", intentSchema, false,
		func(map[string]any) (string, bool) { return `{"success":true}`, false })
	s.AddTool("HassTurnOff", "Turns off/closes a device or entity. For locks, this performs an 'unlock' action.", intentSchema, false,
		func(map[string]any) (string, bool) { return `{"success":true}`, false })
	return s
}

// Search builds a web search server whose single tool returns the given text.
func Search(result string) *Server {
	s := New("search")
	s.AddTool("web_search", "Search the web.",
		`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`, true,
		func(map[string]any) (string, bool) { return result, false })
	return s
}
