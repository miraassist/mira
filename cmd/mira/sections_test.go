package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
)

// The settings sections in the file, what Home Assistant sends in memory
// (docs/spec/bearbeiten.md, Settings sections; step 2).

const sectionsConfig = editConfig + `# the rooms
satellites:
  bad: assist_satellite.bad
notifier:
  cooldown: 30m
`

// sectionsApp is an app running with a mira.yaml that has a notifier and
// a satellite, and the page's config.
func sectionsApp(t *testing.T) (*app, uiConfig, string) {
	t.Helper()
	a, _, path := editApp(t)
	if err := os.WriteFile(path, []byte(sectionsConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a.live = cfg
	// The notifier starts again on a change; it reads the house's structure.
	a.structure = structure.NewCache(structure.Options{Source: a.ha, Subscriber: a.ha, Logger: a.log})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		a.proactiveWG.Wait()
	})
	return a, uiConfig{a: a, runCtx: ctx}, path
}

// stored are the keys Mira keeps in her database that hold a section.
func stored(t *testing.T, a *app) []string {
	t.Helper()
	var out []string
	for _, name := range config.UISections {
		if _, err := a.store.GetKV(context.Background(), "ui_config:"+name); err == nil {
			out = append(out, name)
		}
	}
	return out
}

func TestBE14_ASectionSavedOnThePageGoesIntoTheFileAndHoldsAtOnce(t *testing.T) {
	a, u, path := sectionsApp(t)
	// When the page saves the notifier with another cooldown
	if err := u.SetSections(context.Background(), map[string]json.RawMessage{"notifier": json.RawMessage(`{"cooldown":"10m"}`)}); err != nil {
		t.Fatal(err)
	}
	// Then the file has the new cooldown and the comment
	file := read(t, path)
	if !strings.Contains(file, "cooldown: 10m") || !strings.Contains(file, "# the rooms") || !strings.Contains(file, "# test house") {
		t.Errorf("file:\n%s", file)
	}
	// nothing is stored in the database
	if s := stored(t, a); len(s) > 0 {
		t.Errorf("stored in the database: %v", s)
	}
	// And the running notifier has it without a restart
	if got := a.liveConfig().Notifier.Cooldown; time.Duration(got) != 10*time.Minute {
		t.Errorf("live cooldown %v", got)
	}
	// and no restart is asked for
	if a.edit.changed {
		t.Error("a restart is asked for")
	}
}

func TestBE15_ASectionMiraWouldRefuseIsNotWritten(t *testing.T) {
	a, u, path := sectionsApp(t)
	err := u.SetSections(context.Background(), map[string]json.RawMessage{
		"notifier": json.RawMessage(`{"morning_trigger":{"areas":["keller"]}}`)})
	if err == nil || !strings.Contains(err.Error(), "keller") {
		t.Fatalf("err = %v", err)
	}
	if file := read(t, path); file != sectionsConfig {
		t.Errorf("file changed:\n%s", file)
	}
	if a.liveConfig().Notifier.MorningTrigger.Areas != nil {
		t.Errorf("live changed: %+v", a.liveConfig().Notifier.MorningTrigger)
	}
}

