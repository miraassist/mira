package skills

import (
	"context"
	"fmt"
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

func (f *fakeTasker) RunTask(_ context.Context, _, instruction string) (string, error) {
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
	list, _ := st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}, IncludeExpired: true})
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

// Pakete is a mail application: it loads without settings, and its mail
// section tells the extraction what to keep and never the pickup codes.
func TestPaketeIsAMailApplication(t *testing.T) {
	list, problems := Load([]string{"../../skills"}, map[string]map[string]any{"pakete": nil})
	if len(problems) != 0 || len(list) != 1 || list[0].Mail == nil || list[0].Target != nil {
		t.Fatalf("load: %+v %v", list, problems)
	}
	if ex := list[0].Mail.Extract; !strings.Contains(ex, "Sendungsnummer") || !strings.Contains(ex, "Abholcodes") {
		t.Errorf("extract: %q", ex)
	}
	if list[0].Mail.Done != "●" {
		t.Errorf("done mark: %q", list[0].Mail.Done)
	}
}

// Kochen is a run with a plan: no fields, no finish, an hour per step.
func TestKochenIsAPlan(t *testing.T) {
	list, problems := Load([]string{"../../skills"}, map[string]map[string]any{"kochen": nil})
	if len(problems) != 0 || len(list) != 1 || list[0].Target == nil || !list[0].Target.Plan {
		t.Fatalf("load: %+v %v", list, problems)
	}
	if d := list[0].Target.TimeoutDuration(); d != time.Hour {
		t.Errorf("timeout: %v", d)
	}
}

