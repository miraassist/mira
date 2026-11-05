package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
)

// compatHost is Mira's MCP host connected to a fake server as "ha".
func compatHost(t *testing.T, srv *fakemcp.Server) *mcp.Host {
	t.Helper()
	t.Cleanup(srv.Close)
	h := mcp.New(mcp.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Servers: []config.MCPServer{
		{Name: "ha", Transport: config.TransportHTTP, URL: srv.Endpoint()},
	}})
	t.Cleanup(h.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return h
}

func noTool(map[string]any) (string, bool) { return "", false }

// HC-01 A missing tool fails the check and names it
func TestHC01_AMissingToolFailsTheCheckAndNamesIt(t *testing.T) {
	// Given Home Assistant's MCP server serves GetLiveContext and HassTurnOn only
	srv := fakemcp.New("home-assistant")
	srv.AddTool("homeassistant__GetLiveContext", "live context", `{"type":"object"}`, true, noTool)
	srv.AddTool("intent__HassTurnOn", "turn on", `{"type":"object"}`, false, noTool)
	h := compatHost(t, srv)
	// When the tools are checked
	r := checkTools(h.Tools())
	// Then the check fails and names GetDateTime and HassTurnOff
	if r.ok || !strings.Contains(r.detail, "GetDateTime") || !strings.Contains(r.detail, "HassTurnOff") {
		t.Errorf("check = %s", r)
	}
	// With all of them, the reading ones marked read-only, it passes.
	all := fakemcp.New("home-assistant")
	all.AddTool("homeassistant__GetLiveContext", "live context", `{"type":"object"}`, true, noTool)
	all.AddTool("llm__GetDateTime", "date", `{"type":"object"}`, true, noTool)
	all.AddTool("intent__HassTurnOn", "turn on", `{"type":"object"}`, false, noTool)
	all.AddTool("intent__HassTurnOff", "turn off", `{"type":"object"}`, false, noTool)
	if r := checkTools(compatHost(t, all).Tools()); !r.ok {
		t.Errorf("all tools there: %s", r)
	}
}

// HC-02 A missing prompt fails the check
func TestHC02_AMissingPromptFailsTheCheck(t *testing.T) {
	// Given Home Assistant's MCP server serves no prompt
	h := compatHost(t, fakemcp.Search("ok"))
	// When the prompt is checked
	r := checkPrompt(context.Background(), h, nil)
	// Then the check fails with the reason
	if r.ok || r.detail == "" {
		t.Errorf("check = %s", r)
	}
	if r := checkPrompt(context.Background(), compatHost(t, fakemcp.HA("ok")), nil); !r.ok {
		t.Errorf("Home Assistant's prompt is there: %s", r)
	}
}

// HC-03 A missing action fails the check and names it
func TestHC03_AMissingActionFailsTheCheckAndNamesIt(t *testing.T) {
	all := map[string]map[string][]string{}
	for _, a := range compatActions {
		domain, name, _ := strings.Cut(a, ".")
		if all[domain] == nil {
			all[domain] = map[string][]string{}
		}
		all[domain][name] = nil
	}
	if r := checkActions(all); !r.ok {
		t.Fatalf("all actions there: %s", r)
	}
	// Given Home Assistant has no action assist_satellite.start_conversation
	delete(all["assist_satellite"], "start_conversation")
	// When the actions are checked
	r := checkActions(all)
	// Then the check fails and names it
	if r.ok || !strings.Contains(r.detail, "assist_satellite.start_conversation") {
		t.Errorf("check = %s", r)
	}
}

// HC-04 A warning of the integration in Home Assistant's log fails the check
func TestHC04_AWarningOfTheIntegrationInTheLogFailsTheCheck(t *testing.T) {
	// Given Home Assistant's log has a deprecation warning from custom_components.mira
	deprecated := "\x1b[33m2026-11-05 00:22:25.233+01:00 WARNING (MainThread) [homeassistant.helpers.frame] Detected that custom integration 'mira' uses `device_registry.devices` as a mapping, which is deprecated; at custom_components/mira/managed.py, line 151. This will stop working in Home Assistant 2027.9.0\x1b[0m"
	unreachable := "2026-11-05 00:21:25.949+01:00 ERROR (MainThread) [custom_components.mira.coordinator] Error fetching mira data: GET /v1/status: ClientConnectorError"
	// When the log is checked
	r := checkLog(unreachable + "\n" + deprecated + "\n")
	// Then the check fails and shows the line
	if r.ok || !strings.Contains(r.detail, "managed.py, line 151") || strings.Contains(r.detail, "\x1b") {
		t.Errorf("check = %s", r)
	}
	// The Mira service being away for a while is no matter of compatibility.
	if r := checkLog(unreachable); !r.ok {
		t.Errorf("an unreachable service failed the check: %s", r)
	}
}
