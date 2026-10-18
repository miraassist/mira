package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

const abfallCalendar = "calendar.abfuhrtermine"

func loadAbfall(t *testing.T, url string) Skill {
	t.Helper()
	list, problems := Load([]string{"testdata/skills"}, map[string]map[string]any{
		"abfall": {"calendar": url, "types": []any{"Restabfall", "Wertstoff"}},
	})
	if len(problems) != 0 || len(list) != 1 {
		t.Fatalf("load: %v %v", list, problems)
	}
	return list[0]
}

func TestLoad(t *testing.T) {
	sk := loadAbfall(t, abfallCalendar)
	if len(sk.Schedules) != 1 || sk.Schedules[0].Cron != "0 18 * * *" {
		t.Errorf("skill: %+v", sk)
	}
	if !strings.Contains(sk.Body, "Interessante Abfallarten: Restabfall, Wertstoff") || !strings.Contains(sk.Body, `"calendar.abfuhrtermine"`) {
		t.Errorf("settings not filled in: %s", sk.Body)
	}

	// A skill that cannot start is left out with a reason the user can read;
	// the others still load.
	for name, c := range map[string]struct {
		settings map[string]any
		want     string
	}{
		"abfall": {map[string]any{}, "Die Einstellung „calendar“ fehlt in der mira.yaml"},
		"kaputt": {nil, "muss genauso heißen wie der Ordner"},
		"fehlt":  {nil, "Es gibt keinen Ordner „fehlt“"},
	} {
		list, problems := Load([]string{"testdata/skills"}, map[string]map[string]any{name: c.settings})
		if len(list) != 0 || len(problems) != 1 || problems[0].Skill != name || !strings.Contains(problems[0].Text(i18n.DE), c.want) {
			t.Errorf("%s: %v %+v", name, list, problems)
		}
	}
	list, problems := Load([]string{"testdata/skills"}, map[string]map[string]any{
		"abfall": {"calendar": "x", "farbe": "blau"}, "kaputt": nil})
	if len(list) != 0 || len(problems) != 2 {
		t.Fatalf("problems: %v %+v", list, problems)
	}
	if got := problems[0].Text(i18n.DE); !strings.Contains(got, "„farbe“") || !strings.Contains(got, "calendar, types") {
		t.Errorf("unknown setting: %q", got)
	}
	if got := problems[0].Text(i18n.EN); !strings.Contains(got, `"farbe"`) {
		t.Errorf("english: %q", got)
	}
	ok, bad := Load([]string{"testdata/skills"}, map[string]map[string]any{"abfall": {"calendar": "x"}, "kaputt": nil})
	if len(ok) != 1 || len(bad) != 1 {
		t.Errorf("one broken skill stopped the other: %v %v", ok, bad)
	}
}

func TestProblemTextsComplete(t *testing.T) {
	for _, l := range i18n.All {
		if len(problemTexts[l]) != len(problemTexts[i18n.DE]) {
			t.Errorf("%s has %d problem texts, de has %d", l, len(problemTexts[l]), len(problemTexts[i18n.DE]))
		}
		for kind, text := range problemTexts[l] {
			if text == "" || problemTexts[i18n.DE][kind] == "" {
				t.Errorf("%s: text for %s missing", l, kind)
			}
		}
	}
}

func TestShippedSkillsLoad(t *testing.T) {
	entries, _ := os.ReadDir("../../skills")
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join("../../skills", e.Name(), "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := split(raw); err != nil {
			t.Errorf("%s: %v", e.Name(), err)
		}
	}
}

func berlinSet(t *testing.T, sk Skill) *Set {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	return &Set{Lang: i18n.DE, Skills: []Skill{sk}, Location: berlin,
		Clock: func() time.Time { return time.Date(2026, 10, 21, 18, 0, 0, 0, berlin) }}
}

