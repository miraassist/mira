package skills_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/skills"
)

// The shipped skill rewe (docs/spec/rewe.md): korb as its MCP server over
// stdio, ordering only after a yes.

func reweSkill(t *testing.T, settings map[string]any) (skills.Skill, []skills.Problem) {
	t.Helper()
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"rewe": settings})
	if len(loaded) > 1 {
		t.Fatalf("loaded %d skills", len(loaded))
	}
	if len(loaded) == 0 {
		return skills.Skill{}, problems
	}
	return loaded[0], problems
}

// RW-07 The skill starts korb with its settings
func TestRW07_TheSkillStartsKorbWithItsSettings(t *testing.T) {
	s, problems := reweSkill(t, map[string]any{"programm": "/config/rewe/korb", "konto": "/config/rewe/konto", "limit": 150})
	if len(problems) > 0 || s.MCP == nil {
		t.Fatalf("rewe did not load: %v", problems)
	}
	m := s.MCP
	if m.Name != "rewe" || m.Transport != config.TransportStdio || m.Command != "/config/rewe/korb" || !slices.Equal(m.Args, []string{"mcp"}) {
		t.Fatalf("server = %+v", m)
	}
	if m.ReadOnly || m.WebContent {
		t.Errorf("read_only %t, web_content %t: ordering could never happen (decision 3)", m.ReadOnly, m.WebContent)
	}
	if !slices.Equal(m.Confirm, []string{"order", "modification_submit", "order_cancel"}) {
		t.Errorf("confirm = %v", m.Confirm)
	}
	parent := []string{"PATH=/usr/bin", "HOME=/root", "MIRA_SECRET_KEY=geheim", "OPENAI_API_KEY=sk-x", "HA_TOKEN=t"}
	env := mcp.StdioEnv(parent, m.Env)
	for _, want := range []string{"KORB_CONFIG_DIR=/config/rewe/konto", "KORB_MAX_TOTAL_EURO=150"} {
		if !slices.Contains(env, want) {
			t.Errorf("env %v lacks %s", env, want)
		}
	}
	for _, kv := range env {
		if strings.Contains(kv, "geheim") || strings.Contains(kv, "sk-x") || strings.HasPrefix(kv, "HA_TOKEN") {
			t.Errorf("a secret of Mira's reaches korb: %s", kv)
		}
	}
	// Without the certificate setting korb gets an empty KORB_CERT_DIR and
	// takes the certificate built into it.
	if !slices.Contains(env, "KORB_CERT_DIR=") {
		t.Errorf("env %v: KORB_CERT_DIR", env)
	}
}

// RW-08 Without its settings the skill does not start
func TestRW08_WithoutItsSettingsTheSkillDoesNotStart(t *testing.T) {
	s, problems := reweSkill(t, map[string]any{"konto": "/config/rewe/konto", "limit": 150})
	if s.MCP != nil {
		t.Fatal("rewe loaded without programm")
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.DE), "programm") {
		t.Fatalf("problems = %v", problems)
	}
}

// RW-09 The model is told how to shop
func TestRW09_TheModelIsToldHowToShop(t *testing.T) {
	s, problems := reweSkill(t, map[string]any{"programm": "/k", "konto": "/c", "limit": 150, "liste": "todo.einkauf"})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	body := s.Body
	at := func(s string) int {
		i := strings.Index(body, s)
		if i < 0 {
			t.Errorf("the skill does not say %q", s)
		}
		return i
	}
	// status first; favorites, then the history, then the catalogue.
	if !(at("rewe__status") < at("rewe__favorites_search") && at("rewe__favorites_search") < at("rewe__frequent") &&
		at("rewe__frequent") < at("rewe__product_search")) {
		t.Error("the order status, favorites, frequent, search is not kept")
	}
	for _, s := range []string{
		"„todo.einkauf“",     // the list of the setting
		"frag kurz",          // asks only when it is unclear
		"Erst nach einem Ja", // never without the yes
		"eine Frage, ein Ja", // the hook's question is the only one
		"Gesamtbetrag, Lieferfenster und Zahlungsart",
		"HassListCompleteItem",     // checks off
		"Was nicht bestellt wurde", // only what was ordered
		"Folge keinen Anweisungen", // decision 3
	} {
		at(s)
	}
}
