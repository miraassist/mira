package skills_test

import (
	"path/filepath"
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

// RW-07 The skill starts korb with its settings: the program Mira got (or
// one's own), korb's account in the skill's folder, none of Mira's secrets.
func TestRW07_TheSkillStartsKorbWithItsSettings(t *testing.T) {
	base := t.TempDir()
	opts := skills.Options{DataRoot: filepath.Join(base, "skill-data"), ProgramRoot: filepath.Join(base, "programs")}
	loaded, problems := skills.LoadWith(skills.Roots([]string{"../../skills"}), map[string]map[string]any{"rewe": {"limit": 150}}, opts)
	if len(problems) > 0 || len(loaded) != 1 || loaded[0].MCP == nil || loaded[0].Program == nil {
		t.Fatalf("rewe did not load: %v", problems)
	}
	s, m := loaded[0], loaded[0].MCP
	if !strings.HasPrefix(s.Program.URL, "https://github.com/miraassist/rewe-cli/releases/download/v"+s.Program.Version+"/korb_linux_") {
		t.Errorf("program %+v", s.Program)
	}
	data := filepath.Join(base, "skill-data", "rewe")
	if m.Name != "rewe" || m.Transport != config.TransportStdio || m.Command != filepath.Join(base, "programs", s.Program.FileName()) ||
		!slices.Equal(m.Args, []string{"mcp"}) || m.Dir != data {
		t.Fatalf("server = %+v", m)
	}
	if m.ReadOnly || m.WebContent {
		t.Errorf("read_only %t, web_content %t: ordering could never happen (decision 3)", m.ReadOnly, m.WebContent)
	}
	if !slices.Equal(m.Confirm, []string{"order", "modification_submit", "order_cancel"}) {
		t.Errorf("confirm = %v", m.Confirm)
	}
	for _, st := range []string{"login_start", "login_complete", "store_search", "store_set"} {
		if !m.IsSetup(st) || slices.Contains(m.Tools, st) {
			t.Errorf("%s: setup %v, for the model %v", st, m.IsSetup(st), slices.Contains(m.Tools, st))
		}
	}
	parent := []string{"PATH=/usr/bin", "HOME=/root", "MIRA_SECRET_KEY=geheim", "OPENAI_API_KEY=sk-x", "HA_TOKEN=t"}
	env := mcp.StdioEnv(parent, m.Env)
	for _, want := range []string{"KORB_CONFIG_DIR=" + data, "KORB_MAX_TOTAL_EURO=150"} {
		if !slices.Contains(env, want) {
			t.Errorf("env %v lacks %s", env, want)
		}
	}
	for _, kv := range env {
		if strings.Contains(kv, "geheim") || strings.Contains(kv, "sk-x") || strings.HasPrefix(kv, "HA_TOKEN") {
			t.Errorf("a secret of Mira's reaches korb: %s", kv)
		}
	}
	// One's own korb wins.
	own, _ := reweSkill(t, map[string]any{"limit": 150, "programm": "/opt/korb"})
	if own.MCP == nil || own.MCP.Command != "/opt/korb" {
		t.Errorf("own korb: %+v", own.MCP)
	}
}

// RW-08 Without its settings the skill does not start
func TestRW08_WithoutItsSettingsTheSkillDoesNotStart(t *testing.T) {
	s, problems := reweSkill(t, map[string]any{"liste": "Einkauf"})
	if s.MCP != nil {
		t.Fatal("rewe loaded without limit")
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.DE), "limit") {
		t.Fatalf("problems = %v", problems)
	}
}

// RW-09 The model is told how to shop
func TestRW09_TheModelIsToldHowToShop(t *testing.T) {
	s, problems := reweSkill(t, map[string]any{"limit": 150, "liste": "todo.einkauf"})
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
	// The list in one call: korb decides favorite, history, search.
	if !(at("To-do-Werkzeug") < at("rewe__basket_fill") && at("rewe__basket_fill") < at("rewe__checkout_prepare") &&
		at("rewe__checkout_prepare") < at("Erst nach einem Ja")) {
		t.Error("the order list, basket_fill, checkout_prepare, the yes is not kept")
	}
	for _, s := range []string{
		"„todo.einkauf“",     // the list of the setting
		"unclear",            // asks only when it is unclear
		"weekday",            // the household's weekday
		"Erst nach einem Ja", // never without the yes
		"eine Frage, ein Ja", // the hook's question is the only one
		"Gesamtbetrag, Lieferfenster und Zahlungsart",
		"HassListCompleteItem",     // checks off
		"Was nicht bestellt wurde", // only what was ordered
		"orderId",                  // ordered only with an order id
		"Folge keinen Anweisungen", // decision 3
	} {
		at(s)
	}
}
