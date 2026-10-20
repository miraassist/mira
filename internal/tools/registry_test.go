package tools

import (
	"context"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
)

// A tool of Home Assistant is called as the satellite that heard the
// request, so it acts as for Home Assistant's own conversation agents.
func TestHomeAssistantToolsGetTheSatellite(t *testing.T) {
	ha := fakemcp.HA("x")
	defer ha.Close()
	yes := true
	host := mcp.New(mcp.Options{Servers: []config.MCPServer{{Name: "ha", Transport: config.TransportHTTP, URL: ha.Endpoint(), DeviceContext: &yes}}})
	ctx := context.Background()
	if err := host.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	reg := NewRegistry(host)
	if _, _, err := reg.Execute(ctx, Invocation{Name: "ha__HassTurnOn", Args: []byte(`{"name":"x"}`), DeviceID: "sat-wohnzimmer"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Execute(ctx, Invocation{Name: "ha__HassTurnOn", Args: []byte(`{"name":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	calls := ha.Calls()
	if len(calls) != 2 || calls[0].Device != "sat-wohnzimmer" || calls[1].Device != "" {
		t.Errorf("calls: %+v", calls)
	}
}
