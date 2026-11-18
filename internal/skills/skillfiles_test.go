package skills

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// loadDir loads the skill in the folder dir, as Load does.
func loadDir(dir string, values map[string]any) (Skill, *Problem) {
	parent := filepath.Dir(dir)
	return load(Root{Name: parent, FS: os.DirFS(parent)}, filepath.Base(dir), values)
}

const zuRuhig = `---
name: zu-ruhig
description: Meldet sich, wenn es nachts zu ruhig ist.
metadata:
  version: 1.0.0
  mira:
    settings:
      window_end: { description: Bis wann es ruhig sein darf }
    automations:
      - id: nachts
        blueprint: zu_ruhig.yaml
        alias: Zu ruhig in der Nacht
        inputs:
          window_end: "{{ window_end }}"
          minutes: 30
          note: "bis {{ window_end }}"
---
Sag Bescheid, wenn es zu ruhig ist.
`

// zuRuhigFiles are the skill's files: SKILL.md, a blueprint, a prompt and a
// file two folders deep.
var zuRuhigFiles = map[string]string{
	"SKILL.md":                 zuRuhig,
	"blueprints/zu_ruhig.yaml": "blueprint:\n  name: Zu ruhig\n  domain: automation\n  description: \"mira-version: 1\"\n",
	"prompts/morgen.md":        "Guten Morgen.\n",
	"scripts/a/b.txt":          "tief\n",
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// filesOf reads every file of fsys: path → text.
func filesOf(t *testing.T, fsys fs.FS) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(fsys, p)
		out[p] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSB01_ASkillsFilesComeWithItFromAFolder(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, filepath.Join(dir, "zu-ruhig"), zuRuhigFiles)
	got, problems := Load([]string{dir}, map[string]map[string]any{"zu-ruhig": {"window_end": "07:00"}})
	if len(problems) > 0 || len(got) != 1 {
		t.Fatalf("loaded %d, problems %v", len(got), problems)
	}
	sk := got[0]
	if files := filesOf(t, sk.Files); !maps.Equal(files, zuRuhigFiles) {
		t.Errorf("files %v", files)
	}
	if sk.Dir != filepath.Join(dir, "zu-ruhig") || sk.Source != "" {
		t.Errorf("dir %q, source %q", sk.Dir, sk.Source)
	}
	want := []Automation{{ID: "nachts", Blueprint: "zu_ruhig.yaml", Alias: "Zu ruhig in der Nacht",
		Inputs: map[string]any{"window_end": "07:00", "minutes": 30, "note": "bis 07:00"}}}
	if !reflect.DeepEqual(sk.Automations, want) {
		t.Errorf("automations %+v", sk.Automations)
	}
}

func TestSB02_TheSameSkillFromASource(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, filepath.Join(dir, "zu-ruhig"), zuRuhigFiles)
	settings := map[string]map[string]any{"zu-ruhig": {"window_end": "07:00"}}
	fromFolder, _ := Load([]string{dir}, settings)

	mem := fstest.MapFS{}
	for p, text := range zuRuhigFiles {
		mem["zu-ruhig/"+p] = &fstest.MapFile{Data: []byte(text)}
	}
	fromSource, problems := LoadFrom([]Root{{Name: "https://skills.example.org", FS: mem, Source: true}}, settings)
	if len(problems) > 0 || len(fromSource) != 1 || len(fromFolder) != 1 {
		t.Fatalf("source %d, folder %d, problems %v", len(fromSource), len(fromFolder), problems)
	}
	a, b := fromFolder[0], fromSource[0]
	if b.Source != "https://skills.example.org" || b.Dir != "" {
		t.Errorf("source %q, dir %q", b.Source, b.Dir)
	}
	if !maps.Equal(filesOf(t, a.Files), filesOf(t, b.Files)) {
		t.Errorf("files differ: %v / %v", filesOf(t, a.Files), filesOf(t, b.Files))
	}
	if a.Body != b.Body || a.Description != b.Description || !reflect.DeepEqual(a.Automations, b.Automations) {
		t.Errorf("the skill differs: %+v / %+v", a, b)
	}
}

