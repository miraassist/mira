package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// The output check (docs/spec/agenten.md, Decided 15): a small check of our
// own for the parts of JSON Schema Mira's agents use — type, properties,
// required, enum, items. Other keywords are ignored.

type schema struct {
	Type       any                `json:"type"`
	Properties map[string]*schema `json:"properties"`
	Required   []string           `json:"required"`
	Enum       []any              `json:"enum"`
	Items      *schema            `json:"items"`
}

// checkSchema reports where a JSON value does not follow a schema; nil when
// it does.
func checkSchema(rawSchema, rawValue json.RawMessage) error {
	var s schema
	if err := json.Unmarshal(rawSchema, &s); err != nil {
		return fmt.Errorf("unreadable schema: %w", err)
	}
	var v any
	if err := json.Unmarshal(rawValue, &v); err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	return s.check("", v)
}

func (s *schema) check(path string, v any) error {
	at := path
	if at == "" {
		at = "the answer"
	}
	if s.Type != nil && !s.typeOK(v) {
		return fmt.Errorf("%s is not of type %v", at, s.Type)
	}
	if len(s.Enum) > 0 {
		ok := false
		for _, e := range s.Enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%s is not one of %v", at, s.Enum)
		}
	}
	switch x := v.(type) {
	case map[string]any:
		for _, name := range s.Required {
			if _, ok := x[name]; !ok {
				return fmt.Errorf("%s lacks %q", at, name)
			}
		}
		names := make([]string, 0, len(s.Properties))
		for name := range s.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if val, ok := x[name]; ok {
				if err := s.Properties[name].check(join(path, name), val); err != nil {
					return err
				}
			}
		}
	case []any:
		if s.Items != nil {
			for i, item := range x {
				if err := s.Items.check(fmt.Sprintf("%s[%d]", path, i), item); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// typeOK checks a value against "type", a name or a list of names.
func (s *schema) typeOK(v any) bool {
	var types []string
	switch t := s.Type.(type) {
	case string:
		types = []string{t}
	case []any:
		for _, x := range t {
			types = append(types, fmt.Sprint(x))
		}
	default:
		return true
	}
	for _, t := range types {
		if is(strings.TrimSpace(t), v) {
			return true
		}
	}
	return false
}

func is(t string, v any) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == math.Trunc(f)
	case "null":
		return v == nil
	}
	return true
}