func TestBE16_WhatHomeAssistantSendsIsHeldInMemoryNotWritten(t *testing.T) {
	a, u, path := sectionsApp(t)
	err := u.SetHASections(context.Background(), map[string]json.RawMessage{
		"persons":    json.RawMessage(`{"ben":{"entity":"person.ben"}}`),
		"satellites": json.RawMessage(`{"kueche":"assist_satellite.kueche"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if live := a.liveConfig(); live.Persons["ben"].Entity != "person.ben" || live.Satellites["kueche"] == "" {
		t.Errorf("not running with them: %+v %+v", live.Persons, live.Satellites)
	}
	if file := read(t, path); file != sectionsConfig {
		t.Errorf("file changed:\n%s", file)
	}
	if s := stored(t, a); len(s) > 0 {
		t.Errorf("stored in the database: %v", s)
	}
	if !u.HAManaged() {
		t.Error("not managed")
	}
}

func TestBE17_AfterARestartMiraAsksForTheSettingsAgain(t *testing.T) {
	a, u, _ := sectionsApp(t)
	ctx := context.Background()
	// The exposed list is kept from before the restart.
	if err := a.store.SetKV(ctx, kvExposed, `["light.a"]`); err != nil {
		t.Fatal(err)
	}
	if h := a.exposedHash(ctx); h != "" {
		t.Errorf("fingerprint before the settings came: %q", h)
	}
	if err := u.SetHASections(ctx, map[string]json.RawMessage{"persons": json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if h := a.exposedHash(ctx); len(h) != 64 {
		t.Errorf("fingerprint after: %q", h)
	}
}

func TestBE18_ANotifierSavedWhileHAManagesKeepsTheFilesMorningTrigger(t *testing.T) {
	a, u, path := sectionsApp(t)
	ctx := context.Background()
	// Given the integration sent rooms with a morning trigger
	morning := `"morning_trigger":{"areas":["bad"],"presence":{"bad":["binary_sensor.bad"]}}`
	if err := u.SetHASections(ctx, map[string]json.RawMessage{
		"satellites": json.RawMessage(`{"bad":"assist_satellite.bad"}`),
		"notifier":   json.RawMessage(`{"cooldown":"30m",` + morning + `}`)}); err != nil {
		t.Fatal(err)
	}
	// When the page saves the notifier with another cooldown (the page
	// keeps Home Assistant's morning trigger in what it sends)
	if err := u.SetSections(ctx, map[string]json.RawMessage{"notifier": json.RawMessage(`{"cooldown":"10m",` + morning + `}`)}); err != nil {
		t.Fatal(err)
	}
	// Then the file has the new cooldown and its own morning trigger (none)
	file := read(t, path)
	if !strings.Contains(file, "cooldown: 10m") || strings.Contains(file, "morning_trigger") || strings.Contains(file, "binary_sensor.bad") {
		t.Errorf("file:\n%s", file)
	}
	// And Mira runs with the new cooldown and Home Assistant's trigger
	live := a.liveConfig().Notifier
	if time.Duration(live.Cooldown) != 10*time.Minute || len(live.MorningTrigger.Areas) != 1 {
		t.Errorf("live %+v", live)
	}
}

func TestBE20_OnlyWhatDiffersFromTheDefaultsGoesIntoTheFile(t *testing.T) {
	a, u, path := sectionsApp(t)
	ctx := context.Background()
	// When the page saves the whole notifier with only the cooldown changed
	whole, err := config.Default().SectionJSON("notifier")
	if err != nil {
		t.Fatal(err)
	}
	var n map[string]any
	json.Unmarshal(whole, &n)
	n["cooldown"] = "10m"
	changed, _ := json.Marshal(n)
	if err := u.SetSections(ctx, map[string]json.RawMessage{"notifier": changed}); err != nil {
		t.Fatal(err)
	}
	// Then the file's notifier has only the cooldown
	file := read(t, path)
	if !strings.Contains(file, "notifier:\n  cooldown: 10m\n") || strings.Contains(file, "delivery") || strings.Contains(file, "quiet_hours") {
		t.Errorf("file:\n%s", file)
	}
	// And saved back at its defaults, the file has no notifier
	if err := u.SetSections(ctx, map[string]json.RawMessage{"notifier": whole}); err != nil {
		t.Fatal(err)
	}
	if file := read(t, path); strings.Contains(file, "notifier") {
		t.Errorf("file still has a notifier:\n%s", file)
	}
	if got := a.liveConfig().Notifier.Cooldown; got != config.Default().Notifier.Cooldown {
		t.Errorf("live cooldown %v", got)
	}
}

func TestBE21_ThePersonaGoesUnderPromptTheWebSearchAtTheTop(t *testing.T) {
	a, u, path := sectionsApp(t)
	ctx := context.Background()
	// When the options save a persona and switch the web search off
	if err := u.SetSections(ctx, map[string]json.RawMessage{
		"persona":    json.RawMessage(`"Du duzt alle."`),
		"web_search": json.RawMessage(`{"enabled":false}`)}); err != nil {
		t.Fatal(err)
	}
	// Then the file has the persona under prompt and the web search at the top
	file := read(t, path)
	if !strings.Contains(file, "prompt:\n  persona: Du duzt alle.\n") || !strings.Contains(file, "web_search:\n  enabled: false\n") {
		t.Errorf("file:\n%s", file)
	}
	// And Mira reads the file and runs with both
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prompt.Persona != "Du duzt alle." || cfg.WebSearch.Enabled {
		t.Errorf("file reads %q %+v", cfg.Prompt.Persona, cfg.WebSearch)
	}
	if live := a.liveConfig(); live.Prompt.Persona != "Du duzt alle." || live.WebSearch.Enabled {
		t.Errorf("live %q %+v", live.Prompt.Persona, live.WebSearch)
	}
	// And saved back empty, the file has no prompt
	if err := u.SetSections(ctx, map[string]json.RawMessage{"persona": json.RawMessage(`""`)}); err != nil {
		t.Fatal(err)
	}
	if file := read(t, path); strings.Contains(file, "prompt") {
		t.Errorf("file still has a prompt:\n%s", file)
	}
}