func TestSB18_ADeclarationThatCannotBeRight(t *testing.T) {
	for _, c := range []struct {
		name, automations, want string
	}{
		{"no such blueprint", "      - { id: nachts, blueprint: fehlt.yaml }\n", "fehlt.yaml"},
		{"not under blueprints", "      - { id: nachts, blueprint: ../SKILL.md }\n", "../SKILL.md"},
		{"an id twice", "      - { id: nachts, blueprint: zu_ruhig.yaml }\n      - { id: nachts, blueprint: zu_ruhig.yaml }\n", `"nachts"`},
		{"an id that is not small words", "      - { id: Nachts!, blueprint: zu_ruhig.yaml }\n", "Nachts!"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			files := maps.Clone(zuRuhigFiles)
			files["SKILL.md"] = "---\nname: zu-ruhig\ndescription: Zu ruhig.\nmetadata:\n  mira:\n    automations:\n" + c.automations + "---\nText.\n"
			writeFiles(t, filepath.Join(dir, "zu-ruhig"), files)
			got, problems := Load([]string{dir}, map[string]map[string]any{"zu-ruhig": nil})
			if len(got) != 0 || len(problems) != 1 {
				t.Fatalf("loaded %d, problems %v", len(got), problems)
			}
			if text := problems[0].Error(); !strings.Contains(text, c.want) || !strings.Contains(text, "automation") {
				t.Errorf("problem %q does not name %s", text, c.want)
			}
		})
	}
}

func TestSB29_AScriptDeclarationThatCannotBeRight(t *testing.T) {
	for _, c := range []struct {
		name, scripts, want string
	}{
		{"no such blueprint", "      - { id: abfrage, blueprint: fehlt.yaml, description: D }\n", "fehlt.yaml"},
		{"an id twice", "      - { id: abfrage, blueprint: zu_ruhig.yaml, description: D }\n      - { id: abfrage, blueprint: zu_ruhig.yaml, description: D }\n", "Two scripts"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			files := maps.Clone(zuRuhigFiles)
			files["SKILL.md"] = "---\nname: zu-ruhig\ndescription: Zu ruhig.\nmetadata:\n  mira:\n    scripts:\n" + c.scripts + "---\nText.\n"
			writeFiles(t, filepath.Join(dir, "zu-ruhig"), files)
			got, problems := Load([]string{dir}, map[string]map[string]any{"zu-ruhig": nil})
			if len(got) != 0 || len(problems) != 1 {
				t.Fatalf("loaded %d, problems %v", len(got), problems)
			}
			if text := problems[0].Error(); !strings.Contains(text, c.want) || !strings.Contains(text, "script") {
				t.Errorf("problem %q does not name %s", text, c.want)
			}
		})
	}
	// A right one loads, with its description.
	dir := t.TempDir()
	files := maps.Clone(zuRuhigFiles)
	files["SKILL.md"] = "---\nname: zu-ruhig\ndescription: Zu ruhig.\nmetadata:\n  mira:\n    scripts:\n      - { id: abfrage, blueprint: zu_ruhig.yaml, description: Für das Modell }\n---\nText.\n"
	writeFiles(t, filepath.Join(dir, "zu-ruhig"), files)
	got, problems := Load([]string{dir}, map[string]map[string]any{"zu-ruhig": nil})
	if len(got) != 1 || len(problems) != 0 || len(got[0].Scripts) != 1 || got[0].Scripts[0].Description != "Für das Modell" {
		t.Errorf("loaded %v, problems %v", got, problems)
	}
}

func TestSB34_AScriptDeclarationNeedsADescription(t *testing.T) {
	dir := t.TempDir()
	files := maps.Clone(zuRuhigFiles)
	files["SKILL.md"] = "---\nname: zu-ruhig\ndescription: Zu ruhig.\nmetadata:\n  mira:\n    scripts:\n      - { id: abfrage, blueprint: zu_ruhig.yaml }\n---\nText.\n"
	writeFiles(t, filepath.Join(dir, "zu-ruhig"), files)
	got, problems := Load([]string{dir}, map[string]map[string]any{"zu-ruhig": nil})
	if len(got) != 0 || len(problems) != 1 || !strings.Contains(problems[0].Error(), "abfrage") || !strings.Contains(problems[0].Error(), "description") {
		t.Errorf("loaded %d, problems %v", len(got), problems)
	}
}

// A setting left empty leaves its input out (SB-15's skill side); a secret
// setting is filled, it goes to Home Assistant and never to a prompt.
func TestAutomationInputsFromSettings(t *testing.T) {
	decl := map[string]Setting{"window_end": {}, "token": {Secret: true}}
	got, p := automations("automation", fstest.MapFS{"blueprints/b.yaml": {}}, []automationDecl{{ID: "a", Blueprint: "b.yaml",
		Inputs: map[string]any{"window_end": "{{ window_end }}", "token": "Bearer {{token}}", "tpl": "{{ states('x') }}", "other": "{{ unknown }}"}}},
		map[string]any{"window_end": "", "token": "geheim"}, map[string]string{"window_end": "", "token": "geheim"}, decl)
	if p != nil {
		t.Fatal(p)
	}
	want := map[string]any{"token": "Bearer geheim", "tpl": "{{ states('x') }}", "other": "{{ unknown }}"}
	if !reflect.DeepEqual(got[0].Inputs, want) {
		t.Errorf("inputs %v", got[0].Inputs)
	}
}
