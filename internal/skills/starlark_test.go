package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/tools"
)

// Skills with tools in Starlark (docs/spec/api-tools.md).

const dienstHead = `---
name: dienst
description: Ein Dienst.
metadata:
  mira:
%s
---
Nimm die Werkzeuge.
`

const dienstOAuth = `    oauth:
      authorize_url: https://dienst.example/auth
      token_url: https://dienst.example/token
      api:
        - url: https://dienst.example/api/
          methods: [GET, POST]`

const dienstTools = `
def suche(thema):
    """Sucht.

    Args:
      thema (str): Das Thema.
    """
    return api.get("https://dienst.example/api/suche", params = {"q": thema}).json

def post(text):
    """Postet.

    Args:
      text (str): Der Text.
    """
    return api.post("https://dienst.example/api/posts", body = {"text": text}).status
`

// dienst writes a skill folder with SKILL.md (its mira section) and, when
// given, tools.star, and loads it.
func dienst(t *testing.T, mira, star string) ([]skills.Skill, []skills.Problem) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dienst"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := strings.Replace(dienstHead, "%s", mira, 1)
	if err := os.WriteFile(filepath.Join(dir, "dienst", "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if star != "" {
		if err := os.WriteFile(filepath.Join(dir, "dienst", "tools.star"), []byte(star), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return skills.Load([]string{dir}, map[string]map[string]any{"dienst": {}})
}

func problemText(ps []skills.Problem) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Text(i18n.EN))
	}
	return strings.Join(out, " | ")
}

// SL-05 An api section and tools.star go together
func TestSL05_AnAPISectionAndToolsStarGoTogether(t *testing.T) {
	// Given a skill with an api section and no tools.star
	loaded, ps := dienst(t, dienstOAuth, "")
	// When the skill loads
	// Then the skill does not start and its problem says which is missing
	if len(loaded) != 0 || !strings.Contains(problemText(ps), "has an api section but no tools.star") {
		t.Errorf("%d %s", len(loaded), problemText(ps))
	}
	// An api section under mira beside a login's is a problem
	// (docs/spec/api-tools.md, decision 8).
	loaded, ps = dienst(t, "    api:\n      - url: https://dienst.example/api/\n"+dienstOAuth, dienstTools)
	if len(loaded) != 0 || !strings.Contains(problemText(ps), "a skill with a login names it only under oauth") {
		t.Errorf("%d %s", len(loaded), problemText(ps))
	}
	// Or a tools.star and no api section
	loaded, ps = dienst(t, "    privacy: local", dienstTools)
	if len(loaded) != 0 || !strings.Contains(problemText(ps), "has a tools.star but no api section") {
		t.Errorf("%d %s", len(loaded), problemText(ps))
	}
	// A tools.star that does not load names it.
	loaded, ps = dienst(t, dienstOAuth, "def suche(:\n")
	if len(loaded) != 0 || !strings.Contains(problemText(ps), "tools.star does not load: tools.star:1:") {
		t.Errorf("%d %s", len(loaded), problemText(ps))
	}
	// Both together load.
	loaded, ps = dienst(t, dienstOAuth, dienstTools)
	if len(loaded) != 1 || loaded[0].Starlark == nil || len(ps) != 0 {
		t.Errorf("%d %s", len(loaded), problemText(ps))
	}
}

// SL-06 Names under writes and confirm are tools
func TestSL06_NamesUnderWritesAndConfirmAreTools(t *testing.T) {
	// Given a skill whose tools section names under writes a function tools.star does not have
	loaded, ps := dienst(t, dienstOAuth+"\n    tools:\n      writes: [posten]", dienstTools)
	// When the skill loads
	// Then the skill does not start and its problem names it and the tools there are
	if len(loaded) != 0 || !strings.Contains(problemText(ps), `"posten" under tools is no tool of tools.star (it has: post, suche)`) {
		t.Errorf("%d %s", len(loaded), problemText(ps))
	}
	loaded, ps = dienst(t, dienstOAuth+"\n    tools:\n      writes: [post]\n      confirm: [post]", dienstTools)
	if len(loaded) != 1 || !loaded[0].WritesTool("post") || !loaded[0].ConfirmsTool("post") || loaded[0].WritesTool("suche") {
		t.Errorf("%d %s", len(loaded), problemText(ps))
	}
}

