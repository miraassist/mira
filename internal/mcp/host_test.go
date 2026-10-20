package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func start(t *testing.T, opts mcp.Options) *mcp.Host {
	t.Helper()
	opts.Logger = quietLogger()
	h := mcp.New(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(h.Close)
	return h
}

func TestToolsAreAggregatedPrefixedAndSorted(t *testing.T) {
	ha := fakemcp.HA("Küche: Licht an")
	ha.Token = "ha-secret"
	t.Cleanup(ha.Close)
	search := fakemcp.Search("Ergebnis")
	t.Cleanup(search.Close)

	h := start(t, mcp.Options{
		Servers: []config.MCPServer{
			{Name: "search", Transport: config.TransportHTTP, URL: search.Endpoint(), WebContent: true},
			{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint(), Token: "ha-secret"},
		},
		ReadOnlyTools: []string{"ha__GetLiveContext"},
	})

	var names []string
	for _, tool := range h.Tools() {
		names = append(names, tool.QualifiedName())
	}
	want := "ha__GetLiveContext ha__HassTurnOff ha__HassTurnOn search__web_search"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("tools = %s, want %s", got, want)
	}

	live, _ := h.Lookup("ha__GetLiveContext")
	if !live.ReadOnly {
		t.Error("GetLiveContext must be read-only through the configured list")
	}
	on, _ := h.Lookup("ha__HassTurnOn")
	if on.ReadOnly || on.WebContent {
		t.Errorf("HassTurnOn flags = %+v", on)
	}
	web, _ := h.Lookup("search__web_search")
	if !web.ReadOnly || !web.WebContent {
		t.Errorf("web_search flags: readOnly from annotation, webContent from config; got %+v", web)
	}
	if !json.Valid(on.Schema) || !strings.Contains(string(on.Schema), `"domain"`) {
		t.Errorf("schema not carried over: %s", on.Schema)
	}
}

func TestCallRoutesToTheRightServer(t *testing.T) {
	ha := fakemcp.HA("Wohnzimmer: 21,5 °C")
	t.Cleanup(ha.Close)
	h := start(t, mcp.Options{Servers: []config.MCPServer{
		{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint()},
	}})
	ctx := context.Background()

	res, err := h.Call(ctx, "ha__GetLiveContext", nil)
	if err != nil || res.IsError || res.Text != "Wohnzimmer: 21,5 °C" {
		t.Fatalf("GetLiveContext = %+v, %v", res, err)
	}
	if _, err := h.Call(ctx, "ha__HassTurnOn", json.RawMessage(`{"name":"Deckenlicht","area":"Küche"}`)); err != nil {
		t.Fatalf("HassTurnOn: %v", err)
	}
	calls := ha.Calls()
	if len(calls) != 2 || calls[1].Tool != "HassTurnOn" || calls[1].Args["area"] != "Küche" {
		t.Fatalf("recorded calls = %+v", calls)
	}

	if _, err := h.Call(ctx, "ha__Nope", nil); !errors.Is(err, mcp.ErrUnknownTool) {
		t.Errorf("unknown tool error = %v", err)
	}
	if _, err := h.Call(ctx, "other__GetLiveContext", nil); !errors.Is(err, mcp.ErrUnknownTool) {
		t.Errorf("unknown server error = %v", err)
	}
	if _, err := h.Call(ctx, "ha__HassTurnOn", json.RawMessage(`[1]`)); err == nil {
		t.Error("non-object arguments must be rejected")
	}
}

func TestUnreachableServerDoesNotBlockStart(t *testing.T) {
	ha := fakemcp.HA("ok")
	t.Cleanup(ha.Close)
	h := start(t, mcp.Options{Servers: []config.MCPServer{
		{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint()},
		{Name: "search", Transport: config.TransportHTTP, URL: "http://127.0.0.1:1/mcp"},
	}, CallTimeout: 2 * time.Second})

	if n := len(h.Tools()); n != 3 {
		t.Fatalf("tools = %d, want the 3 of the reachable server", n)
	}
	var down bool
	for _, st := range h.Status() {
		if st.Name == "search" && !st.Connected && st.Error != "" {
			down = true
		}
	}
	if !down {
		t.Errorf("status does not report the unreachable server: %+v", h.Status())
	}
}

func TestStartFailsWhenNothingIsReachable(t *testing.T) {
	h := mcp.New(mcp.Options{Logger: quietLogger(), CallTimeout: 2 * time.Second, Servers: []config.MCPServer{
		{Name: "ha", Transport: config.TransportHTTP, URL: "http://127.0.0.1:1/mcp"},
	}})
	if err := h.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded with no reachable server")
	}
}

