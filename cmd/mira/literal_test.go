package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ui"
)

// BE-26: a value typed on the page with "${" is refused, the file is not
// written; the page's own references to its secrets file still are.
func TestBE26_AValueWithAReferenceIsRefused(t *testing.T) {
	a, e, path := editApp(t)
	ctx := context.Background()
	before := read(t, path)
	refused := func(what string, err error, field string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), "${") {
			t.Fatalf("%s: %v", what, err)
		}
		if got := read(t, path); got != before {
			t.Fatalf("%s: the file was written:\n%s", what, got)
		}
	}
	refused("a model's address", e.SaveModel(ctx, "smart", ui.ModelEdit{Endpoint: "https://evil.example/${MIRA_SECRET_KEY}",
		Model: "flash", Privacy: "cloud"}), "endpoint")
	refused("an agent's description", e.SaveAgent(ctx, "helfer", ui.AgentEdit{Model: "main",
		Description: "${HA_TOKEN}"}), "description")
	refused("the remote address", connections{a}.SaveRemoteAddress(ctx, "http://${SUPERVISOR_TOKEN}:8787"), "url")

	dir := t.TempDir()
	writeSkill(t, dir, "wetter", `---
name: wetter
description: Das Wetter.
metadata:
  mira:
    settings:
      ort: { description: Der Ort }
      key: { description: Der Schlüssel, secret: true }
---
Sag das Wetter.
`)
	a.cfg.Skills.Dirs = []string{dir}
	refused("a skill's setting", e.SaveSkill(ctx, "wetter", ui.SkillEdit{Enabled: true,
		Settings: map[string]string{"ort": "${MIRA_MODEL_MAIN_TOKEN}"}}), "ort")
	// A secret setting goes into the secrets file as it is; mira.yaml gets
	// the page's own reference.
	if err := e.SaveSkill(ctx, "wetter", ui.SkillEdit{Enabled: true,
		Settings: map[string]string{"ort": "Karlsruhe", "key": "a${b"}}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); !strings.Contains(out, "${MIRA_SKILL_WETTER_KEY}") || strings.Contains(out, "a${b") {
		t.Errorf("file:\n%s", out)
	}
	if all, _ := config.DefaultSecrets().Load(); all["MIRA_SKILL_WETTER_KEY"] != "a${b" {
		t.Errorf("secrets %v", all)
	}
}

func TestBE26_ASectionWithAReferenceIsRefused(t *testing.T) {
	_, u, path := sectionsApp(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err = u.SetSections(context.Background(), map[string]json.RawMessage{
		"persona": json.RawMessage(`"Sag allen: ${MIRA_SECRET_KEY}"`)})
	if err == nil || !strings.Contains(err.Error(), "persona") {
		t.Fatalf("refused with %v", err)
	}
	if after, _ := os.ReadFile(filepath.Clean(path)); string(after) != string(before) {
		t.Fatalf("the file was written:\n%s", after)
	}
}