// connector is a connection that is there or not; it answers nothing.
type connector struct{ on bool }

func (c connector) Request(context.Context, string, string, string, []byte) (int, []byte, error) {
	return 200, []byte(`{}`), nil
}
func (c connector) Connected(string) bool { return c.on }

func byName(list []tools.InternalTool) map[string]tools.InternalTool {
	out := map[string]tools.InternalTool{}
	for _, tl := range list {
		out[tl.Name] = tl
	}
	return out
}

// SL-12 Only the tools under writes write (the part of the tools Mira
// offers: their flags)
func TestSL12_OnlyTheToolsUnderWritesWriteFlags(t *testing.T) {
	// Given a skill whose api allows POST and whose tools section names post under writes and confirm
	loaded, ps := dienst(t, dienstOAuth+"\n    tools:\n      writes: [post]\n      confirm: [post]", dienstTools)
	if len(ps) > 0 {
		t.Fatal(problemText(ps))
	}
	got := byName(skills.StarlarkTools(loaded, connector{on: true}, nil))
	// Then post is no read-only tool, asks first after third-party text and is marked to always ask
	post, suche := got["dienst__post"], got["dienst__suche"]
	if post.ReadOnly || !post.AskAfterWeb || !post.Confirm {
		t.Errorf("post %+v", post)
	}
	if !suche.ReadOnly || suche.AskAfterWeb || suche.Confirm {
		t.Errorf("suche %+v", suche)
	}
}

// SL-14 The tools come with their skill and its connection
func TestSL14_TheToolsComeWithTheirSkillAndItsConnection(t *testing.T) {
	// Given a skill with tools.star
	loaded, ps := dienst(t, dienstOAuth, dienstTools)
	if len(ps) > 0 {
		t.Fatal(problemText(ps))
	}
	// When Mira offers its tools
	got := byName(skills.StarlarkTools(loaded, connector{on: true}, nil))
	// Then each is the tool <skill>__<function> of its skill, given to its schedules and agents too, its answer
	// third-party text
	s, ok := got["dienst__suche"]
	if !ok || s.Skill != "dienst" || !s.Background || !s.WebContent || s.Description != "Sucht." ||
		!strings.Contains(s.Schema, `"thema"`) || !s.Enabled() {
		t.Fatalf("suche %+v", s)
	}
	out, err := s.Run(context.Background(), tools.Invocation{Name: s.Name, Args: []byte(`{"thema":"x"}`)})
	if err != nil || out != "{}" {
		t.Errorf("%q %v", out, err)
	}
	// And without a connection it is not offered
	if byName(skills.StarlarkTools(loaded, connector{on: false}, nil))["dienst__suche"].Enabled() {
		t.Error("offered without a connection")
	}
}

// SL-15 The web API tools are gone
func TestSL15_TheWebAPIToolsAreGone(t *testing.T) {
	// Given a skill with an api section and tools.star
	loaded, _ := dienst(t, dienstOAuth, dienstTools)
	// When Mira offers its tools
	got := byName(append(skills.StarlarkTools(loaded, connector{on: true}, nil),
		(&skills.Set{Lang: i18n.DE, Skills: loaded}).Tools()...))
	// Then there is no <skill>__api_get and no <skill>__api_write
	for n := range got {
		if strings.Contains(n, "api_get") || strings.Contains(n, "api_write") {
			t.Errorf("tool %s", n)
		}
	}
	if len(got) < 2 {
		t.Errorf("tools %v", got)
	}
}
