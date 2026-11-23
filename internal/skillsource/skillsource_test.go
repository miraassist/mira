package skillsource

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/skills"
)

// The fake source serves the example answers of the server's contract
// (mira-skills: contract/answers, copied into testdata).
var noWait = &retry.Policy{Attempts: 3, First: time.Millisecond, Max: 30 * time.Second, Growth: retry.Exponential}

type fake struct {
	srv      *httptest.Server
	requests atomic.Int32
	auth     atomic.Value
	version  atomic.Value
}

func newFake(t *testing.T, handle func(w http.ResponseWriter)) *fake {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.auth.Store(r.Header.Get("Authorization"))
		f.version.Store(r.Header.Get("Mira-Version"))
		if r.URL.Path != "/v1/skills" || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		handle(w)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func answer(t *testing.T, status int, file string) func(http.ResponseWriter) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(raw)
	}
}

func (f *fake) load(t *testing.T) (*skills.Root, []skills.Problem) {
	t.Helper()
	return Load(context.Background(), Options{Source: config.SkillSource{URL: f.srv.URL, Key: "mss_test"}, Version: "0.26.0", Policy: noWait})
}

func kindOf(t *testing.T, problems []skills.Problem, want string) skills.Problem {
	t.Helper()
	if len(problems) != 1 {
		t.Fatalf("problems %v, want one %s", problems, want)
	}
	if text := problems[0].Text(i18n.DE); !strings.Contains(text, want) {
		t.Errorf("problem %q does not say %q", text, want)
	}
	return problems[0]
}

func TestSQ01_SkillsLoadedFromTheSource(t *testing.T) {
	f := newFake(t, answer(t, 200, "200-skills.json"))
	root, problems := f.load(t)
	if root == nil || len(problems) > 0 {
		t.Fatalf("root %v, problems %v", root, problems)
	}
	if f.requests.Load() != 1 || f.auth.Load() != "Bearer mss_test" || f.version.Load() != "0.26.0" {
		t.Errorf("requests %d, auth %v, version %v", f.requests.Load(), f.auth.Load(), f.version.Load())
	}
	got, problems := skills.LoadFrom([]skills.Root{*root}, map[string]map[string]any{"beispiel": nil})
	if len(got) != 1 || len(problems) > 0 {
		t.Fatalf("loaded %d, problems %v", len(got), problems)
	}
	sk := got[0]
	if sk.Name != "beispiel" || sk.Source != f.srv.URL || !strings.Contains(sk.Body, "Wenn jemand nach dem Beispiel fragt") {
		t.Errorf("skill %+v", sk)
	}
	if raw, err := fs.ReadFile(sk.Files, "prompts/morgen.md"); err != nil || !strings.HasPrefix(string(raw), "Guten Morgen!") {
		t.Errorf("prompts/morgen.md: %q, %v", raw, err)
	}
}

func TestSQ02_ASourcesSkillNotSwitchedOn(t *testing.T) {
	f := newFake(t, answer(t, 200, "200-skills.json"))
	root, _ := f.load(t)
	roots := []skills.Root{*root}
	if got, _ := skills.LoadFrom(roots, nil); len(got) != 0 {
		t.Errorf("a skill runs that is not switched on: %v", got)
	}
	avail := skills.AvailableIn(roots, nil)
	if len(avail) != 1 || avail[0].Name != "beispiel" || avail[0].Description == "" {
		t.Errorf("available %+v", avail)
	}
}

func TestSQ03_TheSourceCannotBeReached(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) })
	root, problems := f.load(t)
	if root != nil || f.requests.Load() != 3 {
		t.Errorf("root %v after %d requests, want none after 3", root, f.requests.Load())
	}
	p := kindOf(t, problems, "Die Skills aus dem Abo konnten nicht geladen werden")
	if !strings.Contains(p.Text(i18n.DE), "nächsten Start") {
		t.Errorf("problem %q", p.Text(i18n.DE))
	}
	// A source that is not there at all: the same, after three tries.
	f.srv.Close()
	if root, problems := f.load(t); root != nil || len(problems) != 1 {
		t.Errorf("closed source: root %v, problems %v", root, problems)
	}
}

func TestSQ04_TheKeyIsRefused(t *testing.T) {
	for _, c := range []struct {
		status int
		file   string
	}{{401, "401.json"}, {403, "403.json"}} {
		f := newFake(t, answer(t, c.status, c.file))
		root, problems := f.load(t)
		if root != nil || f.requests.Load() != 1 {
			t.Errorf("%d: root %v after %d requests, want none after 1", c.status, root, f.requests.Load())
		}
		kindOf(t, problems, "Dein Abo ist abgelaufen oder der Schlüssel stimmt nicht")
	}
}

