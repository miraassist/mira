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

	"github.com/sker65/lares/internal/config"
	"github.com/sker65/lares/internal/mcp"
	"github.com/sker65/lares/internal/testsupport/fakemcp"
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
