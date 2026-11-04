package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/testsupport/fakeoauth"
)

// CO-07 A refused refresh token makes the connection expired (the repair
// in Home Assistant; the connection itself: internal/oauth).
func TestCO07_AnExpiredConnectionIsARepair(t *testing.T) {
	svc := fakeoauth.New("id-1", "geheim")
	t.Cleanup(svc.Close)
	a, _, _ := editApp(t)
	a.lang = i18n.DE
	a.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := config.MCPServer{Name: "dienst", Skill: "dienst", Transport: config.TransportHTTP, URL: svc.URL() + "/mcp", OAuth: true}
	sk := skills.Skill{Name: "dienst", MCP: &srv, OAuth: &skills.OAuth{Name: "Dienst", TokenURL: svc.TokenURL(),
		ClientID: "id-1", ClientSecret: "geheim"}}
	var err error
	a.oauth, err = oauth.New(context.Background(), oauth.Options{Store: a.store, Skills: []skills.Skill{sk}, HTTP: svc.Client(), Logger: a.log})
	if err != nil {
		t.Fatal(err)
	}
	a.mcp = mcp.New(mcp.Options{Servers: []config.MCPServer{srv}, Connections: a.oauth, Logger: a.log})
	a.mcp.Start(context.Background())
	defer a.mcp.Close()
	// Not connected is no problem: the server is not tried.
	if p := a.allProblems(); len(p) != 0 {
		t.Fatalf("not connected: %+v", p)
	}
	// Given a connection whose refresh token the service refuses
	_, refresh := svc.Grant()
	if err := a.oauth.Put(context.Background(), "dienst", oauth.Handover{AccessToken: "abgelaufen", RefreshToken: refresh, ExpiresIn: 1}); err != nil {
		t.Fatal(err)
	}
	svc.Revoke()
	// When the token is renewed
	if _, err := a.oauth.Token(context.Background(), "dienst"); err != oauth.ErrExpired {
		t.Fatalf("token: %v", err)
	}
	// Then Home Assistant shows a repair, and only that one
	p := a.allProblems()
	if len(p) != 1 || p[0].Source != "skill" || p[0].Name != "dienst" || !strings.Contains(p[0].Text, "abgelaufen") ||
		!strings.Contains(p[0].Text, "Verbindungen") {
		t.Errorf("problems %+v", p)
	}
}

// Client id and secret entered on the Connections page go into mira.yaml
// and the secrets file, and hold at once without a restart.
func TestConnectionsSaveClient(t *testing.T) {
	a, e, path := editApp(t)
	sk := skills.Skill{Name: "dienst", OAuth: &skills.OAuth{Name: "Dienst", TokenURL: "https://dienst.example/token"}}
	var err error
	a.oauth, err = oauth.New(context.Background(), oauth.Options{Store: a.store, Skills: []skills.Skill{sk}})
	if err != nil {
		t.Fatal(err)
	}
	c := connections{a: a}
	if got := c.List(context.Background()); len(got) != 1 || got[0].State != oauth.StateNone || got[0].SecretSet ||
		got[0].Redirect != "https://my.home-assistant.io/redirect/oauth" {
		t.Fatalf("list %+v", got)
	}
	if err := c.SaveClient(context.Background(), "dienst", "id-1", "sehr-geheim"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "client_id: id-1") || !strings.Contains(out, "${MIRA_SKILL_DIENST_CLIENT_SECRET}") || strings.Contains(out, "sehr-geheim") {
		t.Errorf("file:\n%s", out)
	}
	if got := c.List(context.Background()); got[0].ClientID != "id-1" || !got[0].SecretSet {
		t.Errorf("list %+v", got)
	}
	if l, err := a.oauth.Login("dienst"); err != nil || l.ClientSecret != "sehr-geheim" {
		t.Errorf("login %+v %v", l, err)
	}
	if e.State(context.Background()).RestartNeeded {
		t.Error("a restart is asked for")
	}
	// An empty secret keeps the one there is.
	if err := c.SaveClient(context.Background(), "dienst", "id-2", ""); err != nil {
		t.Fatal(err)
	}
	if l, _ := a.oauth.Login("dienst"); l.ClientID != "id-2" || l.ClientSecret != "sehr-geheim" {
		t.Errorf("login %+v", l)
	}
}
