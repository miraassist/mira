package docs

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	// A spec named by its path anywhere: docs/spec/threads.md, (spec/threads.md).
	reSpecPath = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])spec/([A-Za-z0-9_-]+\.(?:md|feature))`)
	// A link from one spec to another: [Threads](threads.md#assignment).
	reSpecLink = regexp.MustCompile(`\]\(([A-Za-z0-9_-]+\.(?:md|feature))(?:#[^)]*)?\)`)
	// The files that name specs.
	textFiles = []string{".go", ".py", ".md", ".feature", ".yaml", ".yml", ".js", ".html", ".json", ".sh", ".txt"}
	// Specs of the other repository (miraassist/mira-fritz) named here.
	foreignSpecs = []string{"fritz.md"}
)

// Every spec the repository names, by its path or by a link between specs,
// exists in docs/spec.
func TestSpecReferencesExist(t *testing.T) {
	specs := map[string]bool{}
	entries, err := os.ReadDir("spec")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		specs[e.Name()] = true
	}
	// The repository's own files, not what is ignored (private notes).
	out, err := exec.Command("git", "-C", "..", "ls-files").Output()
	if err != nil {
		t.Skip("git is not there: ", err)
	}
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path := filepath.Join("..", rel)
		if !slices.Contains(textFiles, filepath.Ext(path)) {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		var named []string
		for _, m := range reSpecPath.FindAllStringSubmatch(text, -1) {
			named = append(named, m[1])
		}
		if filepath.Base(filepath.Dir(path)) == "spec" {
			for _, m := range reSpecLink.FindAllStringSubmatch(text, -1) {
				named = append(named, m[1])
			}
		}
		for _, n := range named {
			if !specs[n] && !slices.Contains(foreignSpecs, n) {
				t.Errorf("%s names docs/spec/%s, which does not exist", rel, n)
			}
		}
	}
}