func TestTools(t *testing.T) {
	set := berlinSet(t, loadAbfall(t, abfallCalendar))
	list := set.Tools()
	if len(list) != 1 || !json.Valid([]byte(list[0].Schema)) || !list[0].ReadOnly {
		t.Fatalf("tools: %+v", list)
	}
	if !strings.Contains(list[0].Description, "- abfall: Abfuhrtermine") {
		t.Errorf("skill list: %s", list[0].Description)
	}
	body, err := list[0].Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"name":"abfall"}`)})
	if err != nil || !strings.Contains(body, "calendar.abfuhrtermine") {
		t.Errorf("open: %q %v", body, err)
	}
	if len((&Set{}).Tools()) != 0 {
		t.Error("tools without skills")
	}
}

type fakeTasker struct {
	answer      string
	instruction string
}

func (f *fakeTasker) RunTask(_ context.Context, instruction string) (string, error) {
	f.instruction = instruction
	return f.answer, nil
}

func TestRunJob(t *testing.T) {
	sk := loadAbfall(t, abfallCalendar)
	set := berlinSet(t, sk)
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tasker := &fakeTasker{answer: `{"importance":3,"spoken":"Morgen wird der Wertstoff abgeholt.","expires_in_hours":14}`}
	r := &Runner{Set: set, Tasker: tasker, Store: st}

	queued, err := r.RunJob(context.Background(), sk, 0)
	if err != nil || !queued {
		t.Fatalf("run: %v %v", queued, err)
	}
	if !strings.Contains(tasker.instruction, `Prüfe mit calendar_events (calendar "calendar.abfuhrtermine"`) {
		t.Errorf("instruction:\n%s", tasker.instruction)
	}
	list, _ := st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}})
	if len(list) != 1 || list[0].Importance != 2 || list[0].Spoken != "Morgen wird der Wertstoff abgeholt." {
		t.Errorf("message (importance capped at 2): %+v", list)
	}

	tasker.answer = `{"importance":0,"spoken":""}`
	if queued, err := r.RunJob(context.Background(), sk, 0); err != nil || queued {
		t.Errorf("nothing to report queued a message: %v %v", queued, err)
	}
}

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

// The alarm clock finishes with a Home Assistant action and can take it
// back; its days may stay empty.
func TestWeckerFinishesWithAnAction(t *testing.T) {
	list, problems := Load([]string{"../../skills"}, map[string]map[string]any{"wecker": nil})
	if len(problems) != 0 || len(list) != 1 || list[0].Target == nil {
		t.Fatalf("load: %v %v", list, problems)
	}
	tg := list[0].Target
	if tg.Finish.Name != FinishAction || tg.Finish.Action.Action != "esphome.{{ satellite.device }}_alarm_set" ||
		tg.Undo == nil || tg.Confirm {
		t.Errorf("target = %+v", tg)
	}
	if !tg.Fields[1].Optional || tg.Fields[0].Optional {
		t.Errorf("fields = %+v", tg.Fields)
	}
}

// The example skill in the guides must load: what the guide shows is what
// works.
func TestGuideExampleLoads(t *testing.T) {
	for file, c := range map[string]struct{ marker, name, setting string }{
		"../../docs/guide/de.md": {"## Eigenen Skill erstellen", "blumen", "pflanzen"},
		"../../docs/guide/en.md": {"## Creating your own skill", "plants", "plants"},
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		i := strings.Index(text, c.marker)
		if i < 0 {
			t.Fatalf("%s: section missing", file)
		}
		block := text[i:]
		start := strings.Index(block, "```markdown\n")
		end := strings.Index(block[start+12:], "```")
		if start < 0 || end < 0 {
			t.Fatalf("%s: no example", file)
		}
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, c.name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, c.name, "SKILL.md"), []byte(block[start+12:start+12+end]), 0o644); err != nil {
			t.Fatal(err)
		}
		list, problems := Load([]string{dir}, map[string]map[string]any{c.name: {c.setting: "Farn jeden Sonntag"}})
		if len(problems) != 0 || len(list) != 1 {
			t.Fatalf("%s: %v %+v", file, list, problems)
		}
		sk := list[0]
		if len(sk.Schedules) != 1 || !strings.Contains(sk.Schedules[0].Task, "Farn jeden Sonntag") || !strings.Contains(sk.Body, "Farn jeden Sonntag") {
			t.Errorf("%s: setting not filled in: %+v", file, sk)
		}
		// Without the setting the skill does not start, and says which one.
		_, problems = Load([]string{dir}, map[string]map[string]any{c.name: nil})
		if len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.DE), "„"+c.setting+"“") {
			t.Errorf("%s: %+v", file, problems)
		}
	}
}
