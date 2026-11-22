package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
)

// Home Assistant's intent answers where nothing succeeded
// (docs/spec/intent-results.md).

const onlyArea = `{"speech": {}, "response_type": "action_done", "data": {"success": [{"name": "Bad unten", "type": "area", "id": "bad_unten"}], "failed": [{"name": "Dot 2", "type": "entity", "id": "media_player.dot_2"}]}}`

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// intentHost is a host with the servers "ha" (Home Assistant's, the intent
// server) and "other", each with a tool answering what answers[name] holds.
func intentHost(t *testing.T, answers map[string]string, haError bool) (*mcp.Host, *logBuf) {
	t.Helper()
	var servers []config.MCPServer
	for _, name := range []string{"ha", "other"} {
		srv := fakemcp.New(name)
		t.Cleanup(srv.Close)
		name := name
		srv.AddTool("HassMediaUnpause", "Resumes a media player", `{"type":"object","properties":{}}`, false,
			func(map[string]any) (string, bool) { return answers[name], name == "ha" && haError })
		servers = append(servers, config.MCPServer{Name: name, Transport: config.TransportHTTP, URL: srv.Endpoint()})
	}
	logs := &logBuf{}
	h := mcp.New(mcp.Options{Servers: servers, IntentServer: "ha", CallTimeout: 5 * time.Second,
		Logger: slog.New(slog.NewTextHandler(logs, nil))})
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h, logs
}

func callIntent(t *testing.T, h *mcp.Host, server string) mcp.Result {
	t.Helper()
	res, err := h.Call(context.Background(), server+"__HassMediaUnpause", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// IR-01 An area named and every entity failed is an error
func TestIR01_AnAreaNamedAndEveryEntityFailedIsAnError(t *testing.T) {
	h, _ := intentHost(t, map[string]string{"ha": onlyArea}, false)
	res := callIntent(t, h, "ha")
	if !res.IsError || res.Text != onlyArea {
		t.Errorf("result %+v", res)
	}
}

// IR-02 A partial success stays a success
func TestIR02_APartialSuccessStaysASuccess(t *testing.T) {
	partial := `{"response_type": "action_done", "data": {"success": [{"name": "Bad unten", "type": "area", "id": "bad_unten"}, {"name": "Lampe", "type": "entity", "id": "light.lampe"}], "failed": [{"name": "Dot 2", "type": "entity", "id": "media_player.dot_2"}]}}`
	h, _ := intentHost(t, map[string]string{"ha": partial}, false)
	if res := callIntent(t, h, "ha"); res.IsError {
		t.Errorf("result %+v", res)
	}
}

// IR-03 An answer that is no such intent answer is left as it is
func TestIR03_AnAnswerThatIsNoSuchIntentAnswerIsLeftAsItIs(t *testing.T) {
	for _, text := range []string{
		`{"response_type": "action_done", "data": {"success": [{"name": "Bad unten", "type": "area"}], "failed": []}}`,
		`{"response_type": "query_answer", "data": {"success": [], "failed": [{"name": "Dot 2", "type": "entity"}]}}`,
		`Licht ist an`,
	} {
		h, _ := intentHost(t, map[string]string{"ha": text}, false)
		if res := callIntent(t, h, "ha"); res.IsError {
			t.Errorf("%s: %+v", text, res)
		}
	}
	// An error Home Assistant raised itself stays an error.
	h, _ := intentHost(t, map[string]string{"ha": "Failed to call media_play for: ['media_player.dot_2']"}, true)
	if res := callIntent(t, h, "ha"); !res.IsError {
		t.Errorf("raised error: %+v", res)
	}
}

// IR-04 Other servers are not touched
func TestIR04_OtherServersAreNotTouched(t *testing.T) {
	h, _ := intentHost(t, map[string]string{"other": onlyArea}, false)
	if res := callIntent(t, h, "other"); res.IsError {
		t.Errorf("result %+v", res)
	}
}

// IR-05 A marked answer leaves a log line
func TestIR05_AMarkedAnswerLeavesALogLine(t *testing.T) {
	h, logs := intentHost(t, map[string]string{"ha": onlyArea}, false)
	callIntent(t, h, "ha")
	l := logs.String()
	for _, want := range []string{`msg="home assistant acted on nothing"`, "tool=ha__HassMediaUnpause", "failed=[media_player.dot_2]"} {
		if !strings.Contains(l, want) {
			t.Errorf("log without %s: %s", want, l)
		}
	}
}
