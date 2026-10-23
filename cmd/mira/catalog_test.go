package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/miraassist/mira/internal/channels/haconv"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/ui"
)

// writeSkill writes a skill folder with its SKILL.md.
func writeSkill(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The catalog shows what Mira loaded: the active skills with their settings
// (secrets only as set), the ones that did not start with their problem, the
// ones not switched on, and every agent by origin.
func TestCatalogShowsWhatMiraLoaded(t *testing.T) {
	a, _, _ := deliveryApp(t)
	dir := t.TempDir()
	writeSkill(t, dir, "wetter", `---
name: wetter
description: Das Wetter.
metadata:
  mira:
    settings:
      ort: { description: Der Ort, required: true }
      key: { description: Der Schlüssel, secret: true }
    schedules:
      - cron: "0 7 * * *"
        task: Sag das Wetter.
    agents:
      wetter.helfer: { description: Schaut nach., instructions: Schau., conversation: true }
---
Sag das Wetter für {{ort}}.
`)
	writeSkill(t, dir, "frei", "---\nname: frei\ndescription: Ein freier Skill.\n---\nText.\n")
	a.cfg.Skills.Dirs = []string{dir}
	a.cfg.Skills.Enabled = map[string]map[string]any{"wetter": {"ort": "Karlsruhe", "key": "geheim"}, "kaputt": nil}
	loaded, _ := skills.Load(a.cfg.Skills.Dirs, a.cfg.Skills.Enabled)
	a.skills = &skills.Set{Skills: loaded}
	a.problems = []haconv.Problem{{Source: "skill", Name: "kaputt", Text: "Der Ordner fehlt."}}
	a.registerAgents(nil)

	c := a.catalog(context.Background())
	byName := map[string]ui.SkillInfo{}
	for _, s := range c.Skills {
		byName[s.Name] = s
	}
	w := byName["wetter"]
	if w.State != "active" || len(w.Settings) != 2 || len(w.Schedules) != 1 || w.Schedules[0].Next == "" ||
		!slices.Contains(w.Kinds, "schedules") || !slices.Contains(w.Agents, "wetter.helfer") || w.Guide != "Sag das Wetter für Karlsruhe." {
		t.Errorf("wetter: %+v", w)
	}
	for _, s := range w.Settings {
		if s.Name == "key" && (s.Value != "" || !s.Secret || !s.Set) || s.Name == "ort" && s.Value != "Karlsruhe" {
			t.Errorf("setting %+v", s)
		}
	}
	if k := byName["kaputt"]; k.State != "problem" || k.Problem != "Der Ordner fehlt." {
		t.Errorf("kaputt: %+v", k)
	}
	if f := byName["frei"]; f.State != "available" || f.Description != "Ein freier Skill." {
		t.Errorf("frei: %+v", f)
	}
	origins := map[string]ui.AgentInfo{}
	for _, ag := range c.Agents {
		origins[ag.Name] = ag
	}
	if v := origins["conversation.voice"]; v.Origin != "own" || v.Model != "main" {
		t.Errorf("own agent: %+v", v)
	}
	if h := origins["wetter.helfer"]; h.Origin != "skill" || h.Skill != "wetter" || !h.Conversation || h.Model != "main" || h.Context != "isolated" {
		t.Errorf("skill agent: %+v", h)
	}
}