// A plan with fields or a finish is a mistake in the skill.
func TestPlanWithFieldsIsAProblem(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: x\ndescription: X.\nmetadata:\n  mira:\n    target:\n      plan: true\n      fields:\n        - name: a\n          description: A\n---\nText.\n"
	if err := os.WriteFile(filepath.Join(dir, "x", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	if list, problems := Load([]string{dir}, map[string]map[string]any{"x": nil}); len(list) != 0 || len(problems) != 1 {
		t.Errorf("plan with fields: %+v %v", list, problems)
	}
}

// Every example skill in the guides' section on writing skills loads,
// without a problem: what the guide shows is what works.
func TestAllGuideExamplesLoad(t *testing.T) {
	settings := map[string]map[string]any{"blumen": {"pflanzen": "Farn"}, "plants": {"plants": "Fern"}}
	for file, marker := range map[string]string{
		"../../docs/guide/de.md": "## Eigenen Skill erstellen",
		"../../docs/guide/en.md": "## Creating your own skill",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		section := string(raw)[strings.Index(string(raw), marker)+len(marker):]
		section = section[:strings.Index(section, "\n## ")]
		blocks := strings.Split(section, "```markdown\n")[1:]
		if len(blocks) != 4 {
			t.Fatalf("%s: %d examples", file, len(blocks))
		}
		for _, b := range blocks {
			text := b[:strings.Index(b, "```")]
			name := strings.TrimSpace(strings.SplitN(strings.SplitN(text, "name:", 2)[1], "\n", 2)[0])
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			list, problems := Load([]string{dir}, map[string]map[string]any{name: settings[name]})
			if len(problems) != 0 || len(list) != 1 {
				t.Errorf("%s, %s: %+v %v", file, name, list, problems)
			}
		}
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

// A per_person schedule runs once for each person it is given, with their
// name, their calendar ("-" without one) and what Mira knows about them in
// the task, and its message goes to that person; a person with nothing to
// report gets nothing.
func TestPerPersonScheduleRunsForEachPerson(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tag"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: tag\ndescription: Der eigene Tag.\nmetadata:\n  mira:\n    schedules:\n      - cron: \"35 5 * * *\"\n        per_person: true\n        task: Termine von {{person}} im Kalender {{person_calendar}}, Pakete {{person_login}}.\n---\nAnleitung.\n"
	if err := os.WriteFile(filepath.Join(dir, "tag", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	list, problems := Load([]string{dir}, map[string]map[string]any{"tag": nil})
	if len(problems) != 0 || len(list) != 1 || !list[0].Schedules[0].PerPerson {
		t.Fatalf("load: %+v %+v", list, problems)
	}
	sk := list[0]
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tasker := &perPersonTasker{answers: map[string]string{
		"Ben":  `{"importance":1,"spoken":"Du hast um 9 Zahnarzt."}`,
		"Anna": `{"importance":0,"spoken":""}`,
	}}
	r := &Runner{Set: berlinSet(t, sk), Tasker: tasker, Store: st, People: func(context.Context) []Person {
		return []Person{{Key: "anna", Name: "Anna"}, {Key: "ben", Name: "Ben", Login: "ben", Calendar: "Ben", Profile: "Ben interessiert sich für den KSC."}}
	}}
	queued, err := r.RunJob(context.Background(), sk, 0)
	if err != nil || !queued {
		t.Fatalf("run: %v %v", queued, err)
	}
	if len(tasker.seen) != 2 || !strings.Contains(tasker.seen[1], "Termine von Ben im Kalender Ben, Pakete ben.") ||
		!strings.Contains(tasker.seen[1], "Was du über Ben weißt") || !strings.Contains(tasker.seen[1], "Ben interessiert sich für den KSC.") {
		t.Errorf("tasks: %q", tasker.seen)
	}
	if !strings.Contains(tasker.seen[0], "Termine von Anna im Kalender -, Pakete -.") || !strings.Contains(tasker.seen[0], "(nichts)") {
		t.Errorf("Anna without calendar and memory: %q", tasker.seen[0])
	}
	msgs, _ := st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}, IncludeExpired: true})
	if len(msgs) != 1 || msgs[0].Receiver != "ben" || msgs[0].Spoken != "Du hast um 9 Zahnarzt." || !strings.HasSuffix(msgs[0].DedupeKey, ":ben") {
		t.Errorf("messages: %+v", msgs)
	}
	// Nobody with a calendar of their own: nothing runs.
	r.People = func(context.Context) []Person { return nil }
	tasker.seen = nil
	if queued, err := r.RunJob(context.Background(), sk, 0); err != nil || queued || len(tasker.seen) != 0 {
		t.Errorf("without people: %v %v %q", queued, err, tasker.seen)
	}
}

type perPersonTasker struct {
	answers map[string]string
	seen    []string
}

func (f *perPersonTasker) RunTask(_ context.Context, _, instruction string) (string, error) {
	f.seen = append(f.seen, instruction)
	for name, a := range f.answers {
		if strings.Contains(instruction, "Termine von "+name) {
			return a, nil
		}
	}
	return `{"importance":0}`, nil
}

// SM-01 A skill's server is connected with the skill's settings (the
// settings part: url and the secret token are filled into the server, not
// into the instructions).
func TestSM01_ASkillsServerTakesTheSkillsSettings(t *testing.T) {
	// Given a skill with an mcp section whose url and token come from its settings, the token secret
	// And mira.yaml sets them under skills.enabled
	loaded, problems := Load([]string{"../../skills"}, map[string]map[string]any{
		"paperless": {"url": "http://paperless-mcp:3000/mcp", "token": "geheim"}})
	if len(problems) != 0 || len(loaded) != 1 {
		t.Fatalf("problems %+v", problems)
	}
	// When Mira starts
	// Then she connects to the server with that url and token
	// And the token is in no prompt
	sk := loaded[0]
	if sk.MCP == nil || sk.MCP.Name != "paperless" || sk.MCP.Skill != "paperless" || sk.MCP.URL != "http://paperless-mcp:3000/mcp" ||
		sk.MCP.Token != "geheim" || !sk.MCP.WebContent || len(sk.MCP.Tools) != 2 {
		t.Errorf("server %+v", sk.MCP)
	}
	if strings.Contains(sk.Body, "geheim") || strings.Contains(sk.Description, "geheim") {
		t.Error("the secret token reached the skill's text")
	}
}

// SM-11 A broken mcp section is a problem of the skill
func TestSM11_ABrokenMCPSectionIsAProblemOfTheSkill(t *testing.T) {
	dir := t.TempDir()
	write := func(mcp string) {
		os.MkdirAll(filepath.Join(dir, "akten"), 0o755)
		text := "---\nname: akten\ndescription: Akten.\nmetadata:\n  mira:\n    mcp:\n" + mcp + "---\nText.\n"
		if err := os.WriteFile(filepath.Join(dir, "akten", "SKILL.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Given a skill whose mcp section has no url
	write("      tools: [suche]\n")
	// When Mira loads it
	_, problems := Load([]string{dir}, map[string]map[string]any{"akten": nil})
	// Then the skill does not start, and the problem says what is missing
	if len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.DE), "Adresse (url)") {
		t.Errorf("problems %+v", problems)
	}
	write("      transport: ftp\n      url: x\n")
	if _, problems := Load([]string{dir}, map[string]map[string]any{"akten": nil}); len(problems) != 1 ||
		!strings.Contains(problems[0].Text(i18n.DE), "„ftp“") {
		t.Errorf("unknown transport: %+v", problems)
	}
}

// Where the house is comes from Home Assistant when a skill is used: the
// placeholders home.latitude, home.longitude and home.name, "-" when unknown.
func TestHomePlaceholders(t *testing.T) {
	sk := Skill{Name: "pollen", Description: "Pollen", Body: "Lies {{ home.latitude }},{{home.longitude}} in {{ home.name }}; {{ andere }}."}
	set := &Set{Lang: i18n.DE, Skills: []Skill{sk}, Home: func(context.Context) map[string]string {
		return map[string]string{"home.latitude": "49.0100", "home.longitude": "8.4000", "home.name": "Zuhause"}
	}}
	out, err := set.Tools()[0].Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"name":"pollen"}`)})
	if err != nil || out != "Lies 49.0100,8.4000 in Zuhause; {{ andere }}." {
		t.Errorf("open: %q %v", out, err)
	}
	set.Home = nil
	if out, _ := set.Tools()[0].Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"name":"pollen"}`)}); out != "Lies -,- in -; {{ andere }}." {
		t.Errorf("without Home Assistant: %q", out)
	}
}

