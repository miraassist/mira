package docs

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// builtSteps are, per feature (its scenario prefix), the steps of its
// switch-over that are built: every scenario tagged with one of them must
// have its Go test.
var builtSteps = map[string][]int{
	"AG": {1, 2, 3, 4, 5, 6, 7, 8, 9}, // docs/spec/agenten.md, Switching over in steps
	"MO": {1, 2, 3, 4, 5},             // docs/spec/modelle.md, Steps
	"SM": {1},                         // docs/spec/skill-mcp.md
}

var (
	reTag      = regexp.MustCompile(`^\s*@step(\d+)\b`)
	reScenario = regexp.MustCompile(`^\s*Scenario:\s*([A-Z]+)-([0-9A-Z]+)\b`)
	// A scenario test: Test, the two-letter prefix and the number (07, 100,
	// A1), an underscore.
	reTest = regexp.MustCompile(`^func Test([A-Z]{2}(?:[0-9]+|[A-Z][0-9]+))_\w*\(t \*testing\.T\)`)
)

type scenario struct {
	id   string // "AG07"
	step int
	file string
}

// Every scenario of a built step has a Go test carrying its number, and
// every such test belongs to a scenario that exists.
func TestScenariosHaveTests(t *testing.T) {
	scenarios := readScenarios(t)
	tests := readTests(t)
	for id, sc := range scenarios {
		if slices.Contains(builtSteps[prefixOf(id)], sc.step) {
			if _, ok := tests[id]; !ok {
				t.Errorf("%s (%s, step %d) has no test Test%s_…", id, sc.file, sc.step, id)
			}
		}
	}
	var unknown []string
	for id, file := range tests {
		if _, ok := scenarios[id]; !ok {
			unknown = append(unknown, id+" in "+file)
		}
	}
	sort.Strings(unknown)
	for _, u := range unknown {
		t.Errorf("test Test%s has no scenario", u)
	}
}

// prefixOf is a scenario id's feature prefix: "AG07" → "AG", "AGA1" → "AG".
func prefixOf(id string) string {
	for p := range builtSteps {
		if strings.HasPrefix(id, p) {
			return p
		}
	}
	return ""
}

func readScenarios(t *testing.T) map[string]scenario {
	t.Helper()
	files, err := filepath.Glob("spec/*.feature")
	if err != nil || len(files) == 0 {
		t.Fatalf("no feature files: %v", err)
	}
	out := map[string]scenario{}
	for _, f := range files {
		file, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		step := 0
		sc := bufio.NewScanner(file)
		for sc.Scan() {
			line := sc.Text()
			if m := reTag.FindStringSubmatch(line); m != nil {
				step, _ = strconv.Atoi(m[1])
				continue
			}
			if m := reScenario.FindStringSubmatch(line); m != nil {
				id := m[1] + m[2]
				if _, dup := out[id]; dup {
					t.Errorf("%s: scenario %s-%s twice", f, m[1], m[2])
				}
				if step == 0 {
					t.Errorf("%s: scenario %s-%s has no @step tag", f, m[1], m[2])
				}
				out[id] = scenario{id: id, step: step, file: f}
				step = 0
			}
		}
		file.Close()
	}
	return out
}

// readTests finds the scenario tests in the whole module: Test<ID>_….
func readTests(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && d.Name() != ".." || d.Name() == "testenv" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if m := reTest.FindStringSubmatch(line); m != nil && prefixOf(m[1]) != "" {
				out[m[1]] = path
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
