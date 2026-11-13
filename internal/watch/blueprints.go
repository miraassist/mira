// Package watch holds Mira's blueprints, the fixed ways of watching a device
// (docs/spec/device-watch.md): the blueprint files embedded in the service,
// making sure Home Assistant has them in this version (step A2), and the
// catalog the planner chooses from, with the code's check of a choice (A3)
// and Home Assistant's rendering of it (A4).
package watch

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed blueprints/*.yaml
var files embed.FS

// Folder is where Mira's blueprints live in Home Assistant's blueprints of
// the domain automation.
const Folder = "mira/"

// MessageInput is the input that holds the entry's text: the service fills
// it in the house's language, never the planner.
const MessageInput = "message"

// Input is one named input of a blueprint, as its selector bounds it.
type Input struct {
	Name string
	// Kind is "entity", "number" or "text".
	Kind string
	// Domain and DeviceClass bound an entity.
	Domain, DeviceClass string
	// Min, Max and Unit bound a number.
	Min, Max float64
	Unit     string
	// Default is the value Home Assistant takes when the input is not given.
	Default    any
	HasDefault bool
}

// Blueprint is one of Mira's blueprints.
type Blueprint struct {
	// Name is the file's name without ".yaml": water_runs_on.
	Name string
	// Version is the number after "mira-version:" in its description: a
	// changed blueprint gets a higher one.
	Version int
	Inputs  []Input
	yaml    string
}

// Path is the blueprint's path as Home Assistant lists it.
func (b Blueprint) Path() string { return Folder + b.Name + ".yaml" }

// Input is the input of that name.
func (b Blueprint) Input(name string) (Input, bool) {
	for _, in := range b.Inputs {
		if in.Name == name {
			return in, true
		}
	}
	return Input{}, false
}

// YAML is the blueprint file as it is saved in Home Assistant.
func (b Blueprint) YAML() string { return b.yaml }

var versionPattern = regexp.MustCompile(`mira-version:\s*(\d+)`)

// VersionOf reads the version from a blueprint's description; 0 when it has
// none.
func VersionOf(description string) int {
	m := versionPattern.FindStringSubmatch(description)
	if m == nil {
		return 0
	}
	v, _ := strconv.Atoi(m[1])
	return v
}

// All are Mira's blueprints, by name, read from the embedded files.
var All = mustLoad()

func mustLoad() []Blueprint {
	list, err := load(files)
	if err != nil {
		panic(err)
	}
	return list
}

func load(fsys fs.FS) ([]Blueprint, error) {
	names, err := fs.Glob(fsys, "blueprints/*.yaml")
	if err != nil {
		return nil, err
	}
	var out []Blueprint
	for _, file := range names {
		raw, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, err
		}
		b, err := parse(strings.TrimSuffix(strings.TrimPrefix(file, "blueprints/"), ".yaml"), raw)
		if err != nil {
			return nil, fmt.Errorf("blueprint %s: %w", file, err)
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parse reads a blueprint's metadata: its version and its inputs with their
// selectors. The file keeps Home Assistant's tags (!input); only the part
// "blueprint" is decoded.
func parse(name string, raw []byte) (Blueprint, error) {
	var doc struct {
		Blueprint struct {
			Description string `yaml:"description"`
			Domain      string `yaml:"domain"`
			Input       yaml.Node
		} `yaml:"blueprint"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Blueprint{}, err
	}
	if doc.Blueprint.Domain != "automation" {
		return Blueprint{}, fmt.Errorf("domain %q, not automation", doc.Blueprint.Domain)
	}
	b := Blueprint{Name: name, Version: VersionOf(doc.Blueprint.Description), yaml: string(raw)}
	if b.Version == 0 {
		return Blueprint{}, fmt.Errorf("no mira-version in its description")
	}
	node := doc.Blueprint.Input
	if node.Kind != yaml.MappingNode {
		return Blueprint{}, fmt.Errorf("no inputs")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		var spec struct {
			Default  any                  `yaml:"default"`
			Selector map[string]yaml.Node `yaml:"selector"`
		}
		if err := node.Content[i+1].Decode(&spec); err != nil {
			return Blueprint{}, fmt.Errorf("input %s: %w", node.Content[i].Value, err)
		}
		in := Input{Name: node.Content[i].Value, Default: spec.Default}
		// A default of null in YAML is no default; Mira's blueprints have none.
		in.HasDefault = spec.Default != nil
		if sel, ok := spec.Selector["entity"]; ok {
			var e struct {
				Filter []struct {
					Domain      string `yaml:"domain"`
					DeviceClass string `yaml:"device_class"`
				} `yaml:"filter"`
			}
			if err := sel.Decode(&e); err != nil {
				return Blueprint{}, fmt.Errorf("input %s: %w", in.Name, err)
			}
			in.Kind = "entity"
			if len(e.Filter) > 0 {
				in.Domain, in.DeviceClass = e.Filter[0].Domain, e.Filter[0].DeviceClass
			}
		} else if sel, ok := spec.Selector["number"]; ok {
			var n struct {
				Min  float64 `yaml:"min"`
				Max  float64 `yaml:"max"`
				Unit string  `yaml:"unit_of_measurement"`
			}
			if err := sel.Decode(&n); err != nil {
				return Blueprint{}, fmt.Errorf("input %s: %w", in.Name, err)
			}
			in.Kind = "number"
			in.Min, in.Max, in.Unit = n.Min, n.Max, n.Unit
		} else if _, ok := spec.Selector["text"]; ok {
			in.Kind = "text"
		} else {
			return Blueprint{}, fmt.Errorf("input %s: a selector Mira does not know", in.Name)
		}
		b.Inputs = append(b.Inputs, in)
	}
	if _, ok := b.Input(MessageInput); !ok {
		return Blueprint{}, fmt.Errorf("no input %s", MessageInput)
	}
	return b, nil
}

// ByName is the blueprint of that name among All.
func ByName(name string) (Blueprint, bool) {
	for _, b := range All {
		if b.Name == name {
			return b, true
		}
	}
	return Blueprint{}, false
}

// NameOf is the name of one of Mira's blueprints from its path
// ("mira/water_runs_on.yaml"); false for any other path.
func NameOf(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, Folder)
	if !ok {
		return "", false
	}
	return strings.TrimSuffix(rest, ".yaml"), true
}
