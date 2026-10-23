package skills

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
)

// A skill's SKILL.md as Mira's page edits it (docs/spec/bearbeiten.md): the
// frontmatter as a YAML tree, so what the page does not change stays as
// written, and the body as text.

// Find is the folder of a skill in dirs, the first that has it; "" if none.
func Find(dirs []string, name string) string { return find(dirs, name) }

// File is a SKILL.md split into its frontmatter and body.
type File struct {
	Front *yaml.Node // the frontmatter's mapping
	Body  string
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
	return File{Front: doc.Content[0], Body: string(body)}, nil
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
	tmp, err := os.MkdirTemp("", "mira-skill-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), raw, 0o644); err != nil {
		return err
	}
	if _, p := load(dir, settings); p != nil {
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