func TestSQ05_NotASkillSource(t *testing.T) {
	for name, handle := range map[string]func(http.ResponseWriter){
		"404":      answer(t, 404, "404.json"),
		"not json": func(w http.ResponseWriter) { _, _ = w.Write([]byte("<html>hello</html>")) },
	} {
		f := newFake(t, handle)
		root, problems := f.load(t)
		if root != nil || f.requests.Load() != 1 {
			t.Errorf("%s: root %v after %d requests, want none after 1", name, root, f.requests.Load())
		}
		if p := kindOf(t, problems, "keine Skill-Quelle"); !strings.Contains(p.Text(i18n.DE), f.srv.URL) {
			t.Errorf("%s: problem %q does not name the address", name, p.Text(i18n.DE))
		}
	}
}

func TestSQ06_APathThatLeavesTheSkillsFolder(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"skills":[
			{"name":"boese","version":"1.0.0","files":{"SKILL.md":"---\nname: boese\ndescription: x\n---\n","../x":"y"}},
			{"name":"gut","version":"1.0.0","files":{"SKILL.md":"---\nname: gut\ndescription: Gut.\n---\nText.\n"}}]}`))
	})
	root, problems := f.load(t)
	p := kindOf(t, problems, `"../x"`)
	if p.Skill != "boese" {
		t.Errorf("problem of %q", p.Skill)
	}
	got, problems := skills.LoadFrom([]skills.Root{*root}, map[string]map[string]any{"gut": nil})
	if len(got) != 1 || len(problems) > 0 || skills.ExistsIn([]skills.Root{*root}, "boese") {
		t.Errorf("loaded %v, problems %v", got, problems)
	}
}

func TestSQ07_ANameTakenTwice(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "beispiel"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beispiel", "SKILL.md"), []byte("---\nname: beispiel\ndescription: Meins.\n---\nMein Text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFake(t, answer(t, 200, "200-skills.json"))
	root, _ := f.load(t)
	got, problems := skills.LoadFrom(append(skills.Roots([]string{dir}), *root), map[string]map[string]any{"beispiel": nil})
	if len(got) != 1 || got[0].Source != "" || got[0].Body != "Mein Text." {
		t.Fatalf("loaded %+v", got)
	}
	p := kindOf(t, problems, dir)
	if text := p.Text(i18n.DE); !strings.Contains(text, f.srv.URL) || !strings.Contains(text, "beispiel") {
		t.Errorf("problem %q does not name both", text)
	}
}

func TestSQ08_NoCopyOnDisk(t *testing.T) {
	f := newFake(t, answer(t, 200, "200-skills.json"))
	work := t.TempDir()
	t.Chdir(work)
	t.Setenv("MIRA_DATA", work)
	root, _ := f.load(t)
	if _, problems := skills.LoadFrom([]skills.Root{*root}, map[string]map[string]any{"beispiel": nil}); len(problems) > 0 {
		t.Fatal(problems)
	}
	entries, err := os.ReadDir(work)
	if err != nil || len(entries) > 0 {
		t.Errorf("files written: %v, %v", entries, err)
	}
	if _, onDisk := root.FS.(interface{ Name() string }); onDisk {
		t.Error("the source's skills are not in memory")
	}
}

// The address must be https, http only on this machine (decision 14).
func TestSourceAddressMustBeHTTPS(t *testing.T) {
	for addr, ok := range map[string]bool{"https://skills.example.org": true, "http://127.0.0.1:8792": true,
		"http://localhost:8792": true, "http://skills.example.org": false, "ftp://x": false, "skills.example.org": false} {
		if allowed(addr) != ok {
			t.Errorf("%s: allowed %v", addr, !ok)
		}
	}
	root, problems := Load(context.Background(), Options{Source: config.SkillSource{URL: "http://skills.example.org", Key: "k"}})
	if root != nil {
		t.Error("asked an http source elsewhere")
	}
	kindOf(t, problems, "https://")
}

// PA-04 A skill of the subscription that cannot be read is left out
// (docs/spec/parsing.md): the others load, and that skill has a problem
// naming why; a version that is a number is taken as it comes.
func TestPA04_ASkillOfTheSubscriptionThatCannotBeReadIsLeftOut(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"skills":[
			{"name":"kaputt","version":"1.0","files":7},
			{"name":"beispiel","version":2,"files":{"SKILL.md":"---\nname: beispiel\ndescription: Ein Beispiel\n---\nBeispiel."}}
		]}`))
	})
	root, problems := f.load(t)
	if root == nil {
		t.Fatal("no skills at all")
	}
	p := kindOf(t, problems, "kaputt")
	if p.Skill != "kaputt" {
		t.Errorf("problem of %q", p.Skill)
	}
	if _, err := fs.ReadFile(root.FS, "beispiel/SKILL.md"); err != nil {
		t.Errorf("the other skill did not load: %v", err)
	}
}
