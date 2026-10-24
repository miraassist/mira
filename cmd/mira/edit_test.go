package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/setup"
	"github.com/miraassist/mira/internal/ui"
)

const editConfig = `# test house
models:
  main:
    endpoint: http://localhost:1234/v1
    model: qwen
    privacy: local
agents:
  helfer:
    description: Hilft.
    instructions: Hilf.
    model: main
  chef:
    description: Ruft den Helfer.
    instructions: Ruf ihn.
    context: background
    tools: [helfer]
`

// editApp is an app with a mira.yaml to edit and its own data folder.
func editApp(t *testing.T) (*app, *editor, string) {
	t.Helper()
	a, _, _ := deliveryApp(t)
	dir := t.TempDir()
	t.Setenv("MIRA_DATA", dir)
	t.Setenv("MIRA_SECRET_KEY", "")
	path := filepath.Join(dir, "mira.yaml")
	if err := os.WriteFile(path, []byte(editConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	a.configPath = path
	return a, a.editor().(*editor), path
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestBE04_ANewModelGoesIntoTheFileItsTokenIntoTheSecrets(t *testing.T) {
	_, e, path := editApp(t)
	err := e.SaveModel(context.Background(), "smart", ui.ModelEdit{Endpoint: "https://example.com/v1", Model: "flash",
		Privacy: "cloud", Token: "sk-geheim"})
	if err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "smart:") || !strings.Contains(out, "${MIRA_MODEL_SMART_TOKEN}") || strings.Contains(out, "sk-geheim") ||
		!strings.Contains(out, "# test house") {
		t.Errorf("file:\n%s", out)
	}
	all, _ := config.DefaultSecrets().Load()
	if all["MIRA_MODEL_SMART_TOKEN"] != "sk-geheim" {
		t.Errorf("secrets %v", all)
	}
	st := e.State(context.Background())
	if !st.RestartNeeded || !st.Models["smart"].TokenSet || st.Models["smart"].Token != "" {
		t.Errorf("state %+v", st.Models["smart"])
	}
}

func TestBE05_RenamingAModelChangesWhoNamesIt(t *testing.T) {
	_, e, path := editApp(t)
	ctx := context.Background()
	if err := e.SaveModel(ctx, "main", ui.ModelEdit{Name: "haupt", Endpoint: "x", Model: "y", Privacy: "local"}); err == nil {
		t.Error("main was renamed")
	}
	if err := e.SaveModel(ctx, "smart", ui.ModelEdit{Endpoint: "https://e/v1", Model: "f", Privacy: "cloud", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveAgent(ctx, "helfer", ui.AgentEdit{Description: "Hilft.", Instructions: "Hilf.", Model: "smart"}); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveModel(ctx, "smart", ui.ModelEdit{Name: "schnell", Endpoint: "https://e/v1", Model: "f", Privacy: "cloud"}); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if strings.Contains(out, "smart") || !strings.Contains(out, "model: schnell") || !strings.Contains(out, "${MIRA_MODEL_SCHNELL_TOKEN}") {
		t.Errorf("file:\n%s", out)
	}
}

func TestBE06_AModelInUseIsNotDeleted(t *testing.T) {
	_, e, path := editApp(t)
	ctx := context.Background()
	e.SaveModel(ctx, "smart", ui.ModelEdit{Endpoint: "https://e/v1", Model: "f", Privacy: "cloud"})
	e.SaveAgent(ctx, "helfer", ui.AgentEdit{Description: "Hilft.", Instructions: "Hilf.", Model: "smart"})
	if err := e.DeleteModel(ctx, "smart"); err == nil || !strings.Contains(err.Error(), "helfer") {
		t.Errorf("err = %v", err)
	}
	if err := e.DeleteModel(ctx, "main"); err == nil {
		t.Error("main was deleted")
	}
	e.SaveAgent(ctx, "helfer", ui.AgentEdit{Description: "Hilft.", Instructions: "Hilf."})
	if err := e.DeleteModel(ctx, "smart"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "smart") {
		t.Error("still in the file")
	}
}

func TestBE07_AnAgentIsCheckedBeforeItIsWrittenAndNotDeletedWhileUsed(t *testing.T) {
	_, e, path := editApp(t)
	ctx := context.Background()
	before := read(t, path)
	// isolated with tools: what Mira would refuse at start is not written
	err := e.SaveAgent(ctx, "neu", ui.AgentEdit{Description: "Neu.", Instructions: "Tu was.", Tools: []string{"web_page"}})
	if err == nil || read(t, path) != before {
		t.Errorf("err = %v", err)
	}
	if err := e.SaveAgent(ctx, "neu", ui.AgentEdit{Description: "Neu.", Instructions: "Tu was.", Context: "background",
		Tools: []string{"web_page"}, Rights: []string{"web"}, MaxRounds: 12, Input: "type: object\nproperties: { url: { type: string } }\n"}); err != nil {
		t.Fatal(err)
	}
	if st := e.State(ctx).Agents["neu"]; st.MaxRounds != 12 || st.Context != "background" || !strings.Contains(st.Input, "url") {
		t.Errorf("state %+v", st)
	}
	if err := e.DeleteAgent(ctx, "helfer"); err == nil || !strings.Contains(err.Error(), "chef") {
		t.Errorf("err = %v", err)
	}
	if err := e.DeleteAgent(ctx, "delegate"); err == nil {
		t.Error("an own agent was deleted")
	}
	if err := e.SaveAgent(ctx, "delegate", ui.AgentEdit{Model: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteAgent(ctx, "neu"); err != nil {
		t.Fatal(err)
	}
}

func TestBE08_SkillSettingsAndAnOwnSkillsInstructions(t *testing.T) {
	a, e, path := editApp(t)
	ctx := context.Background()
	dir := t.TempDir()
	writeSkill(t, dir, "wetter", `---
name: wetter
description: Das Wetter.
metadata:
  mira:
    settings:
      ort: { description: Der Ort, required: true }
      key: { description: Der Schlüssel, secret: true }
---
Sag das Wetter für {{ort}}.
`)
	a.cfg.Skills.Dirs = []string{dir}
	err := e.SaveSkill(ctx, "wetter", ui.SkillEdit{Enabled: true, Settings: map[string]string{"ort": "Karlsruhe", "key": "abc"},
		Parts: []string{"guide"}, Guide: "Sag das Wetter für {{ort}}, kurz.\n"})
	if err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "ort: Karlsruhe") || !strings.Contains(out, "${MIRA_SKILL_WETTER_KEY}") || strings.Contains(out, "abc") {
		t.Errorf("file:\n%s", out)
	}
	if md := read(t, filepath.Join(dir, "wetter", "SKILL.md")); !strings.Contains(md, "kurz.") || !strings.Contains(md, "name: wetter") {
		t.Errorf("SKILL.md:\n%s", md)
	}
	s := e.State(ctx).Skills["wetter"]
	if !s.Enabled || !s.Own || s.Settings["ort"] != "Karlsruhe" || s.Settings["key"] != "" {
		t.Errorf("state %+v", s)
	}
	for _, d := range s.SettingDefs {
		if d.Name == "key" && !d.SecretSet {
			t.Errorf("secret not set: %+v", d)
		}
	}
	// a broken description is not written
	if err := e.SaveSkill(ctx, "wetter", ui.SkillEdit{Enabled: true, Parts: []string{"description"}}); err == nil {
		t.Error("an empty description was written")
	}
	// switching off keeps the settings; switching on again brings them back
	if err := e.SaveSkill(ctx, "wetter", ui.SkillEdit{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); !strings.Contains(out, "enabled: false") || !strings.Contains(out, "ort: Karlsruhe") {
		t.Errorf("file:\n%s", out)
	}
	if s := e.State(ctx).Skills["wetter"]; s.Enabled || s.Settings["ort"] != "Karlsruhe" {
		t.Errorf("state %+v", s)
	}
	if err := e.SaveSkill(ctx, "wetter", ui.SkillEdit{Enabled: true, Settings: map[string]string{"ort": "Karlsruhe"}}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); strings.Contains(out, "enabled") || !strings.Contains(out, "${MIRA_SKILL_WETTER_KEY}") {
		t.Errorf("file:\n%s", out)
	}
}

func TestBE09_MirasOwnSkillCanBeSwitchedOff(t *testing.T) {
	a, e, path := editApp(t)
	a.cfg.Skills.Dirs = []string{"../../skills"}
	ctx := context.Background()
	if err := e.SaveSkill(ctx, "kochen", ui.SkillEdit{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); !strings.Contains(out, "kochen:") || !strings.Contains(out, "enabled: false") {
		t.Errorf("file:\n%s", out)
	}
	if e.State(ctx).Skills["kochen"].Enabled {
		t.Error("still on")
	}
	if err := e.SaveSkill(ctx, "kochen", ui.SkillEdit{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "kochen") {
		t.Error("an own skill that is on needs no line")
	}
}

// MO-02 on the page: a further model beside the integration's main, with
// no main in the file.
func TestBE11_AModelBesideTheIntegrationsMain(t *testing.T) {
	a, e, path := editApp(t)
	ctx := context.Background()
	os.WriteFile(path, []byte("# only agents here\nagents:\n  delegate: { model: main }\n"), 0o644)
	// without the integration's model there is no main anywhere
	if err := e.SaveModel(ctx, "smart", ui.ModelEdit{Endpoint: "https://e/v1", Model: "f", Privacy: "cloud"}); err == nil {
		t.Error("a model was added with no main anywhere")
	}
	if err := setup.Save(ctx, a.store, "http://ha", "token", &setup.LLM{BaseURL: "https://api/v1", Model: "m", Kind: setup.KindCloud}); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveModel(ctx, "smart", ui.ModelEdit{Endpoint: "https://e/v1", Model: "f", Privacy: "cloud"}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); !strings.Contains(out, "smart:") || strings.Contains(out, "main:") {
		t.Errorf("file:\n%s", out)
	}
}
