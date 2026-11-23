package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// PA-06 No parse error goes without a trace (docs/spec/parsing.md, rule 3):
// a parse error dropped with "_ =" has a comment on its line or the two
// lines above saying why it cannot fail or where it is reported.
func TestPA06_NoParseErrorGoesWithoutATrace(t *testing.T) {
	dropped := regexp.MustCompile(`_ = (json|yaml)\.Unmarshal\(`)
	var found []string
	for _, path := range goFiles(t, false) {
		slash := filepath.ToSlash(path)
		if strings.Contains(slash, "/cmd/testha/") || strings.Contains(slash, "/internal/testsupport/") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if !dropped.MatchString(line) {
				continue
			}
			said := strings.Contains(line, "//")
			for k := i - 1; k >= 0 && k >= i-2 && !said; k-- {
				said = strings.HasPrefix(strings.TrimSpace(lines[k]), "//")
			}
			if !said {
				found = append(found, strings.TrimPrefix(slash, "../")+":"+itoa(i+1))
			}
		}
	}
	if len(found) > 0 {
		t.Errorf("a parse error dropped without a word (docs/spec/parsing.md, rule 3); log it, or say why it cannot fail:\n%s",
			strings.Join(found, "\n"))
	}
}
