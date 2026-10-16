package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// UISections are the parts of the config the web UI may change. None of them
// holds a secret; tokens and URLs stay in the YAML file and the environment.
var UISections = []string{"notifier", "persons", "satellites", "persona"}

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
	}
	return nil, fmt.Errorf("unknown or read-only config section %q", name)
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
	case *string:
		var v string
		if err := yaml.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("section %s: %w", name, err)
		}
		*p = strings.TrimSpace(v)
	}
	return nil
}