// A schedule with urgent may report importance 3 (sent at once, at night too);
// without it a job's message stays at 2.
func TestUrgentSchedule(t *testing.T) {
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sk := Skill{Name: "warnungen", Body: "Warnungen.", Schedules: []Schedule{
		{Task: "Prüf die Warnungen.", Urgent: true}, {Task: "Prüf die Warnungen."}}}
	tasker := &fakeTasker{answer: `{"importance":3,"spoken":"Extremes Unwetter.","expires_in_hours":6}`}
	r := &Runner{Set: berlinSet(t, sk), Tasker: tasker, Store: st}
	for i, want := range []int{3, 2} {
		if _, err := r.RunJob(context.Background(), sk, i); err != nil {
			t.Fatal(err)
		}
		list, _ := st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}, IncludeExpired: true})
		var got int
		for _, m := range list {
			if strings.HasSuffix(m.DedupeKey, fmt.Sprintf(":%d:2026-10-21", i)) {
				got = m.Importance
			}
		}
		if got != want {
			t.Errorf("schedule %d: importance %d, want %d (%+v)", i, got, want, list)
		}
	}
	if !strings.Contains(tasker.instruction, "2 = wichtig") || strings.Contains(tasker.instruction, "3 = dringend") {
		t.Errorf("the last, not urgent, schedule was told about 3:\n%s", tasker.instruction)
	}
}
