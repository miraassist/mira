package startools

import (
	"fmt"
	"regexp"
	"strings"
)

// doc is a tool's docstring in Google's style (docs/spec/api-tools.md,
// decision 4): the text is the tool's description, the section "Args:" has
// one entry per parameter, "name (type): text", continued on lines indented
// deeper.
type doc struct {
	Text string
	Args []docArg
}

type docArg struct {
	Name, Type, Text string
	// Line is the entry's line within the docstring, from 0.
	Line int
}

var argLine = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(?:\(([^)]*)\))?\s*:\s*(.*)$`)

// parseDoc reads a docstring. An entry under Args: without a type in
// parentheses is an error naming the entry.
func parseDoc(raw string) (doc, error) {
	lines := cleandoc(raw)
	var text []string
	var d doc
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t != "Args:" && t != "Arguments:" {
			text = append(text, lines[i])
			continue
		}
		head := indent(lines[i])
		entry := -1
		for i++; i < len(lines); i++ {
			l := lines[i]
			if strings.TrimSpace(l) == "" {
				continue
			}
			ind := indent(l)
			if ind <= head {
				i--
				break
			}
			if entry < 0 || ind <= entry {
				entry = ind
				m := argLine.FindStringSubmatch(strings.TrimSpace(l))
				if m == nil {
					return doc{}, fmt.Errorf("line %d of the docstring is no entry \"name (type): text\": %q", i+1, strings.TrimSpace(l))
				}
				if strings.TrimSpace(m[2]) == "" {
					return doc{}, fmt.Errorf("the parameter %s in the docstring has no type in parentheses, as \"%s (str): …\"", m[1], m[1])
				}
				d.Args = append(d.Args, docArg{Name: m[1], Type: strings.TrimSpace(m[2]), Text: strings.TrimSpace(m[3]), Line: i})
				continue
			}
			a := &d.Args[len(d.Args)-1]
			a.Text = strings.TrimSpace(a.Text + " " + strings.TrimSpace(l))
		}
	}
	d.Text = strings.TrimSpace(strings.Join(trimBlank(text), "\n"))
	return d, nil
}

// cleandoc removes the common indentation of the lines after the first, as
// Python's inspect.cleandoc does.
func cleandoc(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\t", "    "), "\n")
	least := -1
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if n := indent(l); least < 0 || n < least {
			least = n
		}
	}
	lines[0] = strings.TrimSpace(lines[0])
	for i := 1; i < len(lines); i++ {
		if least > 0 && len(lines[i]) >= least {
			lines[i] = lines[i][least:]
		} else {
			lines[i] = strings.TrimLeft(lines[i], " ")
		}
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

func indent(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }

// trimBlank drops blank lines at both ends and joins runs of them into one.
func trimBlank(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, l)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}
