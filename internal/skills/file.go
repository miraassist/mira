package skills

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing/fstest"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
)

// A skill's SKILL.md as Mira's page edits it (docs/spec/editing.md): the
// frontmatter as a YAML tree, so what the page does not change stays as
// written, and the body as text.

// Find is the folder of a skill in dirs, the first that has it; "" if none.
func Find(dirs []string, name string) string { return find(dirs, name) }

// File is a SKILL.md split into its frontmatter and body.
type File struct {
	Front *yaml.Node // the frontmatter's mapping
	Body  string
	// Dir is the folder it was read from; Check reads the skill's other
	// files there (docs/spec/skill-files.md). Empty for a new skill.
	Dir string
}

// ReadFile reads the SKILL.md in dir.
func ReadFile(dir string) (File, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return File{}, err
	}
	fm, body, p := split(raw)
	if p != nil {
		return File{}, errors.New(p.Text(i18n.EN))
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		return File{}, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return File{}, errors.New("the frontmatter is no mapping")
	}
	return File{Front: doc.Content[0], Body: string(body), Dir: dir}, nil
}

// Bytes is the file as it is written.
func (f File) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f.Front); err != nil {
		return nil, err
	}
	enc.Close()
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(buf.Bytes())
	out.WriteString("---\n")
	out.WriteString(f.Body)
	return out.Bytes(), nil
}

// Check loads the file as Mira would, from a copy, with the given settings:
// nil when it would start.
func (f File) Check(name string, settings map[string]any, l i18n.Lang) error {
	raw, err := f.Bytes()
	if err != nil {
		return err
	}
	// The changed SKILL.md over the skill's folder: its other files (its
	// blueprints) are checked as they are.
	edited := fstest.MapFS{path.Join(name, "SKILL.md"): {Data: raw, Mode: 0o644}}
	root := Root{Name: name, FS: edited}
	if f.Dir != "" {
		root = Root{Name: filepath.Dir(f.Dir), FS: overlay{base: os.DirFS(filepath.Dir(f.Dir)), top: edited}}
	}
	if _, p := load(root, name, settings, Options{}); p != nil {
		return errors.New(p.Text(l))
	}
	return nil
}

// Write writes the file into dir, through a temporary file.
func (f File) Write(dir string) error {
	raw, err := f.Bytes()
	if err != nil {
		return err
	}
	return config.WriteAtomic(filepath.Join(dir, "SKILL.md"), raw, 0o644)
}

// Mira is the frontmatter's metadata.mira mapping, made when missing.
func (f File) Mira() *yaml.Node {
	return config.MapChild(config.MapChild(f.Front, "metadata"), "mira")
}

// SettingDecls are the settings the frontmatter declares, in order.
func (f File) SettingDecls() []NamedSetting {
	var out []NamedSetting
	meta := config.MapGet(f.Front, "metadata")
	settings := config.MapGet(config.MapGet(meta, "mira"), "settings")
	for _, name := range config.Keys(settings) {
		var s Setting
		_ = config.MapGet(settings, name).Decode(&s)
		out = append(out, NamedSetting{Name: name, Setting: s})
	}
	return out
}

// NamedSetting is a declared setting with its name.
type NamedSetting struct {
	Name string
	Setting
}

// ScheduleDecls are the schedules the frontmatter declares, as written (for
// a skill that is off: shown, not run).
func (f File) ScheduleDecls() []Schedule {
	var out []Schedule
	meta := config.MapGet(f.Front, "metadata")
	if n := config.MapGet(config.MapGet(meta, "mira"), "schedules"); n != nil {
		_ = n.Decode(&out)
	}
	return out
}

// overlay serves top's files over base's; a folder is base's.
type overlay struct {
	base fs.FS
	top  fstest.MapFS
}

func (o overlay) Open(name string) (fs.File, error) {
	if _, ok := o.top[name]; ok {
		return o.top.Open(name)
	}
	return o.base.Open(name)
}
