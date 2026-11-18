package skills

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
)

// BlueprintsFolder is the folder of a skill whose *.yaml files are its skill
// blueprints (docs/spec/skill-files.md, decision 6).
const BlueprintsFolder = "blueprints"

// Automation is a skill automation as the skill declares it
// (docs/spec/skill-files.md, What a skill writes), its inputs filled from
// the settings. A skill script is declared the same way (decision 18).
type Automation struct {
	// ID is the declaration's id, unique within the skill.
	ID string
	// Blueprint is the file under blueprints/ ("zu_ruhig.yaml").
	Blueprint string
	// Alias is the automation's name when Mira makes it; empty for the
	// blueprint's name.
	Alias string
	// Description is what Mira writes into it when she makes it; empty
	// for her own line. For a script it is the tool's description the
	// model reads (docs/spec/skill-files.md, decision 25).
	Description string
	// Inputs are the blueprint's inputs: an input whose value is one
	// placeholder of a setting that is empty is left out (decision 15).
	Inputs map[string]any
}

type automationDecl struct {
	ID          string         `yaml:"id"`
	Blueprint   string         `yaml:"blueprint"`
	Alias       string         `yaml:"alias"`
	Description string         `yaml:"description"`
	Inputs      map[string]any `yaml:"inputs"`
}

var (
	automationIDPattern = regexp.MustCompile(`^[a-z0-9]+([-_][a-z0-9]+)*$`)
	onePlaceholder      = regexp.MustCompile(`^\s*\{\{\s*([a-zA-Z0-9_]+)\s*\}\}\s*$`)
)

// automations checks a skill's declared automations (kind "automation") or
// scripts (kind "script") and fills their inputs: raw are the settings as
// mira.yaml has them, vals as text.
func automations(kind string, files fs.FS, decls []automationDecl, raw map[string]any, vals map[string]string, decl map[string]Setting) ([]Automation, *Problem) {
	var out []Automation
	seen := map[string]bool{}
	for i, d := range decls {
		d.ID = strings.TrimSpace(d.ID)
		if !automationIDPattern.MatchString(d.ID) {
			return nil, problem(kind+"id", i+1, d.ID)
		}
		if seen[d.ID] {
			return nil, problem(kind+"twice", d.ID)
		}
		seen[d.ID] = true
		file := strings.TrimSpace(d.Blueprint)
		st, err := fs.Stat(files, path.Join(BlueprintsFolder, file))
		if file == "" || strings.Contains(file, "/") || !strings.HasSuffix(file, ".yaml") || err != nil || st.IsDir() {
			return nil, problem(kind+"blueprint", d.ID, file, BlueprintsFolder)
		}
		if kind == "script" && strings.TrimSpace(d.Description) == "" {
			return nil, problem("scriptdesc", d.ID)
		}
		a := Automation{ID: d.ID, Blueprint: file, Alias: strings.TrimSpace(fill(d.Alias, vals, decl)),
			Description: strings.TrimSpace(fill(d.Description, vals, decl)), Inputs: map[string]any{}}
		for name, v := range d.Inputs {
			s, ok := v.(string)
			if !ok {
				a.Inputs[name] = v
				continue
			}
			if m := onePlaceholder.FindStringSubmatch(s); m != nil {
				if _, known := decl[m[1]]; known {
					if vals[m[1]] == "" {
						continue // the blueprint's default applies
					}
					a.Inputs[name] = raw[m[1]]
					continue
				}
			}
			a.Inputs[name] = fillAll(s, vals, decl)
		}
		out = append(out, a)
	}
	return out, nil
}

// fillAll replaces every {{setting}} the skill declares with its value,
// secret ones too: an automation's inputs go to Home Assistant, never to a
// prompt. Other names stay as written (a template of Home Assistant's).
func fillAll(s string, vals map[string]string, decl map[string]Setting) string {
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		key := placeholder.FindStringSubmatch(m)[1]
		if _, ok := decl[key]; !ok {
			return m
		}
		return vals[key]
	})
}
