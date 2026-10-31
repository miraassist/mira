package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// UISections are the parts of the config the web UI may change. None of them
// holds a secret; tokens and URLs stay in the YAML file and the environment.
var UISections = []string{"notifier", "persons", "satellites", "persona", "web_search"}

// section returns a pointer to the named section inside c.
func (c *Config) section(name string) (any, error) {
	switch name {
	case "notifier":
		return &c.Notifier, nil
	case "persons":
		return &c.Persons, nil
	case "satellites":
		return &c.Satellites, nil
	case "persona":
		return &c.Prompt.Persona, nil
	case "web_search":
		return &c.WebSearch, nil
	}
	return nil, fmt.Errorf("unknown or read-only config section %q", name)
}

// SectionPath is where a section stands in mira.yaml: most at the top, the
// persona under prompt.
func SectionPath(name string) []string {
	if name == "persona" {
		return []string{"prompt", "persona"}
	}
	return []string{name}
}

// SectionJSON renders one section as JSON with the YAML field names, so the
// UI sees the same keys and value formats ("30m", "22:30") as the file.
func (c Config) SectionJSON(name string) (json.RawMessage, error) {
	ptr, err := c.section(name)
	if err != nil {
		return nil, err
	}
	raw, err := yaml.Marshal(ptr)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}

// WithoutDefaults leaves out of a section sent by the UI (JSON) what equals
// Mira's default, compared as Mira reads it ("30m" is "30m0s"), down into
// its parts; what is left goes into mira.yaml (docs/spec/bearbeiten.md,
// Settings sections). A section all at its defaults is null.
func WithoutDefaults(name string, raw json.RawMessage) (json.RawMessage, error) {
	y, err := SectionYAML(name, raw)
	if err != nil {
		return nil, err
	}
	read, def := Default(), Default()
	if err := read.ApplySection(name, y); err != nil {
		return nil, err
	}
	var in, got, want any
	for _, x := range []struct {
		from func() (json.RawMessage, error)
		to   *any
	}{
		{func() (json.RawMessage, error) { return raw, nil }, &in},
		{func() (json.RawMessage, error) { return read.SectionJSON(name) }, &got},
		{func() (json.RawMessage, error) { return def.SectionJSON(name) }, &want},
	} {
		b, err := x.from()
		if err == nil {
			err = json.Unmarshal(b, x.to)
		}
		if err != nil {
			return nil, fmt.Errorf("section %s: %w", name, err)
		}
	}
	out, _ := differing(in, got, want)
	return json.Marshal(out)
}

// differing is what of in (as sent) differs from the default, judged by got
// (in as Mira reads it) against want (the default); false when nothing does.
func differing(in, got, want any) (any, bool) {
	if reflect.DeepEqual(got, want) {
		return nil, false
	}
	inMap, ok1 := in.(map[string]any)
	gotMap, ok2 := got.(map[string]any)
	wantMap, ok3 := want.(map[string]any)
	if !ok1 || !ok2 || !ok3 {
		return in, true
	}
	out := map[string]any{}
	for k, v := range inMap {
		if d, ok := differing(v, gotMap[k], wantMap[k]); ok {
			out[k] = d
		}
	}
	return out, len(out) > 0
}

// SectionYAML converts a section sent by the UI (JSON) to YAML, the form it
// is stored in and applied from.
func SectionYAML(name string, raw json.RawMessage) ([]byte, error) {
	if !slices.Contains(UISections, name) {
		return nil, fmt.Errorf("unknown or read-only config section %q", name)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("section %s: %w", name, err)
	}
	return yaml.Marshal(generic)
}

// ApplySection replaces one section with the given YAML. Fields the YAML
// leaves out get their defaults, not the file's values: the UI always sends
// a whole section.
func (c *Config) ApplySection(name string, raw []byte) error {
	ptr, err := c.section(name)
	if err != nil {
		return err
	}
	def := Default()
	defPtr, _ := def.section(name)
	switch p := ptr.(type) {
	case *Notifier:
		v := *defPtr.(*Notifier)
		if err := yaml.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("section %s: %w", name, err)
		}
		*p = v
	case *map[string]Person:
		var v map[string]Person
		if err := yaml.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("section %s: %w", name, err)
		}
		*p = v
	case *map[string]string:
		var v map[string]string
		if err := yaml.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("section %s: %w", name, err)
		}
		*p = v
	case *WebSearch:
		v := *defPtr.(*WebSearch)
		if err := yaml.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("section %s: %w", name, err)
		}
		*p = v
	case *string:
		var v string
		if err := yaml.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("section %s: %w", name, err)
		}
		*p = strings.TrimSpace(v)
	}
	return nil
}
