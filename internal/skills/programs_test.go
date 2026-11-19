package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
)

const progSum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// writeSkill writes a skill with a program and a stdio server into a folder.
func writeSkill(t *testing.T, program string) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "laden"), 0o755)
	os.WriteFile(filepath.Join(dir, "laden", "SKILL.md"), []byte(`---
name: laden
description: Einkaufen
metadata:
  mira:
    settings:
      programm: { description: "Optional: ein eigenes Programm" }
`+program+`
    mcp:
      transport: stdio
      command: "{{ programm }}"
      args: [mcp]
      env: ["KORB_CONFIG_DIR={{ data }}"]
---
Der Ordner ist {{ data }}, die Architektur {{ arch }}.
`), 0o644)
	return dir
}

const programYAML = `    program:
      name: korb
      version: 0.9.0
      url: https://example.org/v0.9.0/korb_linux_{{ arch }}
      sha256:
        amd64: "` + progSum + `"`

func loadOne(t *testing.T, dir string, settings map[string]any, opts Options) (Skill, []Problem) {
	t.Helper()
	loaded, problems := LoadWith(Roots([]string{dir}), map[string]map[string]any{"laden": settings}, opts)
	if len(loaded) == 1 {
		return loaded[0], problems
	}
	return Skill{}, problems
}

// PG-01, PG-08 The program for Mira's architecture becomes the server's
// command; {{ data }} is the skill's own folder, made with mode 0700.
func TestPG08_TheSkillsFolderAndProgram(t *testing.T) {
	old := Arch
	Arch = "amd64"
	t.Cleanup(func() { Arch = old })
	base := t.TempDir()
	opts := Options{DataRoot: filepath.Join(base, "skill-data"), ProgramRoot: filepath.Join(base, "programs")}
	s, problems := loadOne(t, writeSkill(t, programYAML), nil, opts)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p := s.Program
	if p == nil || p.URL != "https://example.org/v0.9.0/korb_linux_amd64" || p.SHA256 != progSum || p.FileName() != "korb-0.9.0-0123456789ab" {
		t.Fatalf("program %+v", p)
	}
	data := filepath.Join(base, "skill-data", "laden")
	if s.MCP.Command != filepath.Join(base, "programs", "korb-0.9.0-0123456789ab") || s.MCP.Dir != data || s.MCP.Env[0] != "KORB_CONFIG_DIR="+data {
		t.Fatalf("server %+v", s.MCP)
	}
	if st, err := os.Stat(data); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("folder %v %v", st, err)
	}
	if !strings.Contains(s.Body, "Der Ordner ist "+data+", die Architektur amd64.") {
		t.Errorf("body %q", s.Body)
	}
	if _, ok := s.Settings["data"]; ok {
		t.Error("data is shown as a setting")
	}
	// It survives loading again (a restart).
	os.WriteFile(filepath.Join(data, "token"), []byte("x"), 0o600)
	loadOne(t, writeSkill(t, programYAML), nil, opts)
	if _, err := os.Stat(filepath.Join(data, "token")); err != nil {
		t.Error("the folder was emptied")
	}
}

// PG-07 One's own program wins: the setting's path is the command.
func TestPG07_OnesOwnProgramWins(t *testing.T) {
	old := Arch
	Arch = "amd64"
	t.Cleanup(func() { Arch = old })
	s, problems := loadOne(t, writeSkill(t, programYAML), map[string]any{"programm": "/opt/korb"}, Options{ProgramRoot: "/x/programs"})
	if len(problems) > 0 || s.MCP.Command != "/opt/korb" {
		t.Fatalf("%v %+v", problems, s.MCP)
	}
}

// PG-06 Not for this architecture; and the other problems of the section.
func TestPG06_NotForThisArchitecture(t *testing.T) {
	old := Arch
	t.Cleanup(func() { Arch = old })
	Arch = "riscv64"
	_, problems := loadOne(t, writeSkill(t, programYAML), nil, Options{})
	if len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.DE), "riscv64") {
		t.Fatalf("problems %v", problems)
	}
	Arch = "amd64"
	for name, yml := range map[string]string{
		"fields": "    program:\n      name: korb\n      version: 0.9.0\n",
		"url":    strings.Replace(programYAML, "https://", "http://", 1),
		"sum":    strings.Replace(programYAML, progSum, "zz", 1),
		"name":   strings.Replace(programYAML, "name: korb", "name: ../korb", 1),
	} {
		if _, problems := loadOne(t, writeSkill(t, yml), nil, Options{}); len(problems) != 1 {
			t.Errorf("%s: %v", name, problems)
		}
	}
	// A setting named like Mira's placeholder.
	dir := writeSkill(t, programYAML)
	raw, _ := os.ReadFile(filepath.Join(dir, "laden", "SKILL.md"))
	os.WriteFile(filepath.Join(dir, "laden", "SKILL.md"), []byte(strings.Replace(string(raw), "programm: {", "data: {", 1)), 0o644)
	if _, problems := loadOne(t, dir, nil, Options{}); len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.EN), "placeholder") {
		t.Errorf("reserved: %v", problems)
	}
}
