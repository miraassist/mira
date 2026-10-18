package mcp_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp"
)

// TestMain lets the test binary double as a chatty stdio MCP server: with
// MIRA_FAKE_STDIO_SERVER set it prints log lines to stdout, as some real
// servers do, and then serves one tool.
func TestMain(m *testing.M) {
	if os.Getenv("MIRA_FAKE_STDIO_SERVER") == "1" {
		fmt.Println("Web Search MCP Server starting...")
		fmt.Println("[BrowserPool] Configuration: maxBrowsers=3")
		s := sdk.NewServer(&sdk.Implementation{Name: "chatty", Version: "test"}, nil)
		s.AddTool(&sdk.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}},
			func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				fmt.Println("[MCP] Tool call received: echo") // more noise mid-session
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "pong"}}}, nil
			})
		_ = s.Run(context.Background(), &sdk.StdioTransport{})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestStdioServerThatLogsToStdout(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := start(t, mcp.Options{Servers: []config.MCPServer{{
		Name: "chatty", Transport: config.TransportStdio, Command: self,
		Env: []string{"MIRA_FAKE_STDIO_SERVER=1"},
	}}, CallTimeout: 10 * time.Second})

	if tools := h.Tools(); len(tools) != 1 || tools[0].QualifiedName() != "chatty__echo" {
		t.Fatalf("tools = %+v", tools)
	}
	res, err := h.Call(context.Background(), "chatty__echo", nil)
	if err != nil || res.Text != "pong" {
		t.Fatalf("call = %+v, %v", res, err)
	}
}

// A server started over stdio gets only the few variables it needs to run,
// never Mira's own secrets; the config's env list adds what it asks for.
func TestStdioEnvKeepsSecretsAway(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin", "HOME=/home/x", "LANG=de_DE.UTF-8", "LC_ALL=C", "HTTPS_PROXY=http://proxy:3128",
		"HA_TOKEN=secret", "MIRA_API_TOKEN=secret", "GEMINI_API_TOKEN=secret", "OPENAI_API_KEY=secret",
	}
	env := mcp.StdioEnv(parent, []string{"MAX_RESULTS=5"})
	joined := strings.Join(env, "\n")
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/x", "LANG=de_DE.UTF-8", "LC_ALL=C", "HTTPS_PROXY=http://proxy:3128", "MAX_RESULTS=5"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "secret") {
		t.Errorf("a secret reached the server:\n%s", joined)
	}
}
