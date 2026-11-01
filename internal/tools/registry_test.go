package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
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

// A result longer than a quarter of the model's context is cut, and the
// model is told how much it sees and how to ask (docs/spec/context.md, 5.1).
func TestALargeResultIsCutAndSaysSo(t *testing.T) {
	long := strings.Repeat("ä", 6000) // 12,000 bytes
	r := NewRegistry(nil, InternalTool{Name: "big", Run: func(context.Context, Invocation) (string, error) { return long, nil }})
	run := func(inv Invocation) string {
		t.Helper()
		inv.Name = "big"
		out, _, err := r.Execute(context.Background(), inv)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := run(Invocation{ContextTokens: 32768, Lang: i18n.DE}); out != long {
		t.Errorf("a result within the model's share was changed: %d chars", len(out))
	}
	out := run(Invocation{ContextTokens: 4096, Lang: i18n.DE})
	if !strings.HasPrefix(out, strings.Repeat("ä", 2048)) || !strings.Contains(out, "[Gekürzt: Du siehst 4096 von 12000 Zeichen") {
		t.Errorf("cut result: %q", out[len(out)-160:])
	}
	if out := run(Invocation{ContextTokens: 4096, Lang: i18n.EN}); !strings.Contains(out, "[Cut: you see 4096 of 12000 characters") {
		t.Errorf("English note: %q", out[len(out)-120:])
	}
	// without a context the default holds: 16,384
	if out := run(Invocation{}); out != long {
		t.Errorf("default limit cut %d chars", len(out))
	}
}
