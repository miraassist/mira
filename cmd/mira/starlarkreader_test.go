package main

import (
	"context"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/startools"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
	"github.com/miraassist/mira/internal/testsupport/logcap"
	"github.com/miraassist/mira/internal/tools"
)

// SL-22 mira.tool calls a read-only tool the skill names
// (docs/spec/api-tools.md, decision 19), through the service's reader and
// tool registry.
func TestSL22_MiraToolCallsAReadOnlyToolTheSkillNames(t *testing.T) {
	srv := fakemcp.New("geraet")
	t.Cleanup(srv.Close)
	var asked []string
	srv.AddTool("read_state", "state", `{"type":"object","properties":{"field":{"type":"string"}}}`, false, func(args map[string]any) (string, bool) {
		asked = append(asked, args["field"].(string))
		return `{"wert":54}`, false
	})
	srv.AddTool("set_plan", "plan", `{"type":"object"}`, false, func(map[string]any) (string, bool) { return "ok", false })
	log, _ := logcap.Logger()
	host := mcp.New(mcp.Options{Servers: []config.MCPServer{{Name: "geraet", Transport: config.TransportHTTP, URL: srv.Endpoint(),
		ReadOnlyTools: []string{"read_state"}}}, Logger: log})
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	var r starlarkReader
	r.reg.Store(tools.NewRegistry(host))

	// Given a skill whose tools.reads names geraet__read_state
	src := `
def lesen():
    """Liest."""
    r = mira.tool("geraet__read_state", {"field": "x"})
    return {"ok": r.ok, "wert": r.json["wert"], "text": r.text}

def fremd():
    """Liest etwas, das nicht unter reads steht."""
    return mira.tool("geraet__read_prices").ok

def planen():
    """Ruft ein schreibendes Werkzeug."""
    return mira.tool("geraet__set_plan", {"value": 80}).ok

def weg():
    """Ein Werkzeug, das es nicht gibt."""
    r = mira.tool("weg__x")
    return {"ok": r.ok, "error": r.error}
`
	f, err := startools.Load("rechnen", []byte(src), nil, log)
	if err != nil {
		t.Fatal(err)
	}
	env := startools.Env{Reader: &r, Reads: []string{"geraet__read_state", "geraet__set_plan", "weg__x"}}
	// When its tool calls mira.tool("geraet__read_state", {"field": "x"})
	res := f.Call(context.Background(), "lesen", []byte(`{}`), env)
	// Then it gets the tool's text
	if res.Err != nil || !strings.Contains(res.Text, `"wert":54`) || len(asked) != 1 || asked[0] != "x" {
		t.Errorf("lesen: %q %v, asked %v", res.Text, res.Err, asked)
	}
	if len(res.Events) != 1 || res.Events[0].Kind != "tool" || res.Events[0].Tool != "geraet__read_state" {
		t.Errorf("events %+v", res.Events)
	}
	// And a call of a tool not under tools.reads fails naming it
	if res := f.Call(context.Background(), "fremd", []byte(`{}`), env); res.Err == nil ||
		!strings.Contains(res.Err.Error(), "may not call geraet__read_prices") {
		t.Errorf("fremd: %v", res.Err)
	}
	// And a call of a tool that is not read-only fails, even when tools.reads names it
	if res := f.Call(context.Background(), "planen", []byte(`{}`), env); res.Err == nil ||
		!strings.Contains(res.Err.Error(), "not read-only") {
		t.Errorf("planen: %v", res.Err)
	}
	// A tool that is not there is an answer the function can go on with.
	if res := f.Call(context.Background(), "weg", []byte(`{}`), env); res.Err != nil || !strings.Contains(res.Text, "no such tool") {
		t.Errorf("weg: %q %v", res.Text, res.Err)
	}
}

// SL-22, continued: a tool that returns third-party text is refused, and a
// Starlark tool of a skill without an api section is no third-party text.
func TestSL22_ThirdPartyTextIsNotPassedOn(t *testing.T) {
	srv := fakemcp.New("suche")
	t.Cleanup(srv.Close)
	srv.AddTool("web", "Sucht im Netz", `{"type":"object"}`, true, func(map[string]any) (string, bool) { return "fremd", false })
	log, _ := logcap.Logger()
	host := mcp.New(mcp.Options{Servers: []config.MCPServer{{Name: "suche", Transport: config.TransportHTTP, URL: srv.Endpoint(),
		WebContent: true}}, Logger: log})
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	var r starlarkReader
	r.reg.Store(tools.NewRegistry(host))
	f, err := startools.Load("rechnen", []byte("def lesen():\n    \"\"\"Liest.\"\"\"\n    return mira.tool(\"suche__web\").ok\n"), nil, log)
	if err != nil {
		t.Fatal(err)
	}
	res := f.Call(context.Background(), "lesen", []byte(`{}`), startools.Env{Reader: &r, Reads: []string{"suche__web"}})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "third-party text") {
		t.Errorf("lesen: %q %v", res.Text, res.Err)
	}
}