func TestToolListChangeFiresOnChange(t *testing.T) {
	ha := fakemcp.HA("ok")
	t.Cleanup(ha.Close)
	var changes atomic.Int32
	latest := make(chan []mcp.Tool, 4)
	h := start(t, mcp.Options{
		Servers: []config.MCPServer{{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint()}},
		OnChange: func(tools []mcp.Tool) {
			changes.Add(1)
			latest <- tools
		},
	})
	<-latest // the initial build counts as a change from nothing
	_ = h

	ha.RemoveTool("HassTurnOff")
	select {
	case tools := <-latest:
		for _, tool := range tools {
			if tool.Name == "HassTurnOff" {
				t.Fatal("removed tool still listed")
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tool list change was not picked up")
	}
}

func TestPromptQualifiesToolNames(t *testing.T) {
	ha := fakemcp.HA("ok")
	ha.SetPrompt("Assist", "Use HassTurnOn to lock and HassTurnOff to unlock. Ask GetLiveContext. ha__HassTurnOn stays; HassTurnOnly is no tool.")
	t.Cleanup(ha.Close)
	h := start(t, mcp.Options{Servers: []config.MCPServer{
		{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint()},
	}})
	got, err := h.Prompt(context.Background(), "ha", "Assist")
	if err != nil {
		t.Fatal(err)
	}
	want := "Use ha__HassTurnOn to lock and ha__HassTurnOff to unlock. Ask ha__GetLiveContext. ha__HassTurnOn stays; HassTurnOnly is no tool."
	if got != want {
		t.Errorf("prompt =\n%s\nwant\n%s", got, want)
	}
	if _, err := h.Prompt(context.Background(), "ha", "Nope"); err == nil {
		t.Error("unknown prompt did not fail")
	}
	if _, err := h.Prompt(context.Background(), "nope", "Assist"); err == nil {
		t.Error("unknown server did not fail")
	}
}

// A remote server that restarts forgets its sessions. The next call fails
// on the old session; Mira connects anew and repeats it once.
func TestCallReconnectsWhenTheServerForgotTheSession(t *testing.T) {
	ha := fakemcp.HA("Küche: Licht an")
	t.Cleanup(ha.Close)
	h := start(t, mcp.Options{Servers: []config.MCPServer{
		{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint()},
	}})
	ha.ForgetSessions()
	res, err := h.Call(context.Background(), "ha__GetLiveContext", nil)
	if err != nil || res.Text != "Küche: Licht an" {
		t.Fatalf("after the server forgot the session: %+v, %v", res, err)
	}
	ha.ForgetSessions()
	if text, err := h.Prompt(context.Background(), "ha", "nope"); err == nil {
		t.Errorf("unknown prompt answered %q", text)
	}
}

// Home Assistant's MCP server offers a satellite its timers only when asked
// as that satellite. The host asks as each satellite and joins the lists, and
// a call carries the satellite that made the request.
func TestDeviceContextBringsTheToolsOfTheSatelliteAlong(t *testing.T) {
	ha := fakemcp.HA("Küche: Licht an")
	t.Cleanup(ha.Close)
	ha.AddDeviceTool("HassStartTimer", "Starts a timer", `{"type":"object","properties":{"minutes":{"type":"integer"}}}`,
		func(map[string]any) (string, bool) { return `{"response_type":"action_done"}`, false })
	yes := true
	cfg := config.MCPServer{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint(), DeviceContext: &yes}

	h := start(t, mcp.Options{Servers: []config.MCPServer{cfg}, Devices: func() []string { return []string{"sat-kueche", "sat-bad"} }})
	if _, ok := h.Lookup("ha__HassStartTimer"); !ok {
		t.Fatalf("the satellite's timer tool is missing: %v", h.Tools())
	}
	if _, ok := h.Lookup("ha__GetLiveContext"); !ok {
		t.Error("the plain tools are missing")
	}
	// Once without a satellite and once as each.
	if got := ha.Lists(); len(got) != 3 || got[0] != "" || got[1] != "sat-kueche" || got[2] != "sat-bad" {
		t.Errorf("listings: %q", got)
	}

	// The call names the satellite of the request.
	ctx := mcp.WithDevice(context.Background(), "sat-kueche")
	if _, err := h.Call(ctx, "ha__HassStartTimer", json.RawMessage(`{"minutes":5}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Call(context.Background(), "ha__HassTurnOn", json.RawMessage(`{"name":"x"}`)); err != nil {
		t.Fatal(err)
	}
	calls := ha.Calls()
	if len(calls) != 2 || calls[0].Device != "sat-kueche" || calls[1].Device != "" {
		t.Errorf("calls: %+v", calls)
	}
}

// A server that does not take the satellite (a web search) is neither asked
// as a satellite nor told which one.
func TestOtherServersDoNotGetTheSatellite(t *testing.T) {
	search := fakemcp.Search("Ergebnis")
	t.Cleanup(search.Close)
	cfg := config.MCPServer{Name: "search", Transport: config.TransportHTTP, URL: search.Endpoint()}
	h := start(t, mcp.Options{Servers: []config.MCPServer{cfg}, Devices: func() []string { return []string{"sat-kueche"} }})
	if _, err := h.Call(mcp.WithDevice(context.Background(), "sat-kueche"), "search__web_search", json.RawMessage(`{"query":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if got := search.Lists(); len(got) != 1 || got[0] != "" {
		t.Errorf("listings: %q", got)
	}
	if c := search.Calls(); len(c) != 1 || c[0].Device != "" {
		t.Errorf("calls: %+v", c)
	}
}

// A satellite that comes back with timers shows up at the next refresh.
func TestRefreshPicksUpANewSatellite(t *testing.T) {
	ha := fakemcp.HA("x")
	t.Cleanup(ha.Close)
	ha.AddDeviceTool("HassStartTimer", "Starts a timer", `{"type":"object","properties":{}}`, func(map[string]any) (string, bool) { return "{}", false })
	yes := true
	cfg := config.MCPServer{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint(), DeviceContext: &yes}
	devices := []string{}
	h := start(t, mcp.Options{Servers: []config.MCPServer{cfg}, Devices: func() []string { return devices }})
	if _, ok := h.Lookup("ha__HassStartTimer"); ok {
		t.Fatal("timers without any satellite")
	}
	devices = []string{"sat-1"}
	h.Refresh(context.Background())
	if _, ok := h.Lookup("ha__HassStartTimer"); !ok {
		t.Error("the new satellite's tools did not arrive")
	}
}
