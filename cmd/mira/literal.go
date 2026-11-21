package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/i18n"
)

// literalTexts is the page's answer to a value with "${" (docs/spec/editing.md,
// decision 11).
var literalTexts = map[i18n.Lang]string{
	i18n.DE: "„%s“ darf kein „${“ enthalten: Mira läse es beim Start als Verweis auf ein Geheimnis oder eine Umgebungsvariable.",
	i18n.EN: "\"%s\" may not contain \"${\": at her start Mira would read it as a reference to a secret or an environment variable.",
}

// literal refuses a value the page writes into mira.yaml as it is typed
// when it holds "${": Mira reads the file with ${NAME} replaced from the
// secrets file and the environment, so such a value would bring a secret
// onto the page or to an address of the writer's choice (decision 11). The
// references the page makes itself to its secrets file are not written
// through here; YAML text an admin writes (agents, thread, MCP) is not
// checked. fields maps a field's name to its value.
func literal(lang i18n.Lang, fields map[string]string) error {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.Contains(fields[name], "${") {
			return fmt.Errorf(i18n.Pick(literalTexts, lang), name)
		}
	}
	return nil
}

// literalJSON is literal for a settings section the page sends whole: the
// first string in it with "${" is refused, named by its path in the section.
func literalJSON(lang i18n.Lang, section string, raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil // the section's own check names what does not parse
	}
	if path, ok := findRef(v, section); ok {
		return fmt.Errorf(i18n.Pick(literalTexts, lang), path)
	}
	return nil
}

func findRef(v any, path string) (string, bool) {
	switch t := v.(type) {
	case string:
		return path, strings.Contains(t, "${")
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if strings.Contains(k, "${") {
				return path + "." + k, true
			}
			if p, ok := findRef(t[k], path+"."+k); ok {
				return p, true
			}
		}
	case []any:
		for i, e := range t {
			if p, ok := findRef(e, fmt.Sprintf("%s[%d]", path, i)); ok {
				return p, true
			}
		}
	}
	return "", false
}
