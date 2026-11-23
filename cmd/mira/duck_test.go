package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/music"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
	"github.com/miraassist/mira/internal/tools"
)

// MU-40 A volume tool call of the turn keeps the players it reaches: only a
// call of Home Assistant's volume tools that succeeded reaches the ducker,
// through the registry, with the turn's area.
func TestMU40_OnlyHomeAssistantsVolumeToolsThatSucceededReachTheDucker(t *testing.T) {
	const schema = `{"type":"object","properties":{"name":{"type":"string"},"area":{"type":"string"},"volume_level":{"type":"integer"}}}`
	var servers []config.MCPServer
	for _, name := range []string{"ha", "other"} {
		srv := fakemcp.New(name)
		t.Cleanup(srv.Close)
		for _, tool := range []string{"HassSetVolume", "HassSetVolumeRelative", "HassMediaPlayerMute"} {
			srv.AddTool(tool, tool, schema, false, func(args map[string]any) (string, bool) {
				return "done", args["name"] == "Fehler"
			})
		}
		servers = append(servers, config.MCPServer{Name: name, Transport: config.TransportHTTP, URL: srv.Endpoint()})
	}
	host := mcp.New(mcp.Options{Servers: servers, IntentServer: haServer, CallTimeout: 5 * time.Second})
	ctx := context.Background()
	if err := host.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)

	var calls []string
	logs := &bytes.Buffer{}
	newDucker := func() *music.Ducker {
		calls = nil
		d := music.NewDucker(music.DuckOptions{
			Logger: slog.New(slog.NewTextHandler(logs, nil)),
			Players: func() []music.Player {
				return []music.Player{{Entity: "media_player.onkyo", Area: "wohnzimmer"}}
			},
			Satellite: func(string) string { return "wohnzimmer" },
			Named:     func(string, string) []string { return nil },
			Area:      func(string) string { return "" },
			Volumes: func(_ context.Context, ids []string) (map[string]music.Volume, error) {
				return map[string]music.Volume{"media_player.onkyo": {State: "playing", Level: 0.6, Known: true}}, nil
			},
			SetVolume: func(_ context.Context, id string, level float64) error {
				calls = append(calls, fmt.Sprintf("%s %.2f", id, level))
				return nil
			},
		})
		d.Update(ctx, map[string]string{"assist_satellite.wz": "idle"})
		d.Update(ctx, map[string]string{"assist_satellite.wz": "listening"})
		return d
	}
	for _, c := range []struct {
		tool, args, area string
		kept             bool
	}{
		{"ha__HassSetVolume", `{"volume_level":30}`, "wohnzimmer", true},
		{"ha__HassSetVolumeRelative", `{}`, "wohnzimmer", true},
		{"ha__HassSetVolume", `{"name":"Fehler"}`, "wohnzimmer", false},      // Home Assistant's error
		{"ha__HassMediaPlayerMute", `{}`, "wohnzimmer", false},               // muting is no volume
		{"other__HassSetVolume", `{"volume_level":30}`, "wohnzimmer", false}, // not Home Assistant's
		{"ha__HassSetVolume", `{"volume_level":30}`, "", false},              // a turn without an area
	} {
		d := newDucker()
		reg := tools.NewRegistry(host)
		reg.Observe(func(inv tools.Invocation) { volumeCall(slog.New(slog.NewTextHandler(logs, nil)), d, inv) })
		if _, _, err := reg.Execute(ctx, tools.Invocation{Name: c.tool, Args: []byte(c.args), Area: c.area}); err != nil {
			t.Fatal(err)
		}
		d.Update(ctx, map[string]string{"assist_satellite.wz": "idle"})
		want := []string{"media_player.onkyo 0.20", "media_player.onkyo 0.60"}
		if c.kept {
			want = want[:1]
		}
		if !slices.Equal(calls, want) {
			t.Errorf("%s %s (area %q): volume_set calls %v, want %v", c.tool, c.args, c.area, calls, want)
		}
	}
	if !strings.Contains(logs.String(), "these players keep their new volume") {
		t.Errorf("no log line of the volume command:\n%s", logs)
	}
}

// MU-41 … not those that lower themselves: the manufacturer is the one the
// device registry gives the player's device, read through the house
// structure.
func TestMU41_TheManufacturerComesFromTheDeviceRegistry(t *testing.T) {
	snap := structure.Build(structure.Registry{
		Areas:   []haws.Area{{AreaID: "wohnzimmer", Name: "Wohnzimmer"}},
		Devices: []haws.Device{{ID: "dev-dot", Manufacturer: "TECHO5"}, {ID: "dev-onkyo", Manufacturer: "Unknown manufacturer"}},
		Entities: []haws.Entity{
			{EntityID: "media_player.dot_2", Platform: "music_assistant", DeviceID: "dev-dot", AreaID: "wohnzimmer"},
			{EntityID: "media_player.onkyo", Platform: "music_assistant", DeviceID: "dev-onkyo", AreaID: "wohnzimmer"},
			{EntityID: "media_player.cast", Platform: "music_assistant", AreaID: "wohnzimmer"},
		},
		Exposed: map[string]bool{"media_player.dot_2": true, "media_player.onkyo": true, "media_player.cast": true},
	})
	got := map[string]string{}
	for _, p := range musicPlayers(snap) {
		got[p.Entity] = p.Manufacturer
	}
	want := map[string]string{"media_player.dot_2": "TECHO5", "media_player.onkyo": "Unknown manufacturer", "media_player.cast": ""}
	if !maps.Equal(got, want) {
		t.Errorf("manufacturers %v, want %v", got, want)
	}
}
