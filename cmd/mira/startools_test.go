package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/startools"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/fakeoauth"
	"github.com/miraassist/mira/internal/ui"
)

// tryApp is an app with one connected skill "dienst" whose tools.star
// prints, makes a request at the fake service and can fail.
func tryApp(t *testing.T) (*app, *fakeoauth.Service, *bytes.Buffer) {
	t.Helper()
	svc := fakeoauth.New("id-1", "geheim")
	t.Cleanup(svc.Close)
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f, err := startools.Load("dienst", []byte(`
def suche(thema):
    """Sucht beim Dienst.

    Args:
      thema (str): Das Thema.
    """
    print("suche nach", thema)
    r = api.get("`+svc.APIURL()+`v2/suche", params = {"q": thema})
    return {"status": r.status}

def kaputt():
    """Geht schief."""
    return {}["fehlt"]
`), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sk := skills.Skill{Name: "dienst", Description: "Ein Dienst.", Starlark: f,
		OAuth: &skills.OAuth{Name: "Dienst", TokenURL: svc.TokenURL(), ClientID: "id-1", ClientSecret: "geheim",
			API: []skills.APIPrefix{{URL: svc.APIURL(), Methods: []string{"GET"}}}}}
	var logs bytes.Buffer
	a := &app{log: slog.New(slog.NewTextHandler(&logs, nil)), skillsOn: []skills.Skill{sk}}
	a.oauth, err = oauth.New(context.Background(), oauth.Options{Store: st, Skills: a.skillsOn, HTTP: svc.Client(), Logger: a.log})
	if err != nil {
		t.Fatal(err)
	}
	access, refresh := svc.Grant()
	if err := a.oauth.Put(context.Background(), "dienst", oauth.Handover{AccessToken: access, RefreshToken: refresh, ExpiresIn: 7200}); err != nil {
		t.Fatal(err)
	}
	skills.StarlarkTools(a.skillsOn, a.oauth, a.log)
	return a, svc, &logs
}

// SL-16 Ausprobieren shows what went over the wire
func TestSL16_AusprobierenShowsWhatWentOverTheWire(t *testing.T) {
	// Given a connected skill with a Starlark tool that prints and makes a request
	a, svc, logs := tryApp(t)
	// When an admin runs it on the page with arguments
	res, err := starlarkTry{a: a}.Try(context.Background(), "admin-1", "dienst", "suche", json.RawMessage(`{"thema":"voice satellit"}`))
	if err != nil || res.Error != "" {
		t.Fatalf("%+v %v", res, err)
	}
	// Then the page gets the answer, the request with method, address, status, time, size and the start of the
	// answer, and the print, in order
	if res.Text != `{"status":200}` || len(res.Events) != 2 {
		t.Fatalf("result %+v", res)
	}
	p, r := res.Events[0], res.Events[1]
	if p.Kind != "print" || p.Message != "suche nach voice satellit" {
		t.Errorf("print %+v", p)
	}
	if r.Kind != "request" || r.Method != "GET" || r.URL != svc.APIURL()+"v2/suche?q=voice+satellit" || r.Status != 200 ||
		r.Size == 0 || r.Start == "" {
		t.Errorf("request %+v", r)
	}
	if !strings.Contains(logs.String(), "starlark tool tried on the page") || !strings.Contains(logs.String(), "admin-1") {
		t.Errorf("log %s", logs)
	}
	// And a tool that fails gives its error with the backtrace
	res, _ = starlarkTry{a: a}.Try(context.Background(), "admin-1", "dienst", "kaputt", nil)
	if !strings.Contains(res.Error, `key "fehlt" not in dict`) || !strings.Contains(res.Backtrace, "tools.star:") {
		t.Errorf("failure %+v", res)
	}
	// A tool that is not there.
	if _, err := (starlarkTry{a: a}).Try(context.Background(), "admin-1", "dienst", "gibtsnicht", nil); !errors.Is(err, ui.ErrNoTool) {
		t.Errorf("unknown tool: %v", err)
	}
}

// SL-17 The page shows a skill's Starlark tools
func TestSL17_ThePageShowsASkillsStarlarkTools(t *testing.T) {
	// Given a switched-on skill with tools.star
	a, _, _ := tryApp(t)
	a.skillsOn[0].Writes = []string{"kaputt"}
	// When the page lists the skills
	var info ui.SkillInfo
	starlarkInfo(a.skillsOn[0], &info)
	// Then the skill's entry has its tools with description, schema and whether they write
	if len(info.Tools) != 2 || info.Tools[0].Name != "kaputt" || !info.Tools[0].Writes || info.Tools[1].Name != "suche" ||
		info.Tools[1].Writes || info.Tools[1].Description != "Sucht beim Dienst." || !strings.Contains(string(info.Tools[1].Schema), `"thema"`) {
		t.Errorf("tools %+v", info.Tools)
	}
}
