package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
	"github.com/miraassist/mira/internal/ui"
)

// PG-10 The page gets a skill's setup with the skill's label and the
// tool's schema; the call goes to the skill's server and the log names who,
// the skill, the tool and the outcome, not the arguments.
func TestPG10_ThePageBuildsTheFormFromTheToolsSchema(t *testing.T) {
	ha := fakemcp.HA("ok")
	t.Cleanup(ha.Close)
	var logs bytes.Buffer
	a := &app{log: slog.New(slog.NewTextHandler(&logs, nil))}
	srv := config.MCPServer{Name: "laden", Skill: "laden", Transport: config.TransportHTTP, URL: ha.Endpoint(),
		Setup: []config.SetupTool{{Tool: "HassTurnOff", Label: "Ausschalten"}, {Tool: "gibtsnicht", Label: "Fehlt"}}}
	a.skillsOn = []skills.Skill{{Name: "laden", MCP: &srv, DataDir: "/config/skill-data/laden",
		Program: &skills.Program{Name: "korb", Version: "0.9.0", SHA256: strings.Repeat("a", 64)}}}
	a.mcp = mcp.New(mcp.Options{Servers: []config.MCPServer{srv}, Logger: a.log})
	if err := a.mcp.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.mcp.Close)

	var info ui.SkillInfo
	a.setupInfo(a.skillsOn[0], &info)
	if len(info.Setup) != 2 || info.Setup[0].Label != "Ausschalten" || len(info.Setup[0].Schema) == 0 || info.Setup[0].Description == "" {
		t.Fatalf("setup %+v", info.Setup)
	}
	if info.Setup[1].Schema != nil {
		t.Error("a tool the server does not offer has a form")
	}
	if info.Program == nil || info.Program.Version != "0.9.0" || info.DataDir != "/config/skill-data/laden" {
		t.Errorf("program %+v, folder %q", info.Program, info.DataDir)
	}

	res, err := setupCalls{a: a}.Call(context.Background(), "admin-1", "laden", "HassTurnOff", json.RawMessage(`{"name":"Geheimnis"}`))
	if err != nil || res.IsError {
		t.Fatalf("call: %+v %v", res, err)
	}
	line := logs.String()
	if !strings.Contains(line, "setup tool called") || !strings.Contains(line, "admin-1") || !strings.Contains(line, "HassTurnOff") ||
		strings.Contains(line, "Geheimnis") {
		t.Errorf("log %q", line)
	}
	if _, err := (setupCalls{a: a}).Call(context.Background(), "admin-1", "laden", "HassTurnOn", nil); !errors.Is(err, ui.ErrNoSetupTool) {
		t.Errorf("a tool that is no setup tool: %v", err)
	}
}
